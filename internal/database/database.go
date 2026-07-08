package database

import (
	"fmt"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDB initializes database connection based on config
func InitDB(cfg *config.Config) error {
	var err error

	// Configure GORM logger
	gormLogger := logger.Default.LogMode(logger.Info)
	if cfg.Server.Mode == "release" {
		gormLogger = logger.Default.LogMode(logger.Warn)
	}

	// Connect to PostgreSQL
	DB, err = gorm.Open(postgres.Open(cfg.Database.DSN), &gorm.Config{
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

	// Initialize token_version for existing users (NULL or 0 → 1)
	DB.Exec("UPDATE users SET token_version = 1 WHERE token_version IS NULL OR token_version = 0")

	err := DB.AutoMigrate(
		// Core models
		&model.User{},
		&model.Task{},
		&model.Sample{},
		&model.Project{},
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
		&model.ROHRegion{},
		&model.QCResult{},
		&model.CNVAssessment{},
		// Report models
		&model.Report{},
		&model.ReportTemplate{},
		// Upload models
		&model.UploadJob{},
		&model.UploadFile{},
		// Import audit models
		&model.ResultImportBatch{},
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
