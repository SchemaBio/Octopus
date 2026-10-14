package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/service"
	"os"
	"time"
)

// This entry point deliberately never calls AutoMigrate or starts any worker.
func runResearchRetentionPreflight(args []string) error {
	flags := flag.NewFlagSet("research-retention-preflight", flag.ContinueOnError)
	at := flags.String("at", "", "UTC RFC3339 cutoff (default: now)")
	objects := flags.Bool("objects", false, "inventory object versions and unfinished uploads")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	now := time.Now().UTC()
	if *at != "" {
		parsed, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			return err
		}
		now = parsed.UTC()
	}
	cfg := config.Load()
	if err := database.InitDB(cfg); err != nil {
		return err
	}
	defer database.CloseDB()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	tx := database.DB.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		return tx.Error
	}
	defer tx.Rollback()
	manifest, err := service.ResearchRetentionPreflight(ctx, tx, cfg, now, *objects)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(manifest)
}
