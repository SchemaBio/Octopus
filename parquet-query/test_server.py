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
                GnomAD_AF_EAS VARCHAR, Transcript VARCHAR
            )
        """)
        conn.executemany("INSERT INTO source VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", [
            ("chr2", "10", "GENE1", "SNP", "missense_variant", "0.995", "Pathogenic", "0.1&0.01", "ENST000001"),
            ("chr10", "2", "GENE2", "SNP", "missense_variant", "0.15", "Benign", "0.2", "ENST000001"),
            ("chrX", "6", "GENE3", "INDEL", "missense_variant", "0.999", "VUS", "0.3", "ENST000001"),
            ("chr1", "7", "GENE4", "SNP", "synonymous_variant", "0.99", "VUS", ".", "ENST000001"),
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

    def test_selected_svcv4_filters_and_export_keep_legacy_evidence(self):
        import csv
        row_id = server.row_id(self.dataset,self.fingerprint,self.ordinals['GENE1'])
        payload = {'activeAcmgVersion':'svcv4','acmgOverride':'Pathogenic','acmgClassification':'Pathogenic',
                   'svcv4Assessment':{'result':{'classification':'VUS','vusSubclass':'VUS-high','score':4,'state':'classified'}}}
        request=self.request(overlays=[{'rowId':row_id,'version':3,'payload':payload}],
                             filters=[{'column':'acmgClassification','operator':'equals','value':'VUS'}])
        result=server.execute(request)
        self.assertEqual([row['Gene'] for row in result['items']],['GENE1'])
        self.assertEqual(result['items'][0]['__adjustments']['acmgOverride'],'Pathogenic')
        filename=server.execute(request,export=True)
        try:
            with filename.open(encoding='utf-8',newline='') as stream:
                exported=list(csv.DictReader(stream))
            self.assertEqual(exported[0]['acmgClassification'],'VUS')
            self.assertEqual(exported[0]['acmgVusSubclass'],'VUS-high')
            self.assertEqual(exported[0]['acmgTrial'],'true')
            self.assertEqual(exported[0]['activeAcmgVersion'],'svcv4')
        finally:
            filename.unlink()
        payload['activeAcmgVersion']='legacy'
        request['filters'][0]['value']='Pathogenic'
        self.assertEqual(server.execute(request)['total'],1)

    def test_sort_is_stable_and_chromosomes_use_natural_order(self):
        result = server.execute(self.request(sort="Chromosome", direction="asc"))
        self.assertEqual([row["Gene"] for row in result["items"]], ["GENE4", "GENE1", "GENE2", "GENE3"])

    def test_numeric_range_filter_handles_multi_value_annotations(self):
        result = server.execute(self.request(filters=[{"column": "Position", "operator": "between", "value": [8, 11]}]))
        self.assertEqual([row["Gene"] for row in result["items"]], ["GENE1"])

    def test_prepare_persists_versioned_assessment_for_every_source_row(self):
        prepared = server.prepare_automatic_acmg(self.request())
        lines = Path(prepared["assessmentFile"]).read_text(encoding="utf-8").splitlines()
        self.assertEqual(prepared["profile"], "acmg-snv-points-v2")
        self.assertEqual(len(lines), 4)
        records = [__import__("json").loads(line) for line in lines]
        by_id = {record["rowId"]: record for record in records}
        pathogenic_candidate = by_id[server.row_id(self.dataset, self.fingerprint, self.ordinals["GENE1"])]
        self.assertEqual(pathogenic_candidate["assessment"]["criteria"][0]["code"], "PP3")
        self.assertTrue(all(record["profileVersion"] == prepared["profile"] for record in records))

    def test_unadjusted_boolean_and_automatic_fields_are_filterable(self):
        self.assertEqual(server.execute(self.request(filters=[{"column": "reviewed", "operator": "equals", "value": False}]))["total"], 4)
        self.assertEqual(server.execute(self.request(filters=[{"column": "acmgScore", "operator": "gte", "value": 1}]))["total"], 1)

    def test_invalid_transcript_and_score_are_never_scored(self):
        for transcript in (None, "", "ENST1&ENST2", "."):
            self.assertEqual(server.auto_acmg({"Type": "SNP", "Consequence": "missense_variant", "Transcript": transcript, "AlphaMissense_AM": "0.995"})["score"], 0)
        for value in ("nan", "inf", "1.5", "-0.1", "0.8&0.9"):
            self.assertEqual(server.auto_acmg({"Type": "SNP", "Consequence": "missense_variant", "Transcript": "ENST000001", "AlphaMissense_AM": value})["score"], 0)

    def test_row_membership_and_effective_export(self):
        import csv
        row = server.row_id(self.dataset, self.fingerprint, self.ordinals["GENE1"])
        self.assertEqual(server.execute(self.request(rowId=row))["total"], 1)
        self.assertEqual(server.execute(self.request(rowId="c" * 64))["total"], 0)
        path = server.execute(self.request(filters=[{"column": "acmgScore", "operator": "gte", "value": 1}]), export=True)
        try:
            with path.open(encoding="utf-8") as stream: rows = list(csv.DictReader(stream))
            self.assertEqual(len(rows), 1)
            self.assertEqual(rows[0]["reviewed"], "false")
            self.assertEqual(rows[0]["acmgScore"], "4")
            self.assertEqual(rows[0]["row_id"], row)
        finally: path.unlink()

    def test_cnv_assessment_is_filterable_and_exported(self):
        row = server.row_id(self.dataset, self.fingerprint, self.ordinals["GENE1"])
        request = self.request(table="cnv-segment", overlays=[{"rowId":row,"version":1,"payload":{"cnvAssessment":{"cnvId":row,"classification":"Pathogenic","totalScore":1.2}}}], filters=[{"column":"cnvClassification","operator":"equals","value":"Pathogenic"}])
        self.assertEqual(server.execute(request)["total"], 1)
        self.assertEqual(server.execute({**request,"filters":[{"column":"cnvScore","operator":"gte","value":1}]})["total"], 1)

    def test_go_wire_null_lists_are_treated_as_empty(self):
        self.assertEqual(server.execute(self.request(filters=None, overlays=None))["total"],4)
        row=server.row_id(self.dataset,self.fingerprint,self.ordinals["GENE1"])
        self.assertEqual(server.execute(self.request(filters=None, overlays=None, rowId=row))["total"],1)

    def test_sql_and_python_assessments_have_identical_gates(self):
        for transcript,score in [("ENST000001.1","0.995"),("ENST1&ENST2","0.995"),("ENST000001","inf"),("ENST000001","-0.1"),("ENST000001","0.15")]:
            conn=duckdb.connect(":memory:")
            conn.execute("CREATE TABLE t(Type VARCHAR,Consequence VARCHAR,Transcript VARCHAR,AlphaMissense_AM VARCHAR)")
            conn.execute("INSERT INTO t VALUES ('SNP','missense_variant',?,?)",[transcript,score])
            actual=__import__('json').loads(conn.execute("SELECT "+server.automatic_expr({"Type","Consequence","Transcript","AlphaMissense_AM"},"snv-indel")+" FROM t").fetchone()[0])
            expected=server.auto_acmg({"Type":"SNP","Consequence":"missense_variant","Transcript":transcript,"AlphaMissense_AM":score})
            self.assertEqual(actual['score'],expected['score'])
            self.assertEqual(actual['state'],expected['state'])
            conn.close()

    def test_query_rejects_arbitrary_paths(self):
        outside = self.root.parent / "outside.parquet"
        with self.assertRaises(ValueError):
            server.execute(self.request(filePath=str(outside)))


if __name__ == "__main__":
    unittest.main()
