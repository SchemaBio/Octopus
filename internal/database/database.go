package database

import (
	"fmt"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
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
		&model.DataAsset{},
		&model.SampleDataLink{},
		&model.TaskDataAsset{},
		&model.CNVBaseline{},
		&model.CNVBaselineReadPair{},
		// Import audit models
		&model.ResultImportBatch{},
	)
	if err != nil {
		return fmt.Errorf("failed to auto migrate: %w", err)
	}
	if err := migrateSampleOrganizationIndexes(); err != nil {
		return err
	}

	return nil
}

func migrateSampleOrganizationIndexes() error {
	statements := []string{
		"UPDATE samples SET manual_matched_pair = matched_pair WHERE matched_pair IS NOT NULL AND matched_pair::text NOT IN ('', 'null', '{}') AND (manual_matched_pair IS NULL OR manual_matched_pair::text IN ('', 'null', '{}')) AND (auto_matched_pair IS NULL OR auto_matched_pair::text IN ('', 'null', '{}')) AND match_mode IS DISTINCT FROM 'automatic'",
		"UPDATE samples SET auto_matched_pair = matched_pair WHERE matched_pair IS NOT NULL AND matched_pair::text NOT IN ('', 'null', '{}') AND (manual_matched_pair IS NULL OR manual_matched_pair::text IN ('', 'null', '{}')) AND (auto_matched_pair IS NULL OR auto_matched_pair::text IN ('', 'null', '{}')) AND match_mode = 'automatic'",
		"UPDATE samples SET match_status = 'matched', match_mode = 'manual' WHERE manual_matched_pair IS NOT NULL AND manual_matched_pair::text NOT IN ('', 'null', '{}')",
		"UPDATE samples SET match_status = 'matched', match_mode = 'automatic' WHERE (manual_matched_pair IS NULL OR manual_matched_pair::text IN ('', 'null', '{}')) AND auto_matched_pair IS NOT NULL AND auto_matched_pair::text NOT IN ('', 'null', '{}')",
		"UPDATE samples SET manual_matched_pair = 'null'::jsonb WHERE manual_matched_pair IS NULL",
		"UPDATE samples SET auto_matched_pair = 'null'::jsonb WHERE auto_matched_pair IS NULL",
		"UPDATE samples SET match_mode = '' WHERE match_mode IS NULL",
		"DROP INDEX IF EXISTS idx_sample_data_links_sample_id",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_sample_data_link_mode ON sample_data_links (sample_id, match_mode)",
		"DROP INDEX IF EXISTS idx_samples_internal_id",
		"DROP INDEX IF EXISTS uni_samples_internal_id",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_samples_org_internal_id ON samples (external_org_id, internal_id) WHERE external_org_id <> ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_samples_user_internal_id ON samples (created_by, internal_id) WHERE external_org_id = ''",
	}
	for _, statement := range statements {
		if err := DB.Exec(statement).Error; err != nil {
			return fmt.Errorf("failed to migrate sample organization indexes: %w", err)
		}
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
