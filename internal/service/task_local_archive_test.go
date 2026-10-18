package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/sepiida"
)

func TestLocalImportWaitsForArchiveCompletion(t *testing.T) {
	archiveDir := t.TempDir()
	var archived atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(model.SepiidaWorkflowResponse{Workflow: model.SepiidaWorkflow{
			UUID: "task", Status: model.SepiidaStatusSuccess, Archived: archived.Load(),
		}})
	}))
	defer server.Close()
	s := &TaskService{
		cfg:     &config.Config{Edition: "opensource", Task: config.TaskConfig{ArchiveDir: filepath.Dir(archiveDir)}},
		sepiida: sepiida.NewClient(server.URL, "test-query-key"),
	}
	task := &model.Task{UUID: filepath.Base(archiveDir), Executor: model.ExecutorLocal, Status: model.TaskStatusCompleted, ResultImportStatus: model.ResultImportStatusPending}
	// The directory exists, but the Agent is still copying files. No repository
	// is supplied: accidentally starting an import would fail this test.
	s.importTaskArchive(task)
	if task.ResultImportAttempts != 0 || task.ResultImportStatus != model.ResultImportStatusPending {
		t.Fatal("waiting for archive completion must not create a failed import")
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "outputs.resolved.json"), []byte(`{"summary":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if s.localArchiveReady(task, archiveDir) {
		t.Fatal("a manifest alone must not bypass Agent archive completion")
	}
	archived.Store(true)
	if !s.localArchiveReady(task, archiveDir) {
		t.Fatal("completed archive should become importable on the next poll")
	}
	if err := os.Remove(filepath.Join(archiveDir, "outputs.resolved.json")); err != nil {
		t.Fatal(err)
	}
	if !s.localArchiveReady(task, archiveDir) {
		t.Fatal("an acknowledged but corrupt archive must reach the importer and report its real error")
	}
}

func TestLocalArchiveBarrierDoesNotChangeSaaS(t *testing.T) {
	s := &TaskService{cfg: &config.Config{Edition: "saas"}}
	if !s.localArchiveReady(&model.Task{Executor: model.ExecutorCVM}, t.TempDir()) {
		t.Fatal("CVM keeps its existing archive validation and staging")
	}
}
