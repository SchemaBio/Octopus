package database

import "gorm.io/gorm"

// Writers take a shared transaction lock. Preparing a reset takes the exclusive
// lock, draining admitted writes before installing the durable reset fence.
func installOrganizationResetFences(tx *gorm.DB) error {
	if err := tx.Exec(`CREATE OR REPLACE FUNCTION organization_reset_write_fence() RETURNS trigger LANGUAGE plpgsql AS $$
 DECLARE org text; BEGIN
 org=coalesce(nullif(to_jsonb(NEW)->>'external_org_id',''),nullif(to_jsonb(NEW)->>'org_id',''));
 IF org IS NULL THEN RETURN NEW; END IF;
 PERFORM pg_advisory_xact_lock_shared(hashtextextended('organization-reset:'||org,0));
 IF EXISTS(SELECT 1 FROM organization_resets WHERE org_id=org AND state='clearing') THEN
 RAISE EXCEPTION 'organization reset in progress' USING ERRCODE='23514'; END IF;
 RETURN NEW; END $$`).Error; err != nil {
		return err
	}
	for _, table := range []string{"tasks", "samples", "projects", "pedigrees", "gene_lists", "pipelines", "report_templates", "upload_jobs", "data_assets", "sample_data_links", "cnv_baselines", "example_data_grants"} {
		if err := tx.Exec("DROP TRIGGER IF EXISTS organization_reset_write_fence ON " + table).Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE TRIGGER organization_reset_write_fence BEFORE INSERT OR UPDATE ON " + table + " FOR EACH ROW EXECUTE FUNCTION organization_reset_write_fence()").Error; err != nil {
			return err
		}
	}
	if err := tx.Exec("DROP TRIGGER IF EXISTS organization_reset_write_fence ON ai_assistance_requests").Error; err != nil {
		return err
	}
	return tx.Exec("CREATE TRIGGER organization_reset_write_fence BEFORE INSERT ON ai_assistance_requests FOR EACH ROW EXECUTE FUNCTION organization_reset_write_fence()").Error
}
