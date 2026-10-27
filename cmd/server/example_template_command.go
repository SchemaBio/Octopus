package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/service"
)

func runExampleTemplateCommand(args []string) error {
	cfg := config.Load()
	if !cfg.IsSaaS() {
		return fmt.Errorf("example templates are SaaS-only")
	}
	flags := flag.NewFlagSet("example-template", flag.ContinueOnError)
	version := flags.String("version", "", "new immutable template version")
	sample := flags.String("sample", "", "reviewed source sample UUID")
	task := flags.String("task", "", "matching completed single-sample task UUID")
	activate := flags.Bool("activate", false, "activate the new published version")
	switchVersion := flags.String("activate-version", "", "activate an existing version")
	disable := flags.Bool("disable", false, "stop offering examples to new approvals")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *disable || *switchVersion != "" {
		if (*disable && *switchVersion != "") || *version != "" || *sample != "" || *task != "" || *activate {
			return fmt.Errorf("choose either publication, activation or disable")
		}
	} else if *version == "" || *sample == "" || *task == "" {
		return fmt.Errorf("version, sample and task are required")
	}
	if err := database.InitDB(cfg); err != nil {
		return err
	}
	defer database.CloseDB()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// No automatic migrations, schedulers or billing workers run in this command.
	if *disable {
		return service.ActivateExampleTemplate(ctx, cfg, "")
	}
	if *switchVersion != "" {
		return service.ActivateExampleTemplate(ctx, cfg, *switchVersion)
	}
	root, err := os.MkdirTemp("", "octopus-example-publish-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	m, err := service.ExportExampleTemplate(ctx, cfg, *sample, *task, *version, root)
	if err != nil {
		return err
	}
	if err := service.PublishExampleTemplate(ctx, cfg, root, m, *activate); err != nil {
		return err
	}
	fmt.Println("Example template published:", *version, "active:", *activate)
	return nil
}
