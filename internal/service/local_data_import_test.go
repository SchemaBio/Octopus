package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"os"
	"path/filepath"
	"testing"
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
			root, one, two := localPairFixture(t)
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
			if scenario == "success" && (len(assets) != 2 || assets[0].InternalID != "INT-001") {
				t.Fatal("missing pair metadata")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestSaaSLocalImportAlwaysDisabled(t *testing.T) {
	svc := NewDataAssetService(&config.Config{Edition: "saas"})
	if _, err := svc.ImportLocalPair(context.Background(), &model.LocalDataImportRequest{}, model.OverlayActor{UserID: 1, Role: "SUPER_ADMIN", SharedInstance: true}); err == nil {
		t.Fatal("SaaS import enabled")
	}
}
