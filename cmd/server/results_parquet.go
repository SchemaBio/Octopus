package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
	"os"
)

// Administrative, attempt-pinned preparation. No queue, cloud clients or billing are started.
func runResultsParquetCommand(args []string) error {
	flags := flag.NewFlagSet("results-parquet", flag.ContinueOnError)
	taskID := flags.String("task", "", "task UUID")
	attempt := flags.String("attempt", "", "current attempt UUID")
	table := flags.String("table", "snv-indel", "result table")
	repairTable := flags.String("repair-table", "", "rebuild one existing dataset from its archived original report; requires --execute --backfill")
	backfill := flags.Bool("backfill", false, "convert missing manifest-declared archived text tables and publish a separate catalogue; requires --execute")
	inspect := flags.Bool("inspect-archive", false, "inspect archive object metadata and manifest references without mutation")
	execute := flags.Bool("execute", false, "prepare the dataset and migrate proven legacy adjustments")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *taskID == "" || *attempt == "" || flags.NArg() != 0 {
		return fmt.Errorf("--task and --attempt are required")
	}
	cfg := config.Load()
	if err := database.InitDB(cfg); err != nil {
		return err
	}
	defer database.CloseDB()
	var task model.Task
	if err := database.DB.Where("uuid=? AND execution_attempt_id=?", *taskID, *attempt).First(&task).Error; err != nil {
		return fmt.Errorf("current task attempt not found")
	}
	svc := service.NewResultService(cfg)
	if *repairTable != "" && (!*backfill || !*execute) {
		return fmt.Errorf("--repair-table requires --execute --backfill")
	}
	if *backfill && !*execute {
		return fmt.Errorf("--backfill requires --execute")
	}
	if *inspect {
		result, err := svc.InspectParquetArchive(context.Background(), &task)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if !*execute {
		var count int64
		if err := database.DB.Model(&model.ResultDataset{}).Where("task_uuid=? AND execution_attempt_id=? AND \"table\"=?", *taskID, *attempt, *table).Count(&count).Error; err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"mode": "dry_run", "existing_datasets": count, "table": *table})
	}
	if *backfill {
		summary, err := svc.BackfillArchivedParquet(context.Background(), &task, *repairTable)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
			return err
		}
	}
	result, err := svc.QueryParquetTable(context.Background(), &task, *table, model.ParquetQueryRequest{Limit: 1})
	if err != nil {
		return err
	}
	migration, err := svc.MigrateLegacyResultAdjustments(context.Background(), &task, *table, true)
	if err != nil {
		return err
	}
	contextResult, err := svc.GetContext(context.Background(), &task)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"context_state": contextResult.State, "context_counts": contextResult.Types, "mode": "executed", "rows": result.RowCount, "data_version": result.Version, "migration": migration})
}
