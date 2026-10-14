package database

import (
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestRetentionScopeCoversListsCountsAndContent(t *testing.T) {
	connection, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{DisableAutomaticPing: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	if err := db.Callback().Query().Before("gorm:query").Register("test:expiry", func(tx *gorm.DB) { retentionScope(now, tx) }); err != nil {
		t.Fatal(err)
	}
	var count int64
	for _, query := range []*gorm.DB{db.Model(&model.Task{}).Count(&count), db.Table("tasks").Count(&count), db.Model(&model.Sample{}).Count(&count)} {
		if !strings.Contains(query.Statement.SQL.String(), `"expires_at" >`) {
			t.Fatalf("unfenced count: %s", query.Statement.SQL.String())
		}
	}
	content := db.Model(&model.ResultRowAdjustment{}).Count(&count)
	if !strings.Contains(content.Statement.SQL.String(), "EXISTS (SELECT 1 FROM tasks retention_parent") || !strings.Contains(content.Statement.SQL.String(), `"result_row_adjustments"."task_uuid"`) {
		t.Fatalf("unfenced content: %s", content.Statement.SQL.String())
	}
	cleanup := db.Unscoped().Model(&model.Task{}).Count(&count)
	if strings.Contains(cleanup.Statement.SQL.String(), "expires_at") {
		t.Fatal("cleanup cannot inspect expired rows")
	}
	for _, value := range []interface{}{&model.CNVBaseline{}, &model.CVMSubmission{}, &model.CVMCancellation{}, &model.BAMRetentionJob{}} {
		query := db.Model(value).Count(&count)
		if strings.Contains(query.Statement.SQL.String(), "retention_parent") {
			t.Fatalf("resource or settlement fenced after task expiry: %s", query.Statement.SQL.String())
		}
	}
}

func TestRetentionWriteFenceModelsHaveTaskScope(t *testing.T) {
	connection, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{DisableAutomaticPing: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := installRetentionWriteFences(db); err != nil {
		t.Fatal(err)
	}
}
