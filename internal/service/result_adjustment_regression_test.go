package service

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

func TestAdjustmentAtomicCreateAndVersionGuard(t *testing.T) {
	for _, version := range []uint64{0, 1} {
		for _, affected := range []int64{0, 1} {
			db, mock := newUploadTransactionTestDB(t)
			db = db.Session(&gorm.Session{SkipDefaultTransaction: true})
			saved := model.ResultRowAdjustment{TenantID: "org", TaskUUID: "task", ExecutionAttemptID: "attempt", Table: "snv-indel", RowID: "row", DatasetVersion: "hash", PayloadJSON: `{"reviewed":true}`, Version: version + 1}
			if version == 0 {
				mock.ExpectExec(`INSERT INTO "result_row_adjustments".*ON CONFLICT DO NOTHING`).WillReturnResult(sqlmock.NewResult(0, affected))
			} else {
				mock.ExpectExec(`UPDATE "result_row_adjustments".*AND version=`).WillReturnResult(sqlmock.NewResult(0, affected))
			}
			err := persistParquetAdjustment(db, &saved, version)
			if affected == 0 && !errors.Is(err, ErrAdjustmentConflict) {
				t.Fatalf("lost concurrent edit: %v", err)
			}
			if affected == 1 && err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestAdjustmentResponseUsesObjectsAndPreservesComputedClass(t *testing.T) {
	data, err := json.Marshal(model.ResultRowAdjustment{PayloadJSON: `{"acmgClassification":"Pathogenic","acmgClassificationComputed":"VUS"}`})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	_ = json.Unmarshal(data, &decoded)
	if _, ok := decoded["adjustments"].(map[string]interface{}); !ok {
		t.Fatal("adjustments serialized as a string")
	}
	item := normalizeParquetAPIItem("snv-indel", map[string]interface{}{"__row_id": "row", "__adjustments": map[string]interface{}{"acmgClassification": "Pathogenic", "acmgOverride": "Pathogenic", "acmgEvidence": []interface{}{}, "acmgClassificationComputed": "VUS"}, "__acmg": map[string]interface{}{"classification": "Likely_Benign"}})
	if item["acmgClassificationComputed"] != "VUS" || item["acmgClassification"] != "Pathogenic" {
		t.Fatalf("lost baseline: %#v", item)
	}
}

func TestLegacyMappingRequiresFullIdentity(t *testing.T) {
	old := map[string]interface{}{"chromosome": "chr1", "position": float64(10), "ref": "A", "alt": "T", "gene": "GENE1", "transcript": "ENST000001"}
	row := map[string]interface{}{"chromosome": "chr1", "position": "10", "ref": "A", "alt": "T", "gene": "GENE1", "transcript": "ENST000001"}
	if !sameLegacyIdentity(old, row, legacyIdentityFields("snv-indel")) {
		t.Fatal("exact identity rejected")
	}
	row["transcript"] = "ENST000002"
	if sameLegacyIdentity(old, row, legacyIdentityFields("snv-indel")) {
		t.Fatal("different transcript transferred review")
	}
	delete(row, "transcript")
	if sameLegacyIdentity(old, row, legacyIdentityFields("snv-indel")) {
		t.Fatal("missing identity guessed")
	}
}
