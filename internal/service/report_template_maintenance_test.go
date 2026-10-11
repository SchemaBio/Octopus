package service

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReportTemplateMutationsPreserveMaintenancePermission(t *testing.T) {
	for _, active := range []bool{false, true} {
		sqlDB, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		previous := database.DB
		database.DB = db
		svc := &ReportService{templateRepo: repository.NewReportTemplateRepository()}
		actor := model.OverlayActor{UserID: 42, Role: string(model.SystemRoleUser)}
		for _, revision := range []uint64{1, 2} {
			mock.ExpectQuery(`SELECT .*FROM "report_templates"`).WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "name", "revision", "is_active", "api_endpoint", "api_key"}).AddRow("report-1", 42, "report", revision, active, "https://not-deployed.invalid/report", "test-key"))
			mock.ExpectBegin()
			mock.ExpectExec(`UPDATE "report_templates"`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
		}
		status, err := svc.SetTemplateActive(42, "report-1", active, actor)
		if err != nil || status == nil || !status.CanMaintain || status.Revision != 2 || status.IsActive != active {
			t.Fatalf("status response lost maintenance permission: result=%+v err=%v", status, err)
		}
		updated, err := svc.UpdateTemplate(42, "report-1", &model.ReportTemplateUpdateRequest{ExpectedRevision: 2, Description: "updated"}, actor)
		if err != nil || updated == nil || !updated.CanMaintain || updated.Revision != 3 {
			t.Fatalf("edit response lost maintenance permission: result=%+v err=%v", updated, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		database.DB = previous
		_ = sqlDB.Close()
	}
}
