package database

import (
	"fmt"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

// Run before AutoMigrate so legacy selection state is renamed, not replaced by defaults.
func migrateFocusNames() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// The legacy append-only trigger also rejects a schema migration. Take an
		// exclusive lock and disable it only inside this atomic migration transaction.
		var guarded bool
		if tx.Migrator().HasTable("variant_review_events") {
			if err := tx.Raw("SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='variant_review_events'::regclass AND tgname='variant_review_events_append_only')").Scan(&guarded).Error; err != nil {
				return err
			}
			if guarded {
				if err := tx.Exec("LOCK TABLE variant_review_events IN ACCESS EXCLUSIVE MODE").Error; err != nil {
					return err
				}
				if err := tx.Exec("ALTER TABLE variant_review_events DISABLE TRIGGER variant_review_events_append_only").Error; err != nil {
					return err
				}
			}
		}
		fenced := []string{}
		for _, table := range []string{"result_snv_indels", "result_cnv_segments", "result_cnv_exons", "result_strs", "result_mei_variants", "result_mt_variants", "result_upd_regions", "result_roh_regions", "result_row_adjustments", "result_row_adjustment_events", "result_saved_views", "variant_review_events", "history_focuses", "history_reports"} {
			if !tx.Migrator().HasTable(table) {
				continue
			}
			var exists bool
			if err := tx.Raw("SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=?::regclass AND tgname='research_task_content_fence')", table).Scan(&exists).Error; err != nil {
				return err
			}
			if exists {
				if err := tx.Exec("LOCK TABLE " + table + " IN ACCESS EXCLUSIVE MODE").Error; err != nil {
					return err
				}
				if err := tx.Exec("ALTER TABLE " + table + " DISABLE TRIGGER research_task_content_fence").Error; err != nil {
					return err
				}
				fenced = append(fenced, table)
			}
		}
		if tx.Migrator().HasTable("history_reports") && !tx.Migrator().HasTable("history_focuses") {
			if err := tx.Migrator().RenameTable("history_reports", "history_focuses"); err != nil {
				return err
			}
		}
		tables := []string{"result_snv_indels", "result_cnv_segments", "result_cnv_exons", "result_strs", "result_mei_variants", "result_mt_variants", "result_upd_regions", "result_roh_regions", "history_focuses", "history_reports"}
		for _, table := range tables {
			if !tx.Migrator().HasTable(table) {
				continue
			}
			for _, pair := range [][2]string{{"reported", "focused"}, {"reported_by", "focused_by"}, {"reported_at", "focused_at"}, {"reported_classification", "focused_classification"}, {"reported_acmg_version", "focused_acmg_version"}, {"reported_vus_subclass", "focused_vus_subclass"}, {"first_reported_at", "first_focused_at"}, {"last_reported_at", "last_focused_at"}} {
				if !tx.Migrator().HasColumn(table, pair[0]) {
					continue
				}
				if tx.Migrator().HasColumn(table, pair[1]) {
					if err := tx.Migrator().DropColumn(table, pair[0]); err != nil {
						return err
					}
				} else if err := tx.Migrator().RenameColumn(table, pair[0], pair[1]); err != nil {
					return err
				}
			}
		}

		// Transform nested filter/sort metadata and historical states without touching
		// arbitrary evidence strings. Canonical fields win, including false/null.
		if err := tx.Exec(`CREATE OR REPLACE FUNCTION research_migrate_focus_json(value jsonb) RETURNS jsonb LANGUAGE plpgsql AS $$
  DECLARE result jsonb; entry record; canonical text; BEGIN
   IF jsonb_typeof(value)='object' THEN
    result='{}'::jsonb;
    FOR entry IN SELECT key,val FROM jsonb_each(value) AS e(key,val) LOOP
     canonical=CASE entry.key WHEN 'reported' THEN 'focused' WHEN 'reportedBy' THEN 'focusedBy' WHEN 'reportedAt' THEN 'focusedAt' WHEN 'canReport' THEN 'canFocus' WHEN 'reportedClassification' THEN 'focusedClassification' WHEN 'reportedAcmgVersion' THEN 'focusedAcmgVersion' WHEN 'reportedVusSubclass' THEN 'focusedVusSubclass' WHEN 'firstReportedAt' THEN 'firstFocusedAt' WHEN 'lastReportedAt' THEN 'lastFocusedAt' ELSE entry.key END;
     IF canonical<>entry.key AND value ? canonical THEN CONTINUE; END IF;
     result=result||jsonb_build_object(canonical,research_migrate_focus_json(entry.val));
    END LOOP; RETURN result;
   ELSIF jsonb_typeof(value)='array' THEN
    SELECT coalesce(jsonb_agg(research_migrate_focus_json(e.val) ORDER BY e.ordinal),'[]'::jsonb) INTO result FROM jsonb_array_elements(value) WITH ORDINALITY AS e(val,ordinal); RETURN result;
   ELSIF value='"reported"'::jsonb THEN RETURN '"focused"'::jsonb;
   ELSIF value='"canReport"'::jsonb THEN RETURN '"canFocus"'::jsonb;
   END IF; RETURN value;
  END $$`).Error; err != nil {
			return err
		}
		for _, item := range [][2]string{{"result_row_adjustments", "payload_json"}, {"result_row_adjustment_events", "before_json"}, {"result_row_adjustment_events", "after_json"}, {"variant_review_events", "variant_snapshot_json"}, {"result_saved_views", "state_json"}, {"history_focuses", "fields_json"}, {"history_reports", "fields_json"}} {
			if !tx.Migrator().HasTable(item[0]) || !tx.Migrator().HasColumn(item[0], item[1]) {
				continue
			}
			sql := fmt.Sprintf("UPDATE %s SET %s=research_migrate_focus_json(%s) WHERE %s IS DISTINCT FROM research_migrate_focus_json(%s)", item[0], item[1], item[1], item[1], item[1])
			if err := tx.Exec(sql).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable("history_reports") && tx.Migrator().HasTable("history_focuses") {
			// Existing canonical projections win. Legacy-only identities are preserved.
			if err := tx.Exec(`INSERT INTO history_focuses SELECT (jsonb_populate_record(NULL::history_focuses,to_jsonb(old))).* FROM history_reports old ON CONFLICT(id) DO NOTHING`).Error; err != nil {
				return err
			}
			if err := tx.Migrator().DropTable("history_reports"); err != nil {
				return err
			}
		}
		if err := tx.Exec("DROP FUNCTION research_migrate_focus_json(jsonb)").Error; err != nil {
			return err
		}
		if tx.Migrator().HasTable(&model.ResultRowAdjustmentEvent{}) {
			if err := tx.Exec(`UPDATE result_row_adjustment_events SET reason=CASE WHEN reason LIKE '%回报%' OR reason LIKE '%回報%' THEN CASE WHEN after_json->>'focused'='true' THEN '关注变异' ELSE '取消关注' END ELSE reason END WHERE reason LIKE '%回报%' OR reason LIKE '%回報%'`).Error; err != nil {
				return err
			}
		}
		for _, table := range fenced {
			if tx.Migrator().HasTable(table) {
				if err := tx.Exec("ALTER TABLE " + table + " ENABLE TRIGGER research_task_content_fence").Error; err != nil {
					return err
				}
			}
		}
		if guarded {
			return tx.Exec("ALTER TABLE variant_review_events ENABLE TRIGGER variant_review_events_append_only").Error
		}
		return nil
	})
}
