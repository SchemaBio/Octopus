package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func permissionTestDB(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	old := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = old; _ = conn.Close() })
	return mock
}
func expectPermissionUser(mock sqlmock.Sqlmock, role, grants string) {
	mock.ExpectQuery(`SELECT .*users.*`).WillReturnRows(sqlmock.NewRows([]string{"id", "email", "system_role", "is_active", "permissions"}).AddRow(1, "test@example.invalid", role, true, grants))
}
func permissionRouter(route, method, edition string, wrote *bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, route, func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Set("email", "test@example.invalid")
		c.Set("role", "USER")
		if !enforceBusinessPermissions(c, &config.Config{Edition: edition}) {
			return
		}
		*wrote = true
		c.Status(204)
	})
	return r
}
func TestPermissionRevocationUsesCurrentDatabaseState(t *testing.T) {
	mock := permissionTestDB(t)
	wrote := false
	r := permissionRouter("/api/v1/samples", "POST", "opensource", &wrote)
	for _, grants := range []string{`["samples.write"]`, `[]`} {
		expectPermissionUser(mock, "USER", grants)
		w := httptest.NewRecorder()
		wrote = false
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/samples", strings.NewReader(`{}`)))
		if grants == `[]` {
			if w.Code != 403 || wrote || !strings.Contains(w.Body.String(), `"required_permission":"samples.write"`) {
				t.Fatalf("revocation failed: %d %s", w.Code, w.Body.String())
			}
		} else if w.Code != 204 || !wrote {
			t.Fatal("grant failed")
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAdministratorAndSaaSBypassBusinessGrants(t *testing.T) {
	mock := permissionTestDB(t)
	for _, edition := range []string{"opensource", "saas"} {
		if edition == "opensource" {
			expectPermissionUser(mock, "SUPER_ADMIN", `[]`)
		}
		wrote := false
		r := permissionRouter("/api/v1/gene-lists", "POST", edition, &wrote)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/gene-lists", strings.NewReader(`{}`)))
		if w.Code != 204 || !wrote {
			t.Fatalf("%s rejected", edition)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestBaselineUsesStoredTaskType(t *testing.T) {
	mock := permissionTestDB(t)
	for _, template := range []string{"baseline_fix", "single"} {
		mock.ExpectQuery(`SELECT .*tasks.*`).WillReturnRows(sqlmock.NewRows([]string{"uuid", "template", "external_org_id"}).AddRow("task-1", template, ""))
		expectPermissionUser(mock, "USER", `["baselines.manage"]`)
		wrote := false
		r := permissionRouter("/api/v1/tasks/:id/start", "POST", "opensource", &wrote)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tasks/task-1/start", strings.NewReader(`{"template":"baseline_fix"}`)))
		if (w.Code == 204) != (template == "baseline_fix") {
			t.Fatalf("task type not enforced: %s %d", template, w.Code)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestUploadTypeCannotBeSpoofed(t *testing.T) {
	mock := permissionTestDB(t)
	expectPermissionUser(mock, "USER", `["bed.manage"]`)
	wrote := false
	r := permissionRouter("/api/v1/upload/jobs", "POST", "opensource", &wrote)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/upload/jobs", strings.NewReader(`{"file_type":"fastq_single","files":[{"read_type":"single"}]}`)))
	if w.Code != 403 || wrote {
		t.Fatal("BED grant allowed sequencing upload")
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/upload/jobs", strings.NewReader(`{"file_type":"other","files":[{"read_type":"bed"}]}`)))
	if w.Code != 400 || wrote {
		t.Fatal("forged file type accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestReadOperationsRequireNoWriteGrantAndCannotBindScope(t *testing.T) {
	for _, endpoint := range []string{"/api/v1/tasks/estimate", "/api/v1/tasks/batch/preview", "/api/v1/tasks/:id/results/tables/:table/query", "/api/v1/tasks/:id/results/tables/:table/export"} {
		wrote := false
		r := permissionRouter(endpoint, "POST", "opensource", &wrote)
		path := strings.ReplaceAll(strings.ReplaceAll(endpoint, ":id", "t"), ":table", "snv")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{}`)))
		if w.Code != 204 {
			t.Fatalf("read blocked: %s %d", endpoint, w.Code)
		}
	}
}

func TestEveryBusinessGrantIsIndependent(t *testing.T) {
	endpoints := []struct{ path, body, grant string }{
		{"/samples", `{}`, "samples.write"},
		{"/upload/jobs", `{"file_type":"fastq_single","files":[{"read_type":"single"}]}`, "data.manage"},
		{"/tasks", `{}`, "tasks.manage"},
		{"/pipelines", `{}`, "pipelines.manage"},
		{"/gene-lists", `{}`, "gene_lists.manage"},
		{"/upload/jobs", `{"file_type":"bed","files":[{"read_type":"bed"}]}`, "bed.manage"},
		{"/cnv-baselines", `{}`, "baselines.manage"},
		{"/report-templates", `{}`, "report_services.manage"},
		{"/ai-assistance/requests", `{}`, "interpretation_reports.manage"},
	}
	for _, endpoint := range endpoints {
		t.Run(endpoint.grant, func(t *testing.T) {
			mock := permissionTestDB(t)
			for _, granted := range []bool{false, true} {
				grants := `[]`
				if granted {
					grants = `["` + endpoint.grant + `"]`
				}
				expectPermissionUser(mock, "USER", grants)
				wrote := false
				router := permissionRouter("/api/v1"+endpoint.path, "POST", "opensource", &wrote)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1"+endpoint.path, strings.NewReader(endpoint.body)))
				if granted {
					if response.Code != 204 || !wrote {
						t.Fatalf("grant failed: %d", response.Code)
					}
				} else if response.Code != 403 || wrote {
					t.Fatalf("unauthorized side effect: %d", response.Code)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
