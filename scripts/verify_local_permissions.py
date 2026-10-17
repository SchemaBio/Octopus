"""Verify an explicitly selected local deployment; cleans only its own fixtures."""
import argparse, hashlib, json, secrets, urllib.request, urllib.error
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--env", required=True)
parser.add_argument("--url", default="http://127.0.0.1:8080/api/v1")
args = parser.parse_args()
assert args.url.startswith(("http://localhost:", "http://127.0.0.1:")), "Local deployment only"
env = dict(line.strip().split("=", 1) for line in Path(args.env).read_text().splitlines() if "=" in line and not line.lstrip().startswith("#"))
base = args.url
checks = 0

def request(method, path, token=None, body=None, status=200):
    global checks
    headers = {"Content-Type": "application/json"}
    if token: headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(base + path, data=json.dumps(body).encode() if body is not None else None, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            code, raw = r.status, r.read()
            data = json.loads(raw) if raw else {}
    except urllib.error.HTTPError as r: code, data = r.code, json.load(r)
    assert code == status, f"{method} {path}: expected {status}, got {code}: {data.get('error', '')}"
    checks += 1
    return data.get("data", data)

def denied(method, path, token, body, permission):
    r = request(method, path, token, body, 403)
    assert r.get("code") == "permission_denied" and r.get("required_permission") == permission, (path, r)

def login(email, password):
    return request("POST", "/auth/login", body={"email": email, "password": password})["access_token"]

admin = login("admin@schemabio.local", env["DEFAULT_ADMIN_PASSWORD"].strip("\"'"))
assert len(request("GET", "/auth/me", admin)["permissions"]) == 9
suffix = secrets.token_hex(6)
password = "Q7!" + secrets.token_urlsafe(20)
users, sample_ids, job_ids, gene_ids = [], [], [], []

def grant(uid, permissions):
    return request("PUT", f"/users/{uid}", admin, {"permissions": permissions})

try:
    for letter in ("a", "b"):
        email = f"permissions-{suffix}-{letter}@example.invalid"
        user = request("POST", "/users", admin, {"email": email, "name": "Permission verification " + letter, "password": password, "system_role": "USER"}, 201)
        users.append((str(user["id"]), email))
        assert user["permissions"] == []
    a, b = [login(email, password) for _, email in users]
    aid, bid = [uid for uid, _ in users]
    # Both Data Center and BED pages depend on this shared storage aggregate.
    for token in (admin, a, b):
        request("GET", "/upload/files/stats", token)
        request("GET", "/data/assets?read_type=bed", token)
        request("GET", "/data/assets?read_type=read1", token)
    grant(aid, ["tasks.manage"])
    request("GET", "/upload/files/stats", a)
    request("GET", "/data/assets?read_type=bed", a)
    request("GET", "/data/assets?read_type=read1", a)
    grant(aid, [])
    denied("POST", "/samples", a, {}, "samples.write")
    denied("POST", "/pipelines", a, {}, "pipelines.manage")
    denied("POST", "/gene-lists", a, {}, "gene_lists.manage")
    denied("POST", "/cnv-baselines", a, {}, "baselines.manage")
    denied("POST", "/report-templates", a, {}, "report_services.manage")
    denied("POST", "/tasks", a, {}, "tasks.manage")
    denied("POST", "/tasks/batch", a, {}, "tasks.manage")
    denied("POST", "/tasks/not-a-task/reports", a, {}, "interpretation_reports.manage")
    request("PUT", f"/users/{aid}", a, {"permissions": ["samples.write"]}, 403)
    request("PUT", "/auth/me", a, {"name": "Ordinary profile", "permissions": ["samples.write"]})
    assert request("GET", "/auth/me", a)["permissions"] == []
    request("PUT", f"/users/{aid}", admin, {"permissions": ["unknown.manage"]}, 400)
    assert grant(aid, ["samples.write", "samples.write"])["permissions"] == ["samples.write"]
    request("PUT", f"/users/{aid}", admin, {"name": "Preserve grants"})
    assert request("GET", "/auth/me", a)["permissions"] == ["samples.write"]
    sample = request("POST", "/samples", a, {"internal_id": "perm-" + suffix}, 201)
    sid = sample["id"]; sample_ids.append(sid)
    assert request("GET", f"/samples/{sid}", b)["internal_id"] == "perm-" + suffix
    listed = request("GET", "/samples?search=perm-" + suffix, b)
    assert any(x["id"] == sid for x in listed["items"])
    denied("PUT", f"/samples/{sid}", b, {"remark": "denied"}, "samples.write")
    grant(bid, ["samples.write"])
    request("PUT", f"/samples/{sid}", b, {"remark": "shared edit"})
    assert request("GET", f"/samples/{sid}", a)["remark"] == "shared edit"
    grant(bid, [])
    denied("PUT", f"/samples/{sid}", b, {"remark": "revoked"}, "samples.write")
    assert request("GET", f"/samples/{sid}", b)["remark"] == "shared edit"
    # Personal grants cannot imply write access to another domain.
    denied("POST", "/tasks/not-a-task/results/igv/snapshot", a, {}, "interpretation_reports.manage")
    request("POST", "/tasks/estimate", b, {}, 400)
    request("POST", "/tasks/batch/preview", b, {}, 400)
    grant(aid, ["gene_lists.manage"])
    gene = request("POST", "/gene-lists", a, {"name": "permissions-" + suffix, "genes": ["BRCA1"]}, 201)
    gid = gene["id"]; gene_ids.append(gid)
    assert gene["scope"] == "instance"
    request("GET", f"/gene-lists/{gid}", b)
    denied("POST", f"/gene-lists/{gid}/publish", b, {"expected_revision": gene["revision"]}, "gene_lists.manage")
    request("POST", f"/gene-lists/{gid}/publish", a, {"expected_revision": gene["revision"]})
    grant(aid, ["bed.manage"])
    fastq = {"name": "permission test", "file_type": "fastq_single", "files": [{"file_name": "test.fastq.gz", "read_type": "single", "file_size": 10}]}
    bed = {"name": "permission test " + suffix, "file_type": "bed", "reference_genome": "hg19", "provider": "local", "files": [{"file_name": "test.bed", "read_type": "bed", "file_size": 10}]}
    denied("POST", "/upload/jobs", a, fastq, "data.manage")
    request("POST", "/upload/jobs", a, {**bed, "file_type": "other"}, 400)
    job = request("POST", "/upload/jobs", a, bed, 201)
    jid = job["id"]; job_ids.append(jid)
    fid = job["files"][0]["id"]
    request("GET", f"/upload/jobs/{jid}", b)
    grant(aid, [])
    for method, path in [("POST", f"/upload/files/{fid}/start"), ("POST", f"/upload/files/{fid}/complete"), ("POST", f"/upload/files/{fid}/multipart"), ("DELETE", f"/upload/jobs/{jid}")]:
        denied(method, path, a, {}, "bed.manage")
    grant(aid, ["data.manage"])
    denied("POST", "/upload/jobs", a, bed, "bed.manage")
    denied("POST", "/upload/jobs", a, {**fastq, "internal_id": "perm-" + suffix}, "samples.write")
    grant(aid, ["tasks.manage"])
    denied("POST", "/tasks/not-a-task/reports", a, {}, "interpretation_reports.manage")
    grant(aid, ["interpretation_reports.manage"])
    denied("POST", "/tasks", a, {}, "tasks.manage")
    request("PUT", f"/users/{aid}", admin, {"system_role": "SUPER_ADMIN"})
    assert len(request("GET", f"/users/{aid}", admin)["permissions"]) == 9
    request("PUT", f"/users/{aid}", admin, {"system_role": "USER"})
    assert request("GET", f"/users/{aid}", admin)["permissions"] == []
    # Shared history can be read and advanced without any business grant.
    history = request("GET", "/history/sync?table=snv-indel", b)
    request("GET", "/history/sync?table=snv-indel&cursor=" + urllib.parse.quote(history["cursor"]), b)
    print(f"PASS: {checks} local API checks; shared data, independent grants, revocation, upload follow-ups and role changes")
finally:
    for gid in gene_ids: request("DELETE", f"/gene-lists/{gid}", admin, status=204)
    for jid in job_ids: request("DELETE", f"/upload/jobs/{jid}", admin, status=204)
    for sid in sample_ids: request("DELETE", f"/samples/{sid}", admin, status=204)
    for uid, email in users:
        actual = request("GET", f"/users/{uid}", admin)
        assert actual["email"] == email
        request("DELETE", f"/users/{uid}", admin, status=204)
    print("Temporary test accounts and records cleaned")
