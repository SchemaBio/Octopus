import os
import tempfile
import unittest
from pathlib import Path

os.environ["PARQUET_CACHE_ROOT"] = tempfile.mkdtemp(prefix="octopus-parquet-query-")
import duckdb
import server


class QueryServerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="parquet-fixture-")
        self.root = Path(self.temp.name).resolve()
        server.ROOT = self.root
        server.ASSESSMENT_ROOT = self.root / "assessments"
        server.ASSESSMENT_ROOT.mkdir()
        self.dataset = "a" * 64
        self.fingerprint = "b" * 64
        self.file = self.root / "fixture.parquet"
        conn = duckdb.connect(":memory:")
        conn.execute("""
            CREATE TABLE source(
                Chromosome VARCHAR, Position VARCHAR, Gene VARCHAR, Type VARCHAR,
                Consequence VARCHAR, AlphaMissense_AM VARCHAR, ClinVar_Sig VARCHAR,
                GnomAD_AF_EAS VARCHAR
            )
        """)
        conn.executemany("INSERT INTO source VALUES (?, ?, ?, ?, ?, ?, ?, ?)", [
            ("chr2", "10", "GENE1", "SNP", "missense_variant", "0.995", "Pathogenic", "0.1&0.01"),
            ("chr10", "2", "GENE2", "SNP", "missense_variant", "0.15", "Benign", "0.2"),
            ("chrX", "6", "GENE3", "INDEL", "missense_variant", "0.999", "VUS", "0.3"),
            ("chr1", "7", "GENE4", "SNP", "synonymous_variant", "0.99", "VUS", "."),
        ])
        conn.execute("COPY source TO ? (FORMAT PARQUET)", [str(self.file)])
        self.ordinals = dict(conn.execute("SELECT Gene, file_row_number FROM read_parquet(?, file_row_number=true)", [str(self.file)]).fetchall())
        conn.close()

    def tearDown(self):
        self.temp.cleanup()

    def request(self, **updates):
        request = {
            "table": "snv-indel", "filePath": str(self.file), "datasetId": self.dataset,
            "objectSha256": self.fingerprint, "offset": 0, "limit": 20,
            "filters": [], "overlays": [],
        }
        request.update(updates)
        return request

    def test_automatic_acmg_requires_missense_snp_and_uses_calibrated_points(self):
        result = server.execute(self.request())
        by_gene = {row["Gene"]: row["__acmg"] for row in result["items"]}
        self.assertEqual(by_gene["GENE1"]["criteria"][0]["strength"], "strong")
        self.assertEqual(by_gene["GENE1"]["score"], 4)
        self.assertEqual(by_gene["GENE2"]["criteria"][0]["strength"], "supporting")
        self.assertEqual(by_gene["GENE3"]["state"], "insufficient_evidence")
        self.assertEqual(by_gene["GENE4"]["state"], "insufficient_evidence")

    def test_filter_overlay_is_applied_before_filtering_and_multi_value_numeric_match(self):
        row_id = server.row_id(self.dataset, self.fingerprint, self.ordinals["GENE1"])
        overlays = [{"rowId": row_id, "version": 1, "payload": {"reviewed": True, "acmgOverride": "Pathogenic", "acmgClassification": "Pathogenic"}}]
        result = server.execute(self.request(
            overlays=overlays,
            filters=[
                {"column": "reviewed", "operator": "equals", "value": "true"},
                {"column": "acmgClassification", "operator": "equals", "value": "Pathogenic"},
                {"column": "GnomAD_AF_EAS", "operator": "lt", "value": 0.02},
            ],
        ))
        self.assertEqual([row["Gene"] for row in result["items"]], ["GENE1"])
        self.assertEqual(result["items"][0]["__adjustments"]["reviewed"], True)
        self.assertEqual(result["items"][0]["__adjustment_version"], 1)

    def test_empty_manual_acmg_evidence_does_not_fall_back_to_automatic_classification(self):
        row_id = server.row_id(self.dataset, self.fingerprint, self.ordinals["GENE1"])
        result = server.execute(self.request(
            overlays=[{"rowId": row_id, "version": 2, "payload": {"acmgEvidence": [], "acmgClassification": "", "acmgScore": 0}}],
            filters=[{"column": "acmgClassification", "operator": "equals", "value": "VUS"}],
        ))
        self.assertEqual(result["total"], 0)

    def test_sort_is_stable_and_chromosomes_use_natural_order(self):
        result = server.execute(self.request(sort="Chromosome", direction="asc"))
        self.assertEqual([row["Gene"] for row in result["items"]], ["GENE4", "GENE1", "GENE2", "GENE3"])

    def test_numeric_range_filter_handles_multi_value_annotations(self):
        result = server.execute(self.request(filters=[{"column": "Position", "operator": "between", "value": [8, 11]}]))
        self.assertEqual([row["Gene"] for row in result["items"]], ["GENE1"])

    def test_prepare_persists_versioned_assessment_for_every_source_row(self):
        prepared = server.prepare_automatic_acmg(self.request())
        lines = Path(prepared["assessmentFile"]).read_text(encoding="utf-8").splitlines()
        self.assertEqual(prepared["profile"], "acmg-snv-points-v1")
        self.assertEqual(len(lines), 4)
        records = [__import__("json").loads(line) for line in lines]
        by_id = {record["rowId"]: record for record in records}
        pathogenic_candidate = by_id[server.row_id(self.dataset, self.fingerprint, self.ordinals["GENE1"])]
        self.assertEqual(pathogenic_candidate["assessment"]["criteria"][0]["code"], "PP3")
        self.assertTrue(all(record["profileVersion"] == prepared["profile"] for record in records))

    def test_query_rejects_arbitrary_paths(self):
        outside = self.root.parent / "outside.parquet"
        with self.assertRaises(ValueError):
            server.execute(self.request(filePath=str(outside)))


if __name__ == "__main__":
    unittest.main()
