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
)

func runResultsAssessmentCommand(args []string) error {
	flags := flag.NewFlagSet("results-assessment", flag.ContinueOnError)
	task := flags.String("task", "", "task UUID")
	attempt := flags.String("attempt", "", "execution attempt UUID")
	execute := flags.Bool("execute", false, "prepare evidence; otherwise inspect only")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *task == "" || *attempt == "" {
		return fmt.Errorf("--task and --attempt are required")
	}
	cfg := config.Load()
	if err := database.InitDB(cfg); err != nil {
		return err
	}
	defer database.CloseDB()
	output, err := service.NewResultService(cfg).PrepareAssessmentEvidence(context.Background(), *task, *attempt, *execute)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(output)
}
