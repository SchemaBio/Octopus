package database

import (
	"fmt"
	"os"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDB initializes database connection based on config
func InitDB(cfg *config.Config) error {
	var err error
	var dialector gorm.Dialector

	// Configure GORM logger
	gormLogger := logger.Default.LogMode(logger.Info)
	if cfg.Server.Mode == "release" {
		gormLogger = logger.Default.LogMode(logger.Warn)
	}

	// Select driver based on config
	switch cfg.Database.Driver {
	case "postgres":
		dialector = postgres.Open(cfg.Database.DSN)
	case "sqlite":
		// Ensure directory exists for SQLite file
		if cfg.Database.DSN != "" {
			dir := cfg.Database.DSN
			if lastSlash := len(dir) - 1; dir[lastSlash] == '/' {
				dir = dir[:lastSlash]
			}
			for i := len(dir) - 1; i >= 0; i-- {
				if dir[i] == '/' || dir[i] == '\\' {
					dir = dir[:i]
					break
				}
			}
			if dir != "" && dir != "." {
				os.MkdirAll(dir, 0755)
			}
		}
		dialector = sqlite.Open(cfg.Database.DSN)
	default:
		return fmt.Errorf("unsupported database driver: %s", cfg.Database.Driver)
	}

	// Connect to database
	DB, err = gorm.Open(dialector, &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return fmt.Errorf("failed to connect database: %w", err)
	}

	return nil
}

// AutoMigrate runs auto migration for all models
func AutoMigrate() error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	err := DB.AutoMigrate(
		// Core models
		&model.User{},
		&model.Task{},
		&model.Sample{},
		&model.Project{},
		// Organization models
		&model.Organization{},
		&model.UserOrganization{},
		// Pedigree models
		&model.Pedigree{},
		&model.PedigreeMember{},
		// Gene list models
		&model.GeneList{},
		// Pipeline models
		&model.Pipeline{},
		// Variant result models
		&model.SNVIndel{},
		&model.CNVSegment{},
		&model.CNVExon{},
		&model.STR{},
		&model.MEIVariant{},
		&model.MitochondrialVariant{},
		&model.UPDRegion{},
		&model.QCResult{},
		// Report models
		&model.Report{},
		&model.ReportTemplate{},
	)
	if err != nil {
		return fmt.Errorf("failed to auto migrate: %w", err)
	}

	return nil
}

// GetDB returns the database instance
func GetDB() *gorm.DB {
	return DB
}

// CloseDB closes database connection
func CloseDB() error {
	if DB == nil {
		return nil
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
