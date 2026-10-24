package model

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSNVLongPeptideMigrationPreservesAnnotation(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN must point to an isolated PostgreSQL test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	schema := fmt.Sprintf("test_snv_peptide_%d", time.Now().UnixNano())
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if err := db.Exec("SET search_path TO " + schema).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE result_snv_indels (id varchar(36) PRIMARY KEY, amino_acids varchar(50), variant_id varchar(100))").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO result_snv_indels (id,amino_acids,variant_id) VALUES ('existing','A/V','1_123_A_G')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&SNVIndel{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE variant_review_events (id varchar(36) PRIMARY KEY, variant_id varchar(100) NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&VariantReviewEvent{}); err != nil {
		t.Fatal(err)
	}
	peptide := strings.Repeat("ACDEFGHIKLMNPQRSTVWY", 6) + "/-"
	variantID := "1_248801611_C_C" + strings.Repeat("ACGT", 30)
	want := SNVIndel{ID: "long-indel", AminoAcids: peptide, VariantID: variantID}
	if err := db.Create(&want).Error; err != nil {
		t.Fatal(err)
	}
	var got, existing SNVIndel
	if err := db.First(&got, "id = ?", want.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.AminoAcids != peptide || got.VariantID != variantID {
		t.Fatal("long Indel peptide or variant identity was truncated")
	}
	if err := db.First(&existing, "id = ?", "existing").Error; err != nil || existing.AminoAcids != "A/V" || existing.VariantID != "1_123_A_G" {
		t.Fatalf("migration changed existing annotation: %v", err)
	}
	// The review audit uses the same external variant identity as the result row.
	event := VariantReviewEvent{ID: "long-review", TenantID: "test", TaskUUID: "task", ExecutionAttemptID: "attempt", VariantType: "snv_indel", VariantID: variantID, VariantFingerprint: "fingerprint", HistoryGroupKey: "group", Action: VariantReviewActionReviewed, VariantSnapshotJSON: "{}"}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	var reviewedID string
	if err := db.Raw("SELECT variant_id FROM variant_review_events LIMIT 1").Scan(&reviewedID).Error; err != nil || reviewedID != variantID {
		t.Fatalf("review identity changed: %q %v", reviewedID, err)
	}
}
