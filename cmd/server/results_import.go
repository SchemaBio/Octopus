package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/service"
)

func runResultsImportCommand(args []string) error {
	flags := flag.NewFlagSet("results-import", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	taskUUID := flags.String("task", "", "completed CVM task UUID")
	attemptID := flags.String("attempt", "", "current execution attempt UUID")
	execute := flags.Bool("execute", false, "perform the result import; without this flag only inspect")
	annotationsOnly := flags.Bool("annotations-only", false, "restore SNV source annotations while preserving result IDs and review state")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *taskUUID == "" || *attemptID == "" {
		return fmt.Errorf("--task and --attempt are required")
	}

	cfg := config.Load()
	if err := database.InitDB(cfg); err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer database.CloseDB()

	taskService := service.NewTaskService(cfg)
	if *annotationsOnly {
		result, err := taskService.RecoverArchivedSNVAnnotations(context.Background(), *taskUUID, *attemptID, *execute)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if !*execute {
		check, err := taskService.InspectArchivedTaskResults(*taskUUID, *attemptID)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"mode": "dry_run", "task_uuid": check.TaskUUID, "attempt_id": check.AttemptID,
			"task_status": check.TaskStatus, "import_status": check.ImportStatus,
			"import_attempts": check.ImportAttempts, "outputs_manifest_ready": check.OutputsManifestReady,
		})
	}

	progress, err := taskService.RecoverArchivedTaskResults(context.Background(), *taskUUID, *attemptID)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
		"mode": "executed", "task_uuid": progress.UUID,
		"attempt_id": progress.ExecutionAttemptID, "task_status": progress.Status,
		"import_status": progress.ResultImportStatus, "import_attempts": progress.ResultImportAttempts,
	})
}
