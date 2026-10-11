package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAIGenerationUsesTaskInterpretationGuard(t *testing.T) {
	for _, tc := range []struct {
		name, org string
		closed    bool
		status    int
	}{{"completed task", "org", true, 409}, {"other tenant", "other", false, 404}, {"open task", "org", false, 204}} {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB, mock, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			defer sqlDB.Close()
			db, e := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
			if e != nil {
				t.Fatal(e)
			}
			old := database.DB
			database.DB = db
			defer func() { database.DB = old }()
			rows := func(closed bool) *sqlmock.Rows {
				var at any
				if closed {
					at = time.Now()
				}
				return sqlmock.NewRows([]string{"id", "uuid", "external_org_id", "interpretation_completed_at"}).AddRow("id", "task", "org", at)
			}
			mock.ExpectQuery(`SELECT .*tasks`).WithArgs("task", 1).WillReturnRows(rows(false))
			if tc.org == "org" {
				mock.ExpectExec(`SELECT pg_advisory_lock`).WithArgs("task-interpretation:task").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery(`SELECT .*tasks`).WithArgs("task", 1).WillReturnRows(rows(tc.closed))
				mock.ExpectQuery(`SELECT pg_advisory_unlock`).WithArgs("task-interpretation:task").WillReturnRows(sqlmock.NewRows([]string{"unlocked"}).AddRow(true))
			}
			h := &AIAssistanceHandler{}
			guard := &TaskHandler{taskRepo: repository.NewTaskRepository()}
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", uint(1))
				c.Set("email", "doctor")
				c.Set("role", "user")
				c.Set("org_id", tc.org)
			})
			executed := false
			router.POST("/ai-assistance/requests", h.DecodeInput, guard.InterpretationEditGuard, func(c *gin.Context) { executed = true; c.Status(204) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", "/ai-assistance/requests", strings.NewReader(`{"feature":"acmg","taskId":"task"}`)))
			if response.Code != tc.status || executed != (tc.status == 204) {
				t.Fatal(response.Code, executed, response.Body.String())
			}
			if e = mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestAIDecodeRejectsVariantsWithoutTask(t *testing.T) {
	router := gin.New()
	h := &AIAssistanceHandler{}
	router.POST("/requests", h.DecodeInput, func(c *gin.Context) { c.Status(204) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/requests", strings.NewReader(`{"feature":"cnv_gain"}`)))
	if response.Code != 400 {
		t.Fatal(response.Code)
	}
}
