package database

import (
	"fmt"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// Unscoped is reserved for expiry workers; ordinary reads/counts/updates are fenced.
func retentionScope(now time.Time, db *gorm.DB) {
	if db.Statement.Unscoped || db.Statement.Schema == nil && db.Statement.Table == "" {
		return
	}
	table := db.Statement.Table
	if db.Statement.Schema != nil {
		table = db.Statement.Schema.Table
	}
	if table != "tasks" && table != "samples" {
		if db.Statement.Schema == nil {
			return
		}
		switch table {
		case "cnv_baselines", "c_vm_submissions", "c_vm_cancellations", "bam_retention_jobs", "bam_retention_events", "execution_outboxes", "ai_assistance_requests":
			return
		}
		field := db.Statement.Schema.LookUpField("TaskUUID")
		if field == nil {
			field = db.Statement.Schema.LookUpField("TaskID")
		}
		if field == nil {
			return
		}
		db.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "EXISTS (SELECT 1 FROM tasks retention_parent WHERE (retention_parent.uuid=? OR retention_parent.id=?) AND retention_parent.deleted_at IS NULL AND retention_parent.expires_at>?)", Vars: []interface{}{clause.Column{Table: clause.CurrentTable, Name: field.DBName}, clause.Column{Table: clause.CurrentTable, Name: field.DBName}, now.UTC()}}}})
		return
	}
	db.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Gt{Column: clause.Column{Table: clause.CurrentTable, Name: "expires_at"}, Value: now.UTC()}}})
}

func EnableSaaSRetention() error {
	if DB == nil {
		return fmt.Errorf("database unavailable")
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("CREATE TABLE IF NOT EXISTS research_erased_resources(digest text PRIMARY KEY)").Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE TABLE IF NOT EXISTS research_schema_changes(id text PRIMARY KEY)").Error; err != nil {
			return err
		}
		provenance := tx.Exec("INSERT INTO research_schema_changes(id) VALUES ('sample-member-provenance-v1') ON CONFLICT DO NOTHING")
		if provenance.Error != nil {
			return provenance.Error
		}
		if provenance.RowsAffected > 0 {
			// The former create-pedigree UI copied the proband identifier and gender.
			// Mark only copies still equal to their source; independent members survive.
			if err := tx.Exec(`UPDATE pedigree_members m SET source_sample_fields=CASE WHEN m.gender=s.gender THEN '["name","gender"]'::jsonb ELSE '["name"]'::jsonb END FROM samples s WHERE m.sample_id=s.uuid AND m.name=s.internal_id AND (m.source_sample_fields IS NULL OR m.source_sample_fields='[]'::jsonb)`).Error; err != nil {
				return err
			}
		}
		// A trigger also protects raw SQL writers and prevents edits extending expiry.
		if err := tx.Exec(`CREATE OR REPLACE FUNCTION research_retention_deadline() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF TG_OP='UPDATE' THEN NEW.created_at=OLD.created_at; END IF; NEW.expires_at=NEW.created_at + interval '720 hours'; IF TG_OP='INSERT' THEN IF NEW.expires_at<=clock_timestamp() OR EXISTS(SELECT 1 FROM research_erased_resources WHERE digest=md5(TG_TABLE_NAME||':'||coalesce(NEW.external_org_id,'')||':'||NEW.uuid)) THEN RAISE EXCEPTION 'resource unavailable' USING ERRCODE='23514'; END IF; END IF; RETURN NEW; END $$`).Error; err != nil {
			return err
		}
		for _, table := range []string{"tasks", "samples"} {
			if err := tx.Exec("UPDATE " + table + " SET expires_at=created_at + interval '720 hours' WHERE expires_at IS DISTINCT FROM created_at + interval '720 hours'").Error; err != nil {
				return err
			}
			if err := tx.Exec("DROP TRIGGER IF EXISTS research_retention_deadline ON " + table).Error; err != nil {
				return err
			}
			if err := tx.Exec("CREATE TRIGGER research_retention_deadline BEFORE INSERT OR UPDATE ON " + table + " FOR EACH ROW EXECUTE FUNCTION research_retention_deadline()").Error; err != nil {
				return err
			}
		}
		if err := installRetentionWriteFences(tx); err != nil {
			return err
		}
		if err := tx.Exec(`CREATE OR REPLACE FUNCTION research_sample_link_fence() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
   IF NEW.sample_id IS NULL OR NEW.sample_id='' THEN RETURN NEW; END IF;
   IF TG_OP='UPDATE' THEN IF NEW.sample_id=OLD.sample_id THEN RETURN NEW; END IF; END IF;
   PERFORM 1 FROM samples WHERE uuid=NEW.sample_id AND expires_at>clock_timestamp() FOR SHARE;
   IF NOT FOUND THEN RAISE EXCEPTION 'sample unavailable' USING ERRCODE='23514'; END IF; RETURN NEW; END $$`).Error; err != nil {
			return err
		}
		for _, table := range []string{"tasks", "pedigree_members"} {
			if err := tx.Exec("DROP TRIGGER IF EXISTS research_sample_link_fence ON " + table).Error; err != nil {
				return err
			}
			if err := tx.Exec("CREATE TRIGGER research_sample_link_fence BEFORE INSERT OR UPDATE ON " + table + " FOR EACH ROW EXECUTE FUNCTION research_sample_link_fence()").Error; err != nil {
				return err
			}
		}
		return tx.Exec("DELETE FROM history_focuses").Error
	}); err != nil {
		return err
	}
	for _, cb := range []struct {
		name     string
		register func(string, func(*gorm.DB)) error
	}{
		{"query", DB.Callback().Query().Before("gorm:query").Register},
		{"row", DB.Callback().Row().Before("gorm:row").Register},
		{"update", DB.Callback().Update().Before("gorm:update").Register},
	} {
		if err := cb.register("research:expiry:"+cb.name, func(db *gorm.DB) { retentionScope(time.Now(), db) }); err != nil {
			return err
		}
	}
	return nil
}

// Database fences cover importers/async writers as well as HTTP request handlers.
// SHARE locks serialize child writes with cleanup's parent deletion.
func installRetentionWriteFences(tx *gorm.DB) error {
	if err := tx.Exec(`CREATE OR REPLACE FUNCTION research_task_content_fence() RETURNS trigger LANGUAGE plpgsql AS $$
 DECLARE identity text; BEGIN
 identity=to_jsonb(NEW)->>TG_ARGV[0];
 PERFORM 1 FROM tasks WHERE (uuid=identity OR id=identity) AND deleted_at IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'task unavailable' USING ERRCODE='23514'; END IF;
 RETURN NEW; END $$`).Error; err != nil {
		return err
	}
	models := []interface{}{&model.SNVIndel{}, &model.CNVSegment{}, &model.CNVExon{}, &model.STR{}, &model.MEIVariant{}, &model.MitochondrialVariant{}, &model.UPDRegion{}, &model.ROHRegion{}, &model.QCResult{}, &model.CNVAssessment{}, &model.Report{}, &model.ReportGeneration{}, &model.ResultPackage{}, &model.ResultDownload{}, &model.RawResultPackage{}, &model.ResultImportBatch{}, &model.VariantReviewEvent{}, &model.ResultDataset{}, &model.IGVSnapshot{}, &model.ResultAssessmentContext{}, &model.ResultSavedView{}, &model.ResultLegacyRowMapping{}, &model.ResultRowAutomaticAssessment{}, &model.ResultRowAdjustment{}, &model.ResultRowAdjustmentEvent{}, &model.HistoryFocus{}, &model.TaskDataAsset{}}
	for _, value := range models {
		stmt := &gorm.Statement{DB: tx}
		if err := stmt.Parse(value); err != nil {
			return err
		}
		field := stmt.Schema.LookUpField("TaskUUID")
		if field == nil {
			field = stmt.Schema.LookUpField("TaskID")
		}
		if field == nil {
			return fmt.Errorf("task content lacks scope")
		}
		table := stmt.Schema.Table
		if err := tx.Exec("DROP TRIGGER IF EXISTS research_task_content_fence ON " + table).Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE TRIGGER research_task_content_fence BEFORE INSERT OR UPDATE ON " + table + " FOR EACH ROW EXECUTE FUNCTION research_task_content_fence('" + field.DBName + "')").Error; err != nil {
			return err
		}
	}
	return nil
}
