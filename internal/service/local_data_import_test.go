package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
)

func localPairFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	one, two := filepath.Join(root, "r1.fastq.gz"), filepath.Join(root, "r2.fastq.gz")
	for i, path := range []string{one, two} {
		var raw bytes.Buffer
		z := gzip.NewWriter(&raw)
		suffix := "1"
		if i == 1 {
			suffix = "2"
		}
		z.Write([]byte("@read/" + suffix + "\nACGT\n+\nIIII\n"))
		z.Close()
		if err := os.WriteFile(path, raw.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root, one, two
}
func TestLocalImportValidatesCompleteGzipPairAndAssignsUUIDs(t *testing.T) {
	root, one, two := localPairFixture(t)
	assets, err := validateLocalPair(context.Background(), root, one, two)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 || assets[0].UUID == assets[1].UUID || len(assets[0].UUID) != 36 || assets[0].ValidationSHA256 == "" || assets[0].FileSize <= 0 || assets[1].ReadType != model.ReadTypeRead2 {
		t.Fatalf("invalid registered metadata: %#v", assets)
	}
}
func TestLocalImportRejectsCorruptionUnpairedFilesAndInvalidPaths(t *testing.T) {
	for _, scenario := range []string{"crc", "truncated", "mismatch", "swapped", "same", "outside", "relative", "format", "empty", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root, one, two := localPairFixture(t)
			ctx := context.Background()
			switch scenario {
			case "crc":
				raw, _ := os.ReadFile(two)
				raw[len(raw)-8] ^= 0xff
				os.WriteFile(two, raw, 0600)
			case "truncated":
				raw, _ := os.ReadFile(two)
				os.WriteFile(two, raw[:len(raw)-5], 0600)
			case "mismatch":
				two = filepath.Join(root, "mismatch.fastq")
				os.WriteFile(two, []byte("@different\nACGT\n+\nIIII\n"), 0600)
			case "swapped":
				one, two = two, one
			case "same":
				two = one
			case "outside":
				root = t.TempDir()
			case "relative":
				one = "r1.fastq.gz"
			case "format":
				two = filepath.Join(root, "invalid.fastq")
				os.WriteFile(two, []byte("@read\nACGT\n+\nIII\n"), 0600)
			case "empty":
				os.WriteFile(two, nil, 0600)
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			assets, err := validateLocalPair(ctx, root, one, two)
			if err == nil || assets != nil {
				t.Fatal("invalid pair accepted")
			}
		})
	}
}
func TestLocalImportRechecksGrantsAndRegistersPairAtomically(t *testing.T) {
	for _, scenario := range []string{"success", "revoked", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			one := filepath.Join(root, "future", "r1.fastq.gz")
			two := filepath.Join(root, "future", "r2.fastq.gz")
			db, mock := newUploadTransactionTestDB(t)
			previous := database.DB
			database.DB = db
			t.Cleanup(func() { database.DB = previous })
			mock.ExpectBegin()
			grants := `["data.manage","samples.write"]`
			if scenario == "revoked" {
				grants = `[]`
			}
			mock.ExpectQuery(`SELECT .*users.*`).WillReturnRows(sqlmock.NewRows([]string{"id", "system_role", "is_active", "permissions"}).AddRow(1, "USER", true, grants))
			if scenario != "revoked" {
				count := 0
				if scenario == "duplicate" {
					count = 1
				}
				mock.ExpectQuery(`SELECT count\(\*\) FROM "data_assets"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
				if scenario == "success" {
					mock.ExpectQuery(`SELECT count\(\*\) FROM "data_assets"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
					mock.ExpectQuery(`INSERT INTO "data_assets"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2))
					mock.ExpectCommit()
				} else {
					mock.ExpectRollback()
				}
			} else {
				mock.ExpectRollback()
			}
			svc := NewDataAssetService(&config.Config{Edition: "opensource", Storage: config.StorageConfig{Provider: "local", LocalDir: root}})
			assets, err := svc.ImportLocalPair(context.Background(), &model.LocalDataImportRequest{InternalID: "INT-001", Read1Path: one, Read2Path: two}, model.OverlayActor{UserID: 1, Role: "SUPER_ADMIN", SharedInstance: true})
			if (err == nil) != (scenario == "success") {
				t.Fatalf("%s: %v", scenario, err)
			}
			if scenario == "success" {
				if len(assets) != 2 || assets[0].InternalID != "INT-001" {
					t.Fatal("missing pair metadata")
				}
				for _, asset := range assets {
					if asset.Status != model.FileStatusPending || asset.ValidationStatus != "pending" || asset.FileSize != 0 {
						t.Fatalf("missing file must stay pending: %#v", asset)
					}
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalFASTQWaitPublishesBothFilesOnlyAfterValidation(t *testing.T) {
	root, one, two := localPairFixture(t)
	validRead2, err := os.ReadFile(two)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(two); err != nil {
		t.Fatal(err)
	}
	pair, err := registerLocalFASTQPair(root, one, two)
	if err != nil {
		t.Fatal(err)
	}
	db, mock := newUploadTransactionTestDB(t)
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	svc := NewDataAssetService(&config.Config{Storage: config.StorageConfig{LocalDir: root}})
	observations := make(map[string]localFASTQObservation)
	poll := func(validation string) {
		t.Helper()
		rows := sqlmock.NewRows([]string{"id", "local_import_pair_id", "storage_key", "read_type"})
		for i, asset := range pair {
			rows.AddRow(i+1, *asset.LocalImportPairID, asset.StorageKey, asset.ReadType)
		}
		mock.ExpectQuery(`SELECT .*FROM "data_assets".*local_import_pair_id IS NOT NULL`).WillReturnRows(rows)
		if validation != "" {
			mock.ExpectBegin()
			for range pair {
				pattern := `UPDATE "data_assets" SET .*"validation_status"`
				if validation == "valid" {
					pattern = `UPDATE "data_assets" SET .*"status".*"validation_sha256"`
				}
				mock.ExpectExec(pattern).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectCommit()
		}
		observations = svc.checkLocalFASTQPairs(context.Background(), observations)
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	}
	poll("") // Only R1 is present.
	if len(observations) != 0 {
		t.Fatal("observed a pair while R2 was missing")
	}
	if err := os.WriteFile(two, validRead2[:len(validRead2)-5], 0600); err != nil {
		t.Fatal(err)
	}
	poll("") // First observation of the pair.
	poll("invalid")
	if !observations[*pair[0].LocalImportPairID].checked {
		t.Fatal("unchanged corrupt data should not be repeatedly validated")
	}
	poll("")
	if err := os.WriteFile(two, validRead2, 0600); err != nil {
		t.Fatal(err)
	}
	poll("") // The replacement must settle too.
	poll("valid")
	if len(observations) != 0 {
		t.Fatal("completed pair is no longer pending")
	}
}
func TestSaaSLocalImportAlwaysDisabled(t *testing.T) {
	svc := NewDataAssetService(&config.Config{Edition: "saas"})
	if _, err := svc.ImportLocalPair(context.Background(), &model.LocalDataImportRequest{}, model.OverlayActor{UserID: 1, Role: "SUPER_ADMIN", SharedInstance: true}); err == nil {
		t.Fatal("SaaS import enabled")
	}
}
