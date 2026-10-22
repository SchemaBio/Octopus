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
	if err := db.Exec("CREATE TABLE result_snv_indels (id varchar(36) PRIMARY KEY, amino_acids varchar(50))").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO result_snv_indels (id,amino_acids) VALUES ('existing','A/V')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&SNVIndel{}); err != nil {
		t.Fatal(err)
	}
	peptide := strings.Repeat("ACDEFGHIKLMNPQRSTVWY", 6) + "/-"
	want := SNVIndel{ID: "long-indel", AminoAcids: peptide}
	if err := db.Create(&want).Error; err != nil {
		t.Fatal(err)
	}
	var got, existing SNVIndel
	if err := db.First(&got, "id = ?", want.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.AminoAcids != peptide {
		t.Fatal("long Indel peptide was truncated")
	}
	if err := db.First(&existing, "id = ?", "existing").Error; err != nil || existing.AminoAcids != "A/V" {
		t.Fatalf("migration changed existing annotation: %v", err)
	}
}
