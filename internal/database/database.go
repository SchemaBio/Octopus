package database

import (
	"fmt"
	"strings"

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
		&model.ResultPackage{},
		// Upload models
		&model.UploadJob{},
		&model.UploadFile{},
		&model.DataAsset{},
		&model.SampleDataLink{},
		&model.TaskDataAsset{},
		&model.CNVBaseline{},
		&model.CNVBaselineReadPair{},
		&model.WorkflowThresholdConfig{},
		// Import audit models
		&model.ResultImportBatch{},
		&model.VariantReviewEvent{},
	)
	if err != nil {
		return fmt.Errorf("failed to auto migrate: %w", err)
	}
	if err := migrateSampleOrganizationIndexes(); err != nil {
		return err
	}
	if err := migrateTaskExecutionColumns(); err != nil {
		return err
	}
	if err := preserveBEDDataAssets(); err != nil {
		return err
	}
	if err := migrateAuditScope(); err != nil {
		return err
	}

	return nil
}

// migrateAuditScope backfills the canonical tenant/attempt columns and
// creates one append-only legacy review event for existing reviewed rows. It
// is intentionally idempotent: result IDs are used as legacy event IDs.
func migrateAuditScope() error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}
	if err := DB.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		return fmt.Errorf("failed to enable SHA-256 support: %w", err)
	}
	if err := DB.Exec(`CREATE OR REPLACE FUNCTION audit_sha256(value text) RETURNS text AS $$ SELECT encode(digest(value, 'sha256'), 'hex') $$ LANGUAGE sql IMMUTABLE`).Error; err != nil {
		return fmt.Errorf("failed to create audit fingerprint helper: %w", err)
	}
	if err := DB.Exec(`UPDATE tasks SET tenant_id = CASE WHEN NULLIF(external_org_id, '') IS NOT NULL THEN 'org:' || external_org_id WHEN created_by <> 0 THEN 'user:' || created_by::text ELSE NULL END WHERE tenant_id IS NULL OR tenant_id = '' OR tenant_id = 'user:0'`).Error; err != nil {
		return fmt.Errorf("failed to backfill task tenant IDs: %w", err)
	}
	var missing int64
	if err := DB.Model(&model.Task{}).Where("tenant_id IS NULL OR tenant_id = ''").Count(&missing).Error; err != nil {
		return fmt.Errorf("failed to validate task tenant IDs: %w", err)
	}
	if missing > 0 {
		var taskIDs []string
		_ = DB.Raw(`SELECT uuid FROM tasks WHERE tenant_id IS NULL OR tenant_id = '' LIMIT 20`).Scan(&taskIDs).Error
		return fmt.Errorf("cannot migrate audit scope: %d tasks have no resolvable tenant (task UUIDs: %s)", missing, strings.Join(taskIDs, ", "))
	}
	if err := DB.Exec(`ALTER TABLE tasks ALTER COLUMN tenant_id SET NOT NULL`).Error; err != nil {
		return fmt.Errorf("failed to enforce tasks tenant scope: %w", err)
	}

	resultTables := []string{
		"result_snv_indels", "result_cnv_segments", "result_cnv_exons", "result_strs",
		"result_mei_variants", "result_mt_variants", "result_upd_regions", "result_roh_regions",
		"result_qc", "cnv_assessments",
	}
	for _, table := range resultTables {
		if err := DB.Exec(fmt.Sprintf(`UPDATE %s r SET tenant_id = t.tenant_id, execution_attempt_id = COALESCE(NULLIF(r.execution_attempt_id, ''), COALESCE(NULLIF(t.execution_attempt_id, ''), t.uuid)) FROM tasks t WHERE t.uuid = r.task_id AND (r.tenant_id IS NULL OR r.tenant_id = '' OR r.execution_attempt_id IS NULL OR r.execution_attempt_id = '')`, table)).Error; err != nil {
			return fmt.Errorf("failed to backfill %s provenance: %w", table, err)
		}
		var orphanIDs []string
		if err := DB.Raw(fmt.Sprintf(`SELECT id::text FROM %s WHERE tenant_id IS NULL OR tenant_id = '' OR execution_attempt_id IS NULL OR execution_attempt_id = '' LIMIT 20`, table)).Scan(&orphanIDs).Error; err != nil {
			return fmt.Errorf("failed to validate %s provenance: %w", table, err)
		}
		if len(orphanIDs) > 0 {
			return fmt.Errorf("cannot migrate audit scope: %s rows have no resolvable task tenant/attempt (ids: %s)", table, strings.Join(orphanIDs, ", "))
		}
		if err := DB.Exec(fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN tenant_id SET NOT NULL`, table)).Error; err != nil {
			return fmt.Errorf("failed to enforce %s tenant scope: %w", table, err)
		}
		if err := DB.Exec(fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN execution_attempt_id SET NOT NULL`, table)).Error; err != nil {
			return fmt.Errorf("failed to enforce %s execution attempt scope: %w", table, err)
		}
	}
	if err := DB.Exec(`UPDATE result_import_batches b SET tenant_id = t.tenant_id, execution_attempt_id = COALESCE(NULLIF(b.execution_attempt_id, ''), COALESCE(NULLIF(t.execution_attempt_id, ''), t.uuid)) FROM tasks t WHERE t.uuid = b.task_uuid AND (b.tenant_id IS NULL OR b.tenant_id = '' OR b.execution_attempt_id IS NULL OR b.execution_attempt_id = '')`).Error; err != nil {
		return fmt.Errorf("failed to backfill import batch provenance: %w", err)
	}
	var missingBatches int64
	if err := DB.Raw(`SELECT COUNT(*) FROM result_import_batches WHERE tenant_id IS NULL OR tenant_id = '' OR execution_attempt_id IS NULL OR execution_attempt_id = ''`).Scan(&missingBatches).Error; err != nil {
		return fmt.Errorf("failed to validate import batch provenance: %w", err)
	}
	if missingBatches > 0 {
		var batchIDs []string
		_ = DB.Raw(`SELECT id::text || ':' || task_uuid FROM result_import_batches WHERE tenant_id IS NULL OR tenant_id = '' OR execution_attempt_id IS NULL OR execution_attempt_id = '' LIMIT 20`).Scan(&batchIDs).Error
		return fmt.Errorf("cannot migrate audit scope: %d import batches have no resolvable tenant/attempt (batch:task IDs: %s)", missingBatches, strings.Join(batchIDs, ", "))
	}
	if err := DB.Exec(`ALTER TABLE result_import_batches ALTER COLUMN tenant_id SET NOT NULL`).Error; err != nil {
		return fmt.Errorf("failed to enforce import batch tenant scope: %w", err)
	}
	if err := DB.Exec(`ALTER TABLE result_import_batches ALTER COLUMN execution_attempt_id SET NOT NULL`).Error; err != nil {
		return fmt.Errorf("failed to enforce import batch execution attempt scope: %w", err)
	}
	// Older releases used task_id alone for these projections.  Drop those
	// indexes before creating the attempt-scoped keys generated by AutoMigrate.
	for _, statement := range []string{
		`DROP INDEX IF EXISTS idx_result_qc_task_id`,
		`DROP INDEX IF EXISTS uni_result_qc_task_id`,
		`DROP INDEX IF EXISTS result_qc_task_id_key`,
		`DROP INDEX IF EXISTS idx_cnv_assessment_variant`,
	} {
		if err := DB.Exec(statement).Error; err != nil {
			return fmt.Errorf("failed to replace result uniqueness indexes: %w", err)
		}
	}
	if err := DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_result_qc_attempt ON result_qc (tenant_id, task_id, execution_attempt_id)`).Error; err != nil {
		return fmt.Errorf("failed to create QC attempt index: %w", err)
	}
	if err := DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_cnv_assessment_variant ON cnv_assessments (tenant_id, task_id, execution_attempt_id, variant_type, variant_id)`).Error; err != nil {
		return fmt.Errorf("failed to create CNV assessment attempt index: %w", err)
	}
	if err := DB.Exec(`ALTER TABLE cnv_assessments ALTER COLUMN tenant_id SET NOT NULL`).Error; err != nil {
		return fmt.Errorf("failed to enforce CNV assessment tenant scope: %w", err)
	}
	if err := DB.Exec(`ALTER TABLE cnv_assessments ALTER COLUMN execution_attempt_id SET NOT NULL`).Error; err != nil {
		return fmt.Errorf("failed to enforce CNV assessment execution attempt scope: %w", err)
	}

	legacyStatements := []string{
		`INSERT INTO variant_review_events (id, tenant_id, task_uuid, execution_attempt_id, variant_type, variant_id, variant_fingerprint, history_group_key, action, actor_email, reference_genome, task_name, pipeline, pipeline_version, sample_id, internal_id, occurred_at, timestamp_known, recorded_at, variant_snapshot_json)
		 SELECT r.id, t.tenant_id, r.task_id, COALESCE(NULLIF(r.execution_attempt_id, ''), t.uuid), 'snv-indel', r.id,
		 audit_sha256(concat_ws('|','snv-indel',COALESCE(t.input_json->>'reference_genome',''),r.chromosome,r.position,r.ref,r.alt)),
		 concat_ws('|','snv-indel',COALESCE(t.input_json->>'reference_genome',''),r.gene,r.hgv_sc,r.hgv_sp), 'REVIEWED', r.reviewed_by,
		 COALESCE(t.input_json->>'reference_genome',''), t.name, t.pipeline, t.pipeline_version, t.sample_id, t.internal_id, r.reviewed_at, r.reviewed_at IS NOT NULL, NOW(), to_jsonb(r)
		 FROM result_snv_indels r JOIN tasks t ON t.uuid=r.task_id WHERE r.reviewed=true ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO variant_review_events (id, tenant_id, task_uuid, execution_attempt_id, variant_type, variant_id, variant_fingerprint, history_group_key, action, actor_email, reference_genome, task_name, pipeline, pipeline_version, sample_id, internal_id, occurred_at, timestamp_known, recorded_at, variant_snapshot_json)
		 SELECT r.id, t.tenant_id, r.task_id, COALESCE(NULLIF(r.execution_attempt_id, ''), t.uuid), 'cnv-segment', r.id,
		 audit_sha256(concat_ws('|','cnv-segment',COALESCE(t.input_json->>'reference_genome',''),r.chromosome,r.start_position,r.end_position,r.type)),
		 concat_ws('|','cnv-segment',COALESCE(t.input_json->>'reference_genome',''),r.chromosome,r.start_position,r.end_position,r.type), 'REVIEWED', r.reviewed_by,
		 COALESCE(t.input_json->>'reference_genome',''), t.name, t.pipeline, t.pipeline_version, t.sample_id, t.internal_id, r.reviewed_at, r.reviewed_at IS NOT NULL, NOW(), to_jsonb(r)
		 FROM result_cnv_segments r JOIN tasks t ON t.uuid=r.task_id WHERE r.reviewed=true ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO variant_review_events (id, tenant_id, task_uuid, execution_attempt_id, variant_type, variant_id, variant_fingerprint, history_group_key, action, actor_email, reference_genome, task_name, pipeline, pipeline_version, sample_id, internal_id, occurred_at, timestamp_known, recorded_at, variant_snapshot_json)
		 SELECT r.id, t.tenant_id, r.task_id, COALESCE(NULLIF(r.execution_attempt_id, ''), t.uuid), 'cnv-exon', r.id,
		 audit_sha256(concat_ws('|','cnv-exon',COALESCE(t.input_json->>'reference_genome',''),r.chromosome,r.start_position,r.end_position,r.gene,r.transcript,r.type)),
		 concat_ws('|','cnv-exon',COALESCE(t.input_json->>'reference_genome',''),r.gene,r.transcript,r.exon_count,r.type), 'REVIEWED', r.reviewed_by,
		 COALESCE(t.input_json->>'reference_genome',''), t.name, t.pipeline, t.pipeline_version, t.sample_id, t.internal_id, r.reviewed_at, r.reviewed_at IS NOT NULL, NOW(), to_jsonb(r)
		 FROM result_cnv_exons r JOIN tasks t ON t.uuid=r.task_id WHERE r.reviewed=true ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO variant_review_events (id, tenant_id, task_uuid, execution_attempt_id, variant_type, variant_id, variant_fingerprint, history_group_key, action, actor_email, reference_genome, task_name, pipeline, pipeline_version, sample_id, internal_id, occurred_at, timestamp_known, recorded_at, variant_snapshot_json)
		 SELECT r.id, t.tenant_id, r.task_id, COALESCE(NULLIF(r.execution_attempt_id, ''), t.uuid), 'str', r.id,
		 audit_sha256(concat_ws('|','str',COALESCE(t.input_json->>'reference_genome',''),r.chromosome,r.position,r.repeat_unit)),
		 concat_ws('|','str',COALESCE(t.input_json->>'reference_genome',''),r.gene,r.chromosome,r.position,r.repeat_unit,r.status), 'REVIEWED', r.reviewed_by,
		 COALESCE(t.input_json->>'reference_genome',''), t.name, t.pipeline, t.pipeline_version, t.sample_id, t.internal_id, r.reviewed_at, r.reviewed_at IS NOT NULL, NOW(), to_jsonb(r)
		 FROM result_strs r JOIN tasks t ON t.uuid=r.task_id WHERE r.reviewed=true ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO variant_review_events (id, tenant_id, task_uuid, execution_attempt_id, variant_type, variant_id, variant_fingerprint, history_group_key, action, actor_email, reference_genome, task_name, pipeline, pipeline_version, sample_id, internal_id, occurred_at, timestamp_known, recorded_at, variant_snapshot_json)
		 SELECT r.id, t.tenant_id, r.task_id, COALESCE(NULLIF(r.execution_attempt_id, ''), t.uuid), 'mei', r.id,
		 audit_sha256(concat_ws('|','mei',COALESCE(t.input_json->>'reference_genome',''),r.chromosome,r.position,r.te_type)),
		 concat_ws('|','mei',COALESCE(t.input_json->>'reference_genome',''),r.chromosome,r.position,r.gene,r.te_type), 'REVIEWED', r.reviewed_by,
		 COALESCE(t.input_json->>'reference_genome',''), t.name, t.pipeline, t.pipeline_version, t.sample_id, t.internal_id, r.reviewed_at, r.reviewed_at IS NOT NULL, NOW(), to_jsonb(r)
		 FROM result_mei_variants r JOIN tasks t ON t.uuid=r.task_id WHERE r.reviewed=true ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO variant_review_events (id, tenant_id, task_uuid, execution_attempt_id, variant_type, variant_id, variant_fingerprint, history_group_key, action, actor_email, reference_genome, task_name, pipeline, pipeline_version, sample_id, internal_id, occurred_at, timestamp_known, recorded_at, variant_snapshot_json)
		 SELECT r.id, t.tenant_id, r.task_id, COALESCE(NULLIF(r.execution_attempt_id, ''), t.uuid), 'mt', r.id,
		 audit_sha256(concat_ws('|','mt',COALESCE(t.input_json->>'reference_genome',''),r.position,r.ref,r.alt)),
		 concat_ws('|','mt',COALESCE(t.input_json->>'reference_genome',''),r.position,r.ref,r.alt), 'REVIEWED', r.reviewed_by,
		 COALESCE(t.input_json->>'reference_genome',''), t.name, t.pipeline, t.pipeline_version, t.sample_id, t.internal_id, r.reviewed_at, r.reviewed_at IS NOT NULL, NOW(), to_jsonb(r)
		 FROM result_mt_variants r JOIN tasks t ON t.uuid=r.task_id WHERE r.reviewed=true ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO variant_review_events (id, tenant_id, task_uuid, execution_attempt_id, variant_type, variant_id, variant_fingerprint, history_group_key, action, actor_email, reference_genome, task_name, pipeline, pipeline_version, sample_id, internal_id, occurred_at, timestamp_known, recorded_at, variant_snapshot_json)
		 SELECT r.id, t.tenant_id, r.task_id, COALESCE(NULLIF(r.execution_attempt_id, ''), t.uuid), 'upd', r.id,
		 audit_sha256(concat_ws('|','upd',COALESCE(t.input_json->>'reference_genome',''),r.chromosome,r.start_position,r.end_position,r.type)),
		 concat_ws('|','upd',COALESCE(t.input_json->>'reference_genome',''),r.chromosome,r.start_position,r.end_position,r.type), 'REVIEWED', r.reviewed_by,
		 COALESCE(t.input_json->>'reference_genome',''), t.name, t.pipeline, t.pipeline_version, t.sample_id, t.internal_id, r.reviewed_at, r.reviewed_at IS NOT NULL, NOW(), to_jsonb(r)
		 FROM result_upd_regions r JOIN tasks t ON t.uuid=r.task_id WHERE r.reviewed=true ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO variant_review_events (id, tenant_id, task_uuid, execution_attempt_id, variant_type, variant_id, variant_fingerprint, history_group_key, action, actor_email, reference_genome, task_name, pipeline, pipeline_version, sample_id, internal_id, occurred_at, timestamp_known, recorded_at, variant_snapshot_json)
		 SELECT r.id, t.tenant_id, r.task_id, COALESCE(NULLIF(r.execution_attempt_id, ''), t.uuid), 'roh', r.id,
		 audit_sha256(concat_ws('|','roh',COALESCE(t.input_json->>'reference_genome',''),r.chr,r.begin,r.end)),
		 concat_ws('|','roh',COALESCE(t.input_json->>'reference_genome',''),r.chr,r.begin,r.end), 'REVIEWED', r.reviewed_by,
		 COALESCE(t.input_json->>'reference_genome',''), t.name, t.pipeline, t.pipeline_version, t.sample_id, t.internal_id, r.reviewed_at, r.reviewed_at IS NOT NULL, NOW(), to_jsonb(r)
		 FROM result_roh_regions r JOIN tasks t ON t.uuid=r.task_id WHERE r.reviewed=true ON CONFLICT (id) DO NOTHING`,
	}
	for _, statement := range legacyStatements {
		if err := DB.Exec(statement).Error; err != nil {
			return fmt.Errorf("failed to bootstrap review events: %w", err)
		}
	}
	if err := DB.Exec(`CREATE OR REPLACE FUNCTION prevent_variant_review_event_mutation() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'variant review events are append-only'; END; $$ LANGUAGE plpgsql`).Error; err != nil {
		return fmt.Errorf("failed to create review event guard: %w", err)
	}
	if err := DB.Exec(`DROP TRIGGER IF EXISTS variant_review_events_append_only ON variant_review_events`).Error; err != nil {
		return fmt.Errorf("failed to reset review event guard: %w", err)
	}
	if err := DB.Exec(`CREATE TRIGGER variant_review_events_append_only BEFORE UPDATE OR DELETE ON variant_review_events FOR EACH ROW EXECUTE FUNCTION prevent_variant_review_event_mutation()`).Error; err != nil {
		return fmt.Errorf("failed to enable review event guard: %w", err)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_variant_review_events_scope ON variant_review_events (tenant_id, variant_type, execution_attempt_id, variant_fingerprint, recorded_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_variant_review_events_task_timeline ON variant_review_events (tenant_id, task_uuid, variant_type, variant_fingerprint, recorded_at DESC)`,
	} {
		if err := DB.Exec(statement).Error; err != nil {
			return fmt.Errorf("failed to create review event indexes: %w", err)
		}
	}
	return nil
}

// BackfillAuditEvents is used by the development seed command after inserting
// demo results so those reviewed rows participate in /history immediately.
func BackfillAuditEvents() error { return migrateAuditScope() }

func preserveBEDDataAssets() error {
	if err := DB.Exec("UPDATE data_assets SET expires_at = NULL WHERE read_type = ? AND expires_at IS NOT NULL", model.ReadTypeBed).Error; err != nil {
		return fmt.Errorf("failed to remove BED data retention expiry: %w", err)
	}
	return nil
}

func migrateTaskExecutionColumns() error {
	statements := []string{
		"ALTER TABLE tasks ADD COLUMN IF NOT EXISTS cvm_archive_staged_at timestamptz",
		"ALTER TABLE tasks ADD COLUMN IF NOT EXISTS cvm_archive_termination_notified_at timestamptz",
	}
	for _, statement := range statements {
		if err := DB.Exec(statement).Error; err != nil {
			return fmt.Errorf("failed to migrate task execution columns: %w", err)
		}
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
