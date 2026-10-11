package database

import (
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"testing"
)

func TestReportProtocolMigrationUpgradesOnlyServiceConfigurations(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	previous := DB
	DB = db
	defer func() { DB = previous }()
	for _, affected := range []int64{2, 0} {
		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "report_templates" SET "contract_version"=\$1,"revision"=revision \+ 1,"updated_at"=\$2 WHERE contract_version IS NULL OR contract_version <> \$3`).WithArgs(model.ReportContractV2, sqlmock.AnyArg(), model.ReportContractV2).WillReturnResult(sqlmock.NewResult(0, affected))
		mock.ExpectCommit()
		if err := migrateReportProtocol(); err != nil {
			t.Fatal(err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
