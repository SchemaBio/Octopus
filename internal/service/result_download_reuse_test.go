package service

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"regexp"
	"testing"
	"time"
)

func TestDownloadQuoteReusesCurrentPaidOrPendingGrant(t *testing.T) {
	for _, paid := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "paid"}[paid], func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			previous := database.DB
			database.DB = db
			defer func() { database.DB = previous }()
			task := &model.Task{UUID: "task", ExternalOrgID: "org"}
			fresh := &model.ResultDownload{ID: "new-grant", TaskUUID: "task", AttemptID: "attempt", OrgID: "org", UserID: 7, Kind: "zip", ObjectKey: "result.zip", ETag: "etag", SizeBytes: 100}
			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT "id" FROM "tasks"`)).WithArgs("task", "org", 1).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			var charged interface{}
			if paid {
				charged = time.Now().Add(-time.Minute)
			}
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "result_downloads"`)).WithArgs("task", "attempt", "org", uint(7), "zip", "result.zip", "etag", int64(100), sqlmock.AnyArg(), sqlmock.AnyArg(), 1).WillReturnRows(sqlmock.NewRows([]string{"id", "task_uuid", "attempt_id", "org_id", "user_id", "kind", "charged_at"}).AddRow("existing-grant", "task", "attempt", "org", 7, "zip", charged))
			mock.ExpectCommit()
			reused, err := reuseOrCreateDownloadQuote(context.Background(), task, fresh)
			if err != nil {
				t.Fatal(err)
			}
			if reused.ID != "existing-grant" {
				t.Fatal("created a new billing identity for an existing grant")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
