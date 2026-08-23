package handler

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newDashboardScopeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return db
}

func newDashboardErrorTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return db, mock
}

func dashboardScopeSQL(t *testing.T, db *gorm.DB, destination any) string {
	t.Helper()
	return db.Session(&gorm.Session{DryRun: true}).Find(destination).Statement.SQL.String()
}

func TestDashboardStandaloneScopesExcludeOrganizationRecords(t *testing.T) {
	db := newDashboardScopeTestDB(t)

	taskSQL := dashboardScopeSQL(t,
		taskDashboardScope(db.Model(&model.Task{}), 42, "", false, false),
		&[]model.Task{},
	)
	if !strings.Contains(taskSQL, "tasks.external_org_id = '' AND tasks.created_by =") {
		t.Fatalf("task dashboard SQL is not standalone-scoped: %q", taskSQL)
	}

	sampleSQL := dashboardScopeSQL(t,
		sampleDashboardScope(db.Model(&model.Sample{}), 42, "", false, false),
		&[]model.Sample{},
	)
	if !strings.Contains(sampleSQL, "samples.external_org_id = '' AND samples.created_by =") {
		t.Fatalf("sample dashboard SQL is not standalone-scoped: %q", sampleSQL)
	}
}

func TestDashboardOrganizationScopeUsesCurrentOrganization(t *testing.T) {
	db := newDashboardScopeTestDB(t)
	sql := dashboardScopeSQL(t,
		sampleDashboardScope(db.Model(&model.Sample{}), 42, "org-current", true, false),
		&[]model.Sample{},
	)
	if !strings.Contains(sql, "samples.external_org_id =") {
		t.Fatalf("sample dashboard SQL is not organization-scoped: %q", sql)
	}
	if strings.Contains(sql, "samples.created_by") {
		t.Fatalf("organization dashboard must include the whole organization, got %q", sql)
	}
}

func TestLoadDashboardStatsReturnsDatabaseErrorInsteadOfZeroes(t *testing.T) {
	db, mock := newDashboardErrorTestDB(t)
	dbErr := errors.New("database unavailable")
	mock.ExpectQuery(`SELECT count\(\*\) FROM "samples" WHERE samples\.external_org_id = \$1`).
		WithArgs("org-1").
		WillReturnError(dbErr)

	stats, err := loadDashboardStats(db, 42, "org-1", true, false)
	if !errors.Is(err, dbErr) {
		t.Fatalf("loadDashboardStats error = %v, want database error", err)
	}
	if stats != (model.DashboardStats{}) {
		t.Fatalf("stats = %+v, want zero value on error", stats)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
