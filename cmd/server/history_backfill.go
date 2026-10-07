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

func runHistoryBackfill(args []string) error {
	flags := flag.NewFlagSet("history-backfill", flag.ContinueOnError)
	execute := flags.Bool("execute", false, "write compact reported-row projections; default only inspects candidates")
	task := flags.String("task", "", "optional task UUID restriction")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	cfg := config.Load()
	if err := database.InitDB(cfg); err != nil {
		return err
	}
	defer database.CloseDB()
	result, err := service.NewResultService(cfg).BackfillHistoryReports(context.Background(), *execute, *task)
	if err != nil {
		return err
	}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return err
	}
	if len(result.Unresolved) > 0 {
		return fmt.Errorf("%d source records require reconciliation", len(result.Unresolved))
	}
	return nil
}
