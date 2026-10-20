package service

import (
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"sync"
	"testing"
)

func TestCVMEventVersionUsesRecoveryColumn(t *testing.T) {
	parsed, err := schema.Parse(&model.Task{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if name := parsed.LookUpField("LastCVMEventVersion").DBName; name != "last_cvm_event_version" {
		t.Fatalf("event fence stored in %q", name)
	}
}
func TestCVMCallbackDoesNotAcknowledgeDatabaseFailure(t *testing.T) {
	transient := errors.New("temporary database failure")
	for _, tc := range []struct {
		name, phase string
		lookup      error
		wantError   bool
	}{
		{"terminal database failure", "terminal", transient, true},
		{"deleted terminal replay", "terminal", gorm.ErrRecordNotFound, false},
		{"missing active task", "running", gorm.ErrRecordNotFound, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			old := database.DB
			database.DB = db
			defer func() { database.DB = old }()
			svc := &TaskService{cfg: &config.Config{}, repo: repository.NewTaskRepository()}
			mock.ExpectQuery(`SELECT \* FROM "tasks"`).WillReturnError(tc.lookup)
			state := "TERMINATED"
			if tc.phase != "terminal" {
				state = "RUNNING"
			}
			err = svc.HandleCVMStateEvent(model.CVMStateEvent{TaskUUID: "task", AttemptID: "attempt", Version: 101, InstanceState: state, ExecutionPhase: tc.phase})
			if (err != nil) != tc.wantError {
				t.Fatalf("callback error=%v", err)
			}
			if tc.lookup == transient && !errors.Is(err, transient) {
				t.Fatalf("database failure lost: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
