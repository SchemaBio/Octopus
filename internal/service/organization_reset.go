package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SchemaBio/Octopus/internal/assistance"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ValidateResetIdentity(org, id string) error {
	for _, value := range []string{org, id} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != value {
			return errors.New("invalid reset identity")
		}
	}
	return nil
}

// One bounded unit per signed control-plane request. Both the receipt and row
// lock are database-backed; restarts and duplicate deliveries are safe.
func StepOrganizationReset(ctx context.Context, cfg *config.Config, org, id, phase string) (string, error) {
	if !cfg.IsSaaS() {
		return "", errors.New("organization reset is SaaS-only")
	}
	if err := ValidateResetIdentity(org, id); err != nil {
		return "", err
	}
	if phase == "check" {
		err := resetPreflight(database.DB.WithContext(ctx).Unscoped(), org)
		return "ready", err
	}
	state := "clearing"
	err := database.DB.WithContext(ctx).Unscoped().Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "organization-reset:"+org).Error; err != nil {
			return err
		}
		var receipt model.OrganizationReset
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).First(&receipt).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if receipt.ID == id && receipt.State == "completed" {
			if receipt.OrgID != org {
				return errors.New("reset identity conflict")
			}
			state = "completed"
			return nil
		}
		if receipt.ID != id {
			var active int64
			if err := tx.Model(&model.OrganizationReset{}).Where("org_id=? AND state='clearing'", org).Count(&active).Error; err != nil {
				return err
			}
			if phase != "prepare" || active > 0 {
				return errors.New("reset identity conflict")
			}
			if err := resetPreflight(tx, org); err != nil {
				return err
			}
			receipt = model.OrganizationReset{OrgID: org, ID: id, State: "clearing"}
			return tx.Create(&receipt).Error
		}
		if receipt.OrgID != org {
			return errors.New("reset identity conflict")
		}
		if phase == "prepare" {
			return nil
		}
		if phase != "step" {
			return errors.New("invalid reset phase")
		}
		var tasks []model.Task
		if err := tx.Where("external_org_id=?", org).Order("uuid").Limit(1).Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) > 0 {
			ret := NewRetentionService(cfg, NewTaskService(cfg))
			ret.AI = assistance.New(database.DB, cfg.ExternalAuth.Enabled, cfg.Overlay.BaseURL, cfg.Overlay.SharedSecret)
			return ret.destroyTaskForReset(ctx, tx, &model.RetentionJob{OrgID: org, ResourceID: tasks[0].UUID}, true)
		}
		var samples []model.Sample
		if err := tx.Where("external_org_id=?", org).Order("uuid").Limit(1).Find(&samples).Error; err != nil {
			return err
		}
		if len(samples) > 0 {
			ret := NewRetentionService(cfg, NewTaskService(cfg))
			return ret.destroySampleForReset(tx, &model.RetentionJob{OrgID: org, ResourceID: samples[0].UUID}, true)
		}
		// All objects under a validated UUID namespace, including versions and
		// unfinished multipart uploads. Shared templates/reference data are outside it.
		storage, err := newS3Storage(ctx, cfg.Storage)
		if err != nil {
			return err
		}
		if err := storage.purgePrefix(ctx, "organizations/"+org+"/"); err != nil {
			return err
		}
		if err := clearOrganizationRows(tx, org); err != nil {
			return err
		}
		state = "completed"
		return tx.Model(&receipt).Update("state", state).Error
	})
	return state, err
}

func resetPreflight(tx *gorm.DB, org string) error {
	checks := []struct {
		sql  string
		args []interface{}
	}{
		{"SELECT count(*) FROM tasks WHERE external_org_id=? AND (status IN ('running','waiting_for_data') OR (executor='cvm_spot' AND execution_attempt_id<>'' AND execution_phase NOT IN ('terminal','idle')) OR result_import_status='running')", []interface{}{org}},
		{"SELECT count(*) FROM upload_jobs WHERE external_org_id=? AND status='uploading'", []interface{}{org}},
		{"SELECT count(*) FROM example_data_grants WHERE org_id=? AND (status NOT IN ('ready','expired') OR lease_until>?)", []interface{}{org, time.Now()}},
		{"SELECT count(*) FROM ai_assistance_requests WHERE org_id=? AND (state NOT IN ('completed','failed','cancelled','expired','uncertain') OR (billing_enabled AND NOT billing_done))", []interface{}{org}},
		{"SELECT count(*) FROM report_generations WHERE tenant_id=? AND state='generating'", []interface{}{"org:" + org}},
		{"SELECT count(*) FROM result_packages WHERE task_uuid IN (SELECT uuid FROM tasks WHERE external_org_id=?) AND status='building'", []interface{}{org}},
		{"SELECT count(*) FROM raw_result_packages WHERE task_uuid IN (SELECT uuid FROM tasks WHERE external_org_id=?) AND status='building'", []interface{}{org}},
	}
	for _, check := range checks {
		var n int64
		if err := tx.Raw(check.sql, check.args...).Scan(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errors.New("organization has active work; stop tasks, uploads and AI/example provisioning before reset")
		}
	}
	return nil
}

func clearOrganizationRows(tx *gorm.DB, org string) error {
	// Children first. Predicates always originate in this organization's parent
	// identity; no creator/user ID is used as a substitute for tenant ownership.
	for _, sql := range []string{
		"DELETE FROM upload_multipart_parts WHERE session_uuid IN (SELECT uuid FROM upload_multipart_sessions WHERE file_uuid IN (SELECT f.uuid FROM upload_files f JOIN upload_jobs j ON j.id=f.job_id WHERE j.external_org_id=?))",
		"DELETE FROM upload_multipart_sessions WHERE file_uuid IN (SELECT f.uuid FROM upload_files f JOIN upload_jobs j ON j.id=f.job_id WHERE j.external_org_id=?)",
		"DELETE FROM upload_files WHERE job_id IN (SELECT id FROM upload_jobs WHERE external_org_id=?)",
		"DELETE FROM pedigree_members WHERE pedigree_id IN (SELECT id FROM pedigrees WHERE external_org_id=?)",
		"DELETE FROM cnv_baseline_read_pairs WHERE baseline_id IN (SELECT id FROM cnv_baselines WHERE external_org_id=?)",
		"DELETE FROM example_data_objects WHERE grant_id IN (SELECT id FROM example_data_grants WHERE org_id=?)",
	} {
		if err := tx.Exec(sql, org).Error; err != nil {
			return err
		}
	}
	for _, item := range []struct {
		value  interface{}
		column string
	}{
		{&model.SampleDataLink{}, "external_org_id"}, {&model.DataAsset{}, "external_org_id"}, {&model.UploadJob{}, "external_org_id"},
		{&model.CNVBaseline{}, "external_org_id"}, {&model.Sample{}, "external_org_id"}, {&model.Pedigree{}, "external_org_id"},
		{&model.Project{}, "external_org_id"}, {&model.GeneList{}, "external_org_id"}, {&model.Pipeline{}, "external_org_id"}, {&model.ReportTemplate{}, "external_org_id"},
		{&model.HistoryFocus{}, "tenant_id"}, {&model.HistoryScopeRevision{}, "tenant_id"}, {&model.RetentionJob{}, "org_id"}, {&model.ExampleDataGrant{}, "org_id"},
	} {
		value := org
		if item.column == "tenant_id" {
			value = "org:" + org
		}
		if err := tx.Where(item.column+"=?", value).Delete(item.value).Error; err != nil {
			return fmt.Errorf("reset scoped records: %w", err)
		}
	}
	return nil
}
