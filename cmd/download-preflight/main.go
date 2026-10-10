package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/SchemaBio/Octopus/internal/service"
)

func main() {
	taskID := flag.String("task", "", "completed task UUID")
	ip := flag.String("ip", "", "deprecated; download links no longer bind an IP")
	probe := flag.Bool("probe-network", false, "GET one byte and verify removing the signed traffic limit is denied (no billing)")
	flag.Parse()
	if *taskID == "" {
		fmt.Fprintln(os.Stderr, "--task is required")
		os.Exit(2)
	}
	cfg := config.Load()
	if err := database.InitDB(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "database initialization failed")
		os.Exit(1)
	}
	task, err := repository.NewTaskRepository().FindByUUID(*taskID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "task not found")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := service.InspectResultDownloadAuthorization(ctx, cfg, task, *ip, *probe)
	if result != nil {
		json.NewEncoder(os.Stdout).Encode(result)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
