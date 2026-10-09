package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestInterpretationGuardClassifiesEdits(t *testing.T) {
	for _, route := range []string{"/tasks/:id", "/tasks/:id/retry", "/tasks/:id/reports", "/tasks/:id/results/rows/:table/:vid", "/tasks/:id/results/:type/:vid/review", "/tasks/:id/results/:type/:vid/report", "/tasks/:id/results/cnv-assessments/:type/:vid", "/archive/:uuid/import"} {
		if !isTaskEditRequest(http.MethodPut, route) {
			t.Errorf("edit exempted: %s", route)
		}
	}
	for _, route := range []string{"/tasks/:id/results/igv/snapshot", "/tasks/:id/results/igv/urls", "/tasks/:id/results/tables/:table/query", "/tasks/:id/results/tables/:table/views", "/tasks/:id/downloads/issue", "/tasks/:id/results/assessment/context"} {
		if isTaskEditRequest(http.MethodPost, route) {
			t.Errorf("viewing blocked: %s", route)
		}
	}
	if isTaskEditRequest(http.MethodGet, "/tasks/:id/reports") {
		t.Fatal("read blocked")
	}
}

func TestInterpretationGuardChecksFreshStateAndReleasesLock(t *testing.T) {
	for _, tt := range []struct {
		name, route, org string
		closed           bool
		code             int
	}{
		{"stale browser mutation", "/tasks/:id/results/rows/:table/:vid", "org", true, 409},
		{"unlock allowed", "/tasks/:id/interpretation-completion", "org", true, 204},
		{"open task", "/tasks/:id/results/rows/:table/:vid", "org", false, 204},
		{"cross tenant", "/tasks/:id/results/rows/:table/:vid", "another", true, 404},
		{"archive mutation", "/archive/:uuid/import", "org", true, 409},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			prior := database.DB
			database.DB = db
			defer func() { database.DB = prior }()
			rows := func(closed bool) *sqlmock.Rows {
				var at interface{}
				if closed {
					at = time.Now()
				}
				return sqlmock.NewRows([]string{"id", "uuid", "external_org_id", "interpretation_completed_at"}).AddRow("id", "task", "org", at)
			}
			// The first authorization sees an open task; a different client closes it
			// before this request obtains the cross-replica lock.
			mock.ExpectQuery(`SELECT .*tasks`).WithArgs("task", 1).WillReturnRows(rows(false))
			if tt.org == "org" {
				mock.ExpectExec(`SELECT pg_advisory_lock`).WithArgs("task-interpretation:task").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery(`SELECT .*tasks`).WithArgs("task", 1).WillReturnRows(rows(tt.closed))
				mock.ExpectQuery(`SELECT pg_advisory_unlock`).WithArgs("task-interpretation:task").WillReturnRows(sqlmock.NewRows([]string{"unlocked"}).AddRow(true))
			}
			h := &TaskHandler{taskRepo: repository.NewTaskRepository()}
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("user_id", uint(42))
				c.Set("email", "doctor")
				c.Set("role", "user")
				c.Set("org_id", tt.org)
			}, h.InterpretationEditGuard)
			executed := false
			r.PUT(tt.route, func(c *gin.Context) { executed = true; c.Status(204) })
			path := "/tasks/task/results/rows/snv/variant"
			if tt.route == "/tasks/:id/interpretation-completion" {
				path = "/tasks/task/interpretation-completion"
			}
			if tt.route == "/archive/:uuid/import" {
				path = "/archive/task/import"
			}
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(http.MethodPut, path, nil))
			if response.Code != tt.code || executed != (tt.code == 204) {
				t.Fatalf("code=%d executed=%v body=%s", response.Code, executed, response.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
