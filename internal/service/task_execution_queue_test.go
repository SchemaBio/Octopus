package service

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCVMPhaseNeedsSubmission(t *testing.T) {
	for _, phase := range []string{"dispatching", "waiting_quota", "waiting_capacity", " WAITING_CAPACITY "} {
		if !cvmPhaseNeedsSubmission(phase) {
			t.Fatalf("phase %q should repair a missing submission", phase)
		}
	}
	for _, phase := range []string{"bootstrapping", "running", "archiving", "terminating", "terminal", ""} {
		if cvmPhaseNeedsSubmission(phase) {
			t.Fatalf("phase %q should not create a new submission", phase)
		}
	}
}

func TestLegacyExecutionRecoveryClassification(t *testing.T) {
	for _, status := range []model.TaskStatus{model.TaskStatusCompleted, model.TaskStatusPendingInterpretation, model.TaskStatusFailed, model.TaskStatusCancelled} {
		if !cvmTerminalTaskStatus(status) {
			t.Fatalf("status %q should be terminal during recovery", status)
		}
	}
	for _, status := range []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning, model.TaskStatusWaitingData} {
		if cvmTerminalTaskStatus(status) {
			t.Fatalf("status %q should remain active during recovery", status)
		}
	}
	for _, status := range []string{"DISPATCHING", "waiting_quota", " WAITING_CAPACITY "} {
		if !cvmDispatchVMStatus(status) {
			t.Fatalf("VM status %q should be dispatching", status)
		}
	}
	if cvmDispatchVMStatus("RUNNING") {
		t.Fatal("RUNNING must not be treated as a new dispatch")
	}
}

func TestCVMSubmissionConcurrentStartAndCancel(t *testing.T) {
	dsn := os.Getenv("EXECUTION_TEST_DSN")
	if dsn == "" {
		t.Skip("set EXECUTION_TEST_DSN to an isolated PostgreSQL instance")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := "octopus_execution_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := root.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	old := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = old
		sqlDB, _ := db.DB()
		sqlDB.Close()
		root.Exec("DROP SCHEMA " + schema + " CASCADE")
		sqlRoot, _ := root.DB()
		sqlRoot.Close()
	})
	if err := db.AutoMigrate(&model.Task{}, &model.CVMSubmission{}, &model.CVMCancellation{}, &model.ExecutionOutbox{}); err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: uuid.NewString(), UUID: uuid.NewString(), Executor: model.ExecutorCVM, ExternalOrgID: uuid.NewString(), Status: model.TaskStatusQueued, InputJSON: "{}"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan string, 20)
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := &TaskService{}
			result, err := s.enqueueCVM(context.Background(), task.UUID, model.OverlayActor{})
			if err != nil {
				failures <- err
				return
			}
			results <- result.ExecutionAttemptID
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	attempt := ""
	for id := range results {
		if attempt == "" {
			attempt = id
		}
		if id != attempt {
			t.Fatal("concurrent starts generated distinct attempts")
		}
	}
	var count int64
	db.Model(&model.CVMSubmission{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected one submission, got %d", count)
	}
	db.Model(&model.ExecutionOutbox{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected one atomic lifecycle event, got %d", count)
	}
	var stale model.Task
	db.Where("uuid = ?", task.UUID).First(&stale)
	svc := &TaskService{}
	cancelled, err := svc.cancelCVM(context.Background(), task.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.ExecutionPhase != "terminating" || cancelled.Status == model.TaskStatusCancelled {
		t.Fatal("cancel must await confirmed resource release")
	}
	var cancellation model.CVMCancellation
	if err := db.Where("attempt_id = ?", attempt).First(&cancellation).Error; err != nil {
		t.Fatalf("durable cancellation was not persisted: %v", err)
	}
	if cancellation.Delivered {
		t.Fatal("cancellation should remain pending until Squid acknowledges it")
	}
	var submission model.CVMSubmission
	if err := db.Where("attempt_id = ?", attempt).First(&submission).Error; err != nil {
		t.Fatalf("submission was not persisted: %v", err)
	}
	if !submission.Delivered {
		t.Fatal("pending dispatch should be fenced after cancellation")
	}
	stale.Status = model.TaskStatusRunning
	if err := repository.NewTaskRepository().Update(&stale); err == nil {
		t.Fatal("stale update erased cancellation")
	}
	retry, err := svc.enqueueCVM(context.Background(), task.UUID, model.OverlayActor{})
	if err != nil {
		t.Fatal(err)
	}
	if retry.ExecutionAttemptID != attempt || retry.ExecutionPhase != "terminating" {
		t.Fatal("restart raced cancellation")
	}
}
