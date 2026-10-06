package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/service"
	"os"
	"time"
)

func runBAMRetentionCommand(args []string) error {
	flags := flag.NewFlagSet("bam-retention", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	task := flags.String("task", "", "completed task UUID")
	attempt := flags.String("attempt", "", "execution attempt UUID")
	execute := flags.Bool("execute", false, "delete only this expired attempt; default inspect without writes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *task == "" || *attempt == "" {
		return fmt.Errorf("--task and --attempt required")
	}
	cfg := config.Load()
	if err := database.InitDB(cfg); err != nil {
		return fmt.Errorf("database unavailable")
	}
	defer database.CloseDB()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	svc := service.NewBAMRetentionService(cfg)
	plan, err := svc.Inspect(ctx, *task, *attempt)
	if err != nil {
		return err
	}
	if !*execute {
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	if plan.Job.ID == "" {
		return fmt.Errorf("completion snapshot not registered; start server before executing cleanup")
	}
	if !plan.Due {
		return fmt.Errorf("BAM is not expired")
	}
	if err := svc.Execute(ctx, plan.Job.ID); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"task_uuid": *task, "attempt_id": *attempt, "cleanup": "confirmed", "object_count": len(plan.Objects)})
}
