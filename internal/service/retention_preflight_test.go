package service

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestRetentionPreflightScansExistingExpiredResources(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	task := "11111111-1111-4111-8111-111111111111"
	mock.ExpectQuery("SELECT has_schema_privilege").WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(true))
	mock.ExpectQuery("SELECT uuid,external_org_id AS org_id,created_at.*FROM tasks").WithArgs(now).WillReturnRows(sqlmock.NewRows([]string{"uuid", "org_id", "expires_at"}).AddRow(task, "tenant-a", now.Add(-time.Hour)))
	mock.ExpectQuery("SELECT uuid,external_org_id AS org_id,created_at.*FROM samples").WithArgs(now).WillReturnRows(sqlmock.NewRows([]string{"uuid", "org_id", "expires_at"}).AddRow("sample", "tenant-a", now.Add(-time.Hour)))
	mock.ExpectQuery("SELECT count.*information_schema.tables").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	out, err := ResearchRetentionPreflight(context.Background(), db, &config.Config{Edition: "saas"}, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if !out.ReadOnly || !out.SchemaCreateAllowed || len(out.Resources) != 2 || out.Resources[0].UUID != task || out.Resources[0].Prefix == "" || out.Resources[1].Kind != "sample" {
		t.Fatalf("incorrect preflight: %#v", out)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
