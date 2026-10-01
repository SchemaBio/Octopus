import math
import tempfile
import hashlib
import json
import os
import re
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import duckdb

ROOT = Path(os.environ.get("PARQUET_CACHE_ROOT", "/data/parquet-cache")).resolve()
ASSESSMENT_ROOT = Path(os.environ.get("PARQUET_ASSESSMENT_ROOT", "/data/parquet-assessments")).resolve()
PORT = int(os.environ.get("PORT", "9100"))
SLOTS = threading.BoundedSemaphore(4)
PREPARE_LOCK = threading.Lock()
TABLES = {"snv-indel", "cnv-segment", "cnv-exon", "str", "mei", "mt", "upd", "roh"}
MAX_BODY = 16 << 20
MAX_LIMIT = 200
MAX_FILTERS = 40
FIELD_PROFILE_VERSION = "parquet-fields-v2"
ACMG_PROFILE = "acmg-snv-points-v2"
OVERLAY_FIELDS = {
    "reviewed", "reported", "interpretation", "acmgClassification", "acmgEvidence",
    "cnvAssessment", "cnvClassification", "cnvScore", "acmgScore", "acmgProfile", "acmgState", "acmgOverride", "acmgOverrideReason",
}
NUMERIC_FIELDS = {
    "acmgScore", "cnvScore",
    "Position", "Start", "End", "Quality", "Depth", "VAF", "GnomAD_AF", "GnomAD_AF_EAS",
    "GnomAD_nhomalt_XX", "GnomAD_nhomalt_XY", "Pangolin_Gain", "Pangolin_Loss", "EVOScore",
    "AlphaMissense_AM", "copy_number", "score", "size", "Repeat_Count", "RepeatCount",
    "Heteroplasmy", "Heteroplasmy_Level", "NbVariants", "Percentage_Homozygosity",
    "Average_Depth", "Log2_Ratio", "Copy_Ratio", "Start_Position", "End_Position",
}


def ident(name):
    if not isinstance(name, str) or not name or len(name) > 256 or "\x00" in name:
        raise ValueError("invalid field")
    return '"' + name.replace('"', '""') + '"'


def row_id(dataset_id, object_hash, ordinal):
    return hashlib.sha256(f"{dataset_id}/{object_hash}/{ordinal}".encode()).hexdigest()


def overlay_expr(column):
    return "json_extract_string(o.payload, '$." + column + "')"


def automatic_expr(columns, table):
    if table != "snv-indel" or not {"Type", "Consequence", "Transcript", "AlphaMissense_AM"}.issubset(columns):
        return "'" + json.dumps({"profile": ACMG_PROFILE, "state": "insufficient_evidence", "score": 0, "criteria": []}) + "'"
    value = "TRY_CAST(t." + ident("AlphaMissense_AM") + " AS DOUBLE)"
    valid = ("lower(trim(t." + ident("Type") + ")) IN ('snp','snv') AND contains(lower(t." + ident("Consequence") + "), 'missense_variant') AND "
             "regexp_full_match(trim(t." + ident("Transcript") + "), 'ENST[0-9]+(\\.[0-9]+)?') AND isfinite(" + value + ") AND " + value + " BETWEEN 0 AND 1")
    points = "(CASE WHEN " + valid + " THEN CASE WHEN " + value + ">=0.990 THEN 4 WHEN " + value + ">=0.906 THEN 2 WHEN " + value + ">=0.792 THEN 1 WHEN " + value + "<0.100 THEN -2 WHEN " + value + "<0.170 THEN -1 ELSE 0 END ELSE 0 END)"
    classification = "CASE WHEN " + points + ">0 THEN 'VUS' WHEN " + points + "<0 THEN 'Likely_Benign' ELSE '' END"
    criteria = "CASE WHEN " + points + "=0 THEN json_array() ELSE json_array(json_object('code',CASE WHEN " + points + ">0 THEN 'PP3' ELSE 'BP4' END,'strength',CASE WHEN abs(" + points + ")=4 THEN 'strong' WHEN abs(" + points + ")=2 THEN 'moderate' ELSE 'supporting' END,'source','AlphaMissense','value'," + value + ")) END"
    return "json_object('profile','" + ACMG_PROFILE + "','score'," + points + ",'classification'," + classification + ",'state',CASE WHEN " + points + "=0 THEN 'insufficient_evidence' ELSE 'classified' END,'criteria'," + criteria + ")"


def field_expr(column, columns, table):
    if column in {"cnvClassification", "cnvScore", "cnvAssessment"}:
        if column == "cnvAssessment": return overlay_expr(column)
        key = "classification" if column == "cnvClassification" else "totalScore"
        return "json_extract_string(o.payload, '$.cnvAssessment." + key + "')"
    if column in {"reviewed", "reported"}:
        return "COALESCE(" + overlay_expr(column) + ", 'false')"
    baseline_names = {"acmgClassification": "classification", "acmgScore": "score",
                      "acmgProfile": "profile", "acmgState": "state", "acmgEvidence": "criteria"}
    if column in baseline_names:
        base = "json_extract_string(" + automatic_expr(columns, table) + ", '$." + baseline_names[column] + "')"
        if column == "acmgClassification":
            manual = "CASE WHEN json_exists(o.payload, '$.acmgEvidence') THEN NULLIF(" + overlay_expr(column) + ", '') ELSE " + base + " END"
            return "COALESCE(NULLIF(" + overlay_expr("acmgOverride") + ", ''), " + manual + ")"
        return "CASE WHEN json_exists(o.payload, '$.acmgEvidence') THEN " + overlay_expr(column) + " ELSE " + base + " END"
    if column in OVERLAY_FIELDS:
        return overlay_expr(column)
    if column not in columns:
        raise ValueError("unknown filter field")
    return "t." + ident(column)


def predicate(column, operator, value, columns, table):
    field = field_expr(column, columns, table)
    value_text = "CAST(" + field + " AS VARCHAR)"
    if operator == "is_missing":
        return f"({field} IS NULL OR {value_text} IN ('', '.'))", []
    if operator == "is_not_missing":
        return f"({field} IS NOT NULL AND {value_text} NOT IN ('', '.'))", []
    if operator in {"contains", "equals", "in"}:
        values = value if isinstance(value, list) else [value]
        if not values or len(values) > 1000:
            raise ValueError("invalid filter values")
        if operator == "contains":
            needle = str(values[0])
            return f"contains(lower({value_text}), lower(?))", [needle]
        if operator == "equals":
            return f"EXISTS (SELECT 1 FROM UNNEST(string_split({value_text}, '&')) AS u(v) WHERE v = ?)", [str(values[0]).lower() if isinstance(values[0], bool) else str(values[0])]
        placeholders = ",".join("?" for _ in values)
        return (
            f"EXISTS (SELECT 1 FROM UNNEST(string_split({value_text}, '&')) AS u(v) WHERE v IN ({placeholders}))",
            [str(item) for item in values],
        )
    if operator in {"gt", "gte", "lt", "lte"}:
        op = {"gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[operator]
        return (
            "EXISTS (SELECT 1 FROM UNNEST(string_split(" + value_text + ", '&')) AS u(v) "
            "WHERE TRY_CAST(NULLIF(v, '.') AS DOUBLE) " + op + " TRY_CAST(? AS DOUBLE))",
            [value],
        )
    if operator == "between":
        values = value if isinstance(value, list) else []
        if len(values) != 2:
            raise ValueError("between requires two numeric bounds")
        try:
            low, high = float(values[0]), float(values[1])
        except (TypeError, ValueError) as exc:
            raise ValueError("between requires numeric bounds") from exc
        if low > high:
            raise ValueError("between lower bound exceeds upper bound")
        return (
            "EXISTS (SELECT 1 FROM UNNEST(string_split(" + value_text + ", '&')) AS u(v) "
            "WHERE TRY_CAST(NULLIF(v, '.') AS DOUBLE) BETWEEN TRY_CAST(? AS DOUBLE) AND TRY_CAST(? AS DOUBLE))",
            [low, high],
        )
    raise ValueError("unsupported filter")


def sort_expression(column, columns, table):
    field = field_expr(column, columns, table)
    if column.lower() in {"chromosome", "chr", "chrom"}:
        value = "regexp_replace(upper(trim(CAST(" + field + " AS VARCHAR))), '^CHR', '')"
        return "CASE WHEN TRY_CAST(" + value + " AS INTEGER) IS NOT NULL THEN TRY_CAST(" + value + " AS INTEGER) " \
            "WHEN " + value + " = 'X' THEN 23 WHEN " + value + " = 'Y' THEN 24 WHEN " + value + " IN ('M', 'MT') THEN 25 ELSE 1000 END"
    if column in NUMERIC_FIELDS or re.search(r"(^|_)(af|vaf|depth|score|count|size|start|end|position|ratio|length|fraction|percent|heteroplasmy)(_|$)", column, re.I):
        return "(SELECT min(TRY_CAST(NULLIF(v, '.') AS DOUBLE)) FROM UNNEST(string_split(CAST(" + field + " AS VARCHAR), '&')) AS u(v))"
    return field


def field_type(column):
    if column in {"reviewed", "reported"}:
        return "boolean"
    if column == "acmgClassification":
        return "enum"
    if column == "acmgScore" or column in NUMERIC_FIELDS or re.search(r"(^|_)(af|vaf|depth|score|count|size|start|end|position|ratio|length|fraction|percent|heteroplasmy)(_|$)", column, re.I):
        return "number"
    return "text"


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        return

    def send_json(self, status, payload):
        data = json.dumps(payload, separators=(",", ":"), default=str).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/health":
            self.send_json(200, {"status": "ok", "duckdb": duckdb.__version__})
        else:
            self.send_json(404, {"error": "not_found"})

    def do_POST(self):
        if self.path not in {"/v1/query", "/v1/prepare", "/v1/export"}:
            self.send_json(404, {"error": "not_found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            length = 0
        if length <= 0 or length > MAX_BODY:
            self.send_json(413, {"error": "request_too_large"})
            return
        if not SLOTS.acquire(timeout=1):
            self.send_json(429, {"error": "query_capacity"})
            return
        try:
            request = json.loads(self.rfile.read(length))
            if not isinstance(request, dict):
                raise ValueError("query body must be an object")
            if self.path == "/v1/prepare":
                with PREPARE_LOCK:
                    self.send_json(200, prepare_automatic_acmg(request))
            elif self.path == "/v1/export":
                output = execute(request, export=True)
                try:
                    self.send_response(200)
                    self.send_header("Content-Type", "text/csv; charset=utf-8")
                    self.send_header("Content-Length", str(output.stat().st_size))
                    self.end_headers()
                    with output.open("rb") as stream:
                        while chunk := stream.read(65536):
                            self.wfile.write(chunk)
                finally:
                    output.unlink(missing_ok=True)
            else:
                self.send_json(200, execute(request))
        except (ValueError, TypeError, KeyError, duckdb.Error, OSError) as exc:
            self.send_json(400, {"error": "query_failed", "message": str(exc)[:240]})
        finally:
            SLOTS.release()


def execute(request, export=False):
    table = request.get("table")
    if table not in TABLES:
        raise ValueError("unsupported table")
    path = source_path(request)
    dataset = str(request["datasetId"])
    object_hash = str(request["objectSha256"])
    if not re.fullmatch(r"[0-9a-f]{64}", object_hash):
        raise ValueError("invalid parquet fingerprint")
    offset = max(0, int(request.get("offset", 0)))
    limit = max(1, min(MAX_LIMIT, int(request.get("limit", 50))))
    conn = duckdb.connect(":memory:")
    timer = threading.Timer(30, conn.interrupt)
    timer.daemon = True
    timer.start()
    try:
        conn.execute("SET memory_limit='256MB'")
        conn.execute("SET threads=1")
        conn.execute("SET TimeZone='UTC'")
        conn.execute("CREATE TEMP TABLE overlays(row_id VARCHAR PRIMARY KEY, payload JSON, version BIGINT)")
        overlays = request.get("overlays", [])
        if overlays is None: overlays = []
        if not isinstance(overlays, list): raise ValueError("invalid row adjustments")
        if len(overlays) > 100000:
            raise ValueError("too many row adjustments")
        if overlays:
            conn.executemany(
                "INSERT INTO overlays VALUES (?, ?, ?)",
                [
                    (
                        str(item["rowId"]),
                        json.dumps(item["payload"], separators=(",", ":")),
                        int(item.get("version", 0)),
                    )
                    for item in overlays
                ],
            )
        source = "read_parquet(?, file_row_number=true)"
        described = conn.execute("DESCRIBE SELECT * FROM " + source, [str(path)]).fetchall()
        columns = {row[0] for row in described if row[0] != "file_row_number"}
        filters = request.get("filters", [])
        if filters is None: filters = []
        if not isinstance(filters, list) or len(filters) > MAX_FILTERS:
            raise ValueError("too many filters")
        where, params = [], []
        for item in filters:
            if not isinstance(item, dict):
                raise ValueError("invalid filter")
            clause, values = predicate(item.get("column", ""), item.get("operator", ""), item.get("value"), columns, table)
            where.append(clause)
            params.extend(values)
        search = str(request.get("search", "")).strip()
        if search:
            searchable = ["contains(lower(CAST(t." + ident(column) + " AS VARCHAR)), lower(?))" for column in sorted(columns)]
            searchable.append("contains(lower(COALESCE(" + field_expr("acmgClassification", columns, table) + ", '')), lower(?))")
            where.append("(" + " OR ".join(searchable) + ")")
            params.extend([search] * len(searchable))
        identity = "sha256(? || '/' || ? || '/' || CAST(t.file_row_number AS VARCHAR))"
        joined = f"FROM {source} t LEFT JOIN overlays o ON o.row_id = {identity}"
        base_params = [str(path), dataset, object_hash]
        requested_row = request.get("rowId")
        if requested_row is not None:
            if not re.fullmatch(r"[0-9a-f]{64}", str(requested_row)):
                raise ValueError("invalid row identity")
            where.append("sha256(? || '/' || ? || '/' || CAST(t.file_row_number AS VARCHAR)) = ?")
            params.extend([dataset, object_hash, requested_row])
        where_sql = " WHERE " + " AND ".join(where) if where else ""
        total = conn.execute("SELECT count(*) " + joined + where_sql, base_params + params).fetchone()[0]
        row_count = int(request.get("rowCount") or 0)
        if row_count <= 0:
            row_count = conn.execute("SELECT count(*) FROM " + source, [str(path)]).fetchone()[0]
        sort = request.get("sort", "")
        order = "t.file_row_number ASC"
        if sort:
            field = sort_expression(sort, columns, table)
            direction = "DESC" if request.get("direction") == "desc" else "ASC"
            order = f"CASE WHEN {field} IS NULL OR CAST({field} AS VARCHAR) IN ('', '.') THEN 1 ELSE 0 END ASC, {field} {direction}, t.file_row_number ASC"
        if export:
            handle, filename = tempfile.mkstemp(prefix="octopus-effective-", suffix=".csv")
            os.close(handle)
            output = Path(filename)
            effective = [field_expr(column, columns, table) + " AS " + ident(column) for column in sorted(OVERLAY_FIELDS)]
            selection = "SELECT t.* EXCLUDE(file_row_number), " + identity + " AS row_id, " + ",".join(effective)
            try:
                conn.execute("COPY (" + selection + " " + joined + where_sql + " ORDER BY " + order + ") TO '" + str(output).replace("'", "''") + "' (FORMAT CSV, HEADER TRUE)",
                             [dataset, object_hash] + base_params + params)
                return output
            except Exception:
                output.unlink(missing_ok=True)
                raise
        rows = conn.execute(
        "SELECT t.*, t.file_row_number AS __ordinal, " + identity + " AS __row_id, o.payload AS __adjustments, o.version AS __adjustment_version " +
            joined + where_sql + " ORDER BY " + order + " LIMIT ? OFFSET ?",
            [dataset, object_hash, str(path), dataset, object_hash] + params + [limit, offset],
        ).fetchall()
        names = [item[0] for item in conn.description]
        output = [dict(zip(names, row)) for row in rows]
        for row in output:
            row["__ordinal"] = int(row.pop("__ordinal"))
            row["__row_id"] = row_id(dataset, object_hash, row["__ordinal"])
            row["__acmg"] = auto_acmg(row) if table == "snv-indel" else None
            if isinstance(row.get("__adjustments"), str):
                try:
                    row["__adjustments"] = json.loads(row["__adjustments"])
                except json.JSONDecodeError:
                    row["__adjustments"] = None
        output_columns = sorted(columns | OVERLAY_FIELDS)
        column_types = {column: field_type(column) for column in output_columns}
        return {"items": output, "total": int(total), "rowCount": int(row_count), "offset": offset, "limit": limit, "columns": output_columns, "columnTypes": column_types, "fieldProfileVersion": FIELD_PROFILE_VERSION}
    finally:
        timer.cancel()
        conn.close()


def source_path(request):
    try:
        path = Path(request.get("filePath", "")).resolve(strict=True)
    except OSError as exc:
        raise ValueError("parquet source is unavailable") from exc
    if not path.is_file() or not path.is_relative_to(ROOT):
        raise ValueError("parquet path is outside the cache")
    return path


def prepare_automatic_acmg(request):
    if request.get("table") != "snv-indel":
        raise ValueError("automatic ACMG is only supported for SNP/InDel")
    path = source_path(request)
    dataset = str(request.get("datasetId", ""))
    object_hash = str(request.get("objectSha256", ""))
    if not re.fullmatch(r"[0-9a-f]{64}", dataset) or not re.fullmatch(r"[0-9a-f]{64}", object_hash):
        raise ValueError("invalid dataset identity")
    profile = ACMG_PROFILE
    ASSESSMENT_ROOT.mkdir(parents=True, exist_ok=True)
    output = ASSESSMENT_ROOT / f"{dataset}-{object_hash}-{profile}.jsonl"
    if output.is_file():
        with output.open("rb") as stream:
            rows = sum(1 for _ in stream)
        return {"assessmentFile": str(output), "profile": profile, "rows": rows}
    temporary = output.with_suffix(".tmp")
    conn = duckdb.connect(":memory:")
    timer = threading.Timer(30, conn.interrupt)
    timer.daemon = True
    timer.start()
    count = 0
    try:
        conn.execute("SET memory_limit='256MB'")
        conn.execute("SET threads=1")
        conn.execute("SET TimeZone='UTC'")
        cursor = conn.execute("SELECT * FROM read_parquet(?, file_row_number=true)", [str(path)])
        names = [item[0] for item in cursor.description]
        with temporary.open("w", encoding="utf-8", newline="\n") as stream:
            while True:
                batch = cursor.fetchmany(1000)
                if not batch:
                    break
                for values in batch:
                    row = dict(zip(names, values))
                    ordinal = int(row.pop("file_row_number"))
                    assessment = auto_acmg(row)
                    record = {
                        "rowId": row_id(dataset, object_hash, ordinal),
                        "profileVersion": profile,
                        "assessment": assessment,
                    }
                    stream.write(json.dumps(record, separators=(",", ":"), default=str) + "\n")
                    count += 1
        temporary.chmod(0o640)
        os.replace(temporary, output)
        return {"assessmentFile": str(output), "profile": profile, "rows": count}
    finally:
        timer.cancel()
        conn.close()
        try:
            temporary.unlink(missing_ok=True)
        except OSError:
            pass


def auto_acmg(row):
    if str(row.get("Type", "")).strip().lower() not in {"snp", "snv"} or "missense_variant" not in str(row.get("Consequence", "")).lower():
        return {"profile": ACMG_PROFILE, "state": "insufficient_evidence", "score": 0, "criteria": [], "pending": ["自动 PP3/BP4 仅适用于有明确错义后果的 SNP"]}
    transcript = str(row.get("Transcript") or "").strip()
    if not re.fullmatch(r"ENST[0-9]+(?:\.[0-9]+)?", transcript):
        return {"profile": ACMG_PROFILE, "state": "insufficient_evidence", "score": 0, "criteria": [], "pending": ["当前转录本缺失、多值或无法对应 AlphaMissense"]}
    raw = row.get("AlphaMissense_AM")
    try:
        if raw is None or "&" in str(raw):
            return {"profile": ACMG_PROFILE, "state": "insufficient_evidence", "score": 0, "criteria": [], "pending": ["AlphaMissense 分值缺失或多值无法对应当前转录本"]}
        score = float(raw)
    except (TypeError, ValueError):
        return {"profile": ACMG_PROFILE, "state": "insufficient_evidence", "score": 0, "criteria": [], "pending": ["AlphaMissense 分值不可解析"]}
    if not math.isfinite(score) or not 0 <= score <= 1:
        return {"profile": ACMG_PROFILE, "state": "insufficient_evidence", "score": 0, "criteria": [], "pending": ["AlphaMissense 分值超出有效范围"]}
    if score >= 0.990:
        evidence, points = "PP3_Strong", 4
    elif score >= 0.906:
        evidence, points = "PP3_Moderate", 2
    elif score >= 0.792:
        evidence, points = "PP3_Supporting", 1
    elif score < 0.100:
        evidence, points = "BP4_Moderate", -2
    elif score < 0.170:
        evidence, points = "BP4_Supporting", -1
    else:
        evidence, points = "", 0
    classification = ""
    if points >= 10:
        classification = "Pathogenic"
    elif points >= 6:
        classification = "Likely_Pathogenic"
    elif points <= -7:
        classification = "Benign"
    elif points <= -1:
        classification = "Likely_Benign"
    elif evidence:
        classification = "VUS"
    return {
        "profile": ACMG_PROFILE,
        "state": "classified" if classification else "insufficient_evidence",
        "score": points,
        "criteria": ([{"code": evidence.split("_")[0], "strength": evidence.split("_")[1].lower(), "source": "AlphaMissense", "value": score}] if evidence else []),
        "classification": classification,
        "pending": ["疾病机制、病例/家系及实验室证据未由本自动初评评估"],
    }


if __name__ == "__main__":
    ROOT.mkdir(parents=True, exist_ok=True)
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
