package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/assistance"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/retention"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RetentionService struct {
	cfg   *config.Config
	tasks *TaskService
	AI    *assistance.Service
}

func NewRetentionService(cfg *config.Config, tasks *TaskService) *RetentionService {
	return &RetentionService{cfg: cfg, tasks: tasks}
}

func (s *RetentionService) Start(ctx context.Context) {
	if !s.cfg.IsSaaS() {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			cycle, cancel := context.WithTimeout(ctx, 50*time.Second)
			if err := s.Scan(cycle, time.Now().UTC()); err != nil {
				log.Print("retention: inventory failed")
			}
			if s.cfg.RetentionCleanupEnabled {
				var jobs []model.RetentionJob
				if database.DB.WithContext(cycle).Where("(next_retry_at IS NULL OR next_retry_at<=?) AND (lease_until IS NULL OR lease_until<=?)", time.Now(), time.Now()).Order("due_at,id").Limit(20).Find(&jobs).Error == nil {
					for _, job := range jobs {
						if cycle.Err() != nil {
							break
						}
						if err := s.Execute(cycle, job.ID); err != nil {
							log.Print("retention: destruction pending")
						}
					}
				}
			}
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *RetentionService) Scan(ctx context.Context, now time.Time) error {
	db := database.DB.WithContext(ctx).Unscoped()
	// INSERT SELECT avoids loading every old task into memory; jobs are durable.
	for _, item := range [][2]string{{"tasks", "task"}, {"samples", "sample"}} {
		statement := fmt.Sprintf(`INSERT INTO retention_jobs(id,kind,resource_id,org_id,due_at,attempts,lease_id,last_error) SELECT '%s:'||uuid,'%s',uuid,external_org_id,expires_at,0,'','' FROM %s WHERE expires_at<=? ON CONFLICT(id) DO NOTHING`, item[1], item[1], item[0])
		if err := db.Exec(statement, now.UTC()).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *RetentionService) Execute(ctx context.Context, id string) error {
	if !s.cfg.IsSaaS() || !s.cfg.RetentionCleanupEnabled {
		return fmt.Errorf("physical cleanup disabled")
	}
	db := database.DB.WithContext(ctx).Unscoped()
	now := time.Now().UTC()
	lease := uuid.NewString()
	until := now.Add(5 * time.Minute)
	claim := db.Model(&model.RetentionJob{}).Where("id=? AND due_at<=? AND (lease_until IS NULL OR lease_until<=?) AND (next_retry_at IS NULL OR next_retry_at<=?)", id, now, now, now).Updates(map[string]interface{}{"lease_id": lease, "lease_until": until, "attempts": gorm.Expr("attempts+1")})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return nil
	}
	var job model.RetentionJob
	if err := db.First(&job, "id=?", id).Error; err != nil {
		return err
	}
	var err error
	if job.Kind == "task" {
		err = db.Transaction(func(tx *gorm.DB) error { return s.destroyTask(ctx, tx, &job) })
	} else if job.Kind == "sample" {
		err = s.destroySample(db, &job)
	} else {
		err = fmt.Errorf("invalid resource kind")
	}
	if err != nil {
		retry := time.Now().UTC().Add(time.Minute)
		retryCtx, retryCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer retryCancel()
		_ = database.DB.WithContext(retryCtx).Unscoped().Model(&model.RetentionJob{}).Where("id=? AND lease_id=?", id, lease).Updates(map[string]interface{}{"last_error": "DESTRUCTION_PENDING", "next_retry_at": retry, "lease_until": nil, "lease_id": ""}).Error
		return err
	}
	return db.Where("id=? AND lease_id=?", id, lease).Delete(&model.RetentionJob{}).Error
}

func (s *RetentionService) destroyTask(ctx context.Context, db *gorm.DB, job *model.RetentionJob) error {
	return s.destroyTaskForReset(ctx, db, job, false)
}

func (s *RetentionService) destroyTaskForReset(ctx context.Context, db *gorm.DB, job *model.RetentionJob, reset bool) error {
	var task model.Task
	err := db.Where("uuid=? AND external_org_id=?", job.ResourceID, job.OrgID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !reset && (task.ExpiresAt == nil || time.Now().Before(*task.ExpiresAt)) {
		return fmt.Errorf("task not due")
	}
	prefix, err := retentionPrefix(task.ExternalOrgID, task.UUID)
	if err != nil {
		return err
	}
	if err := retention.Stop(ctx, task.UUID); err != nil {
		return err
	}
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uuid=? AND external_org_id=?", task.UUID, task.ExternalOrgID).First(&task).Error; err != nil {
		return err
	}
	// Squid confirms ALL attempts are terminal and their credentials revoked.
	if s.tasks.overlay == nil {
		return fmt.Errorf("SaaS overlay unavailable")
	}
	var prepared struct {
		Ready bool `json:"ready"`
	}
	status, _, err := s.tasks.overlay.postJSON(ctx, "/api/v1/overlay/tasks/retention", map[string]interface{}{"task_uuid": task.UUID, "org_id": task.ExternalOrgID, "phase": "prepare"}, &prepared)
	if err != nil || status < 200 || status >= 300 || !prepared.Ready {
		return fmt.Errorf("execution destruction not confirmed")
	}
	s.tasks.mu.Lock()
	if cmd, ok := s.tasks.running[task.UUID]; ok {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		delete(s.tasks.running, task.UUID)
	}
	s.tasks.mu.Unlock()
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return err
	}
	if !reset {
		if err := s.preserveBaselines(ctx, db, storage, &task, prefix); err != nil {
			return err
		}
	}
	// Reject unexpected independently stored screenshot locations rather than silently orphaning them.
	var screenshots []model.IGVSnapshot
	if err := db.Where("task_uuid=?", task.UUID).Find(&screenshots).Error; err != nil {
		return err
	}
	for _, shot := range screenshots {
		if !strings.HasPrefix(shot.ObjectKey, prefix) {
			expected := "organizations/" + task.ExternalOrgID + "/"
			if !strings.HasPrefix(shot.ObjectKey, expected) || !strings.Contains(shot.ObjectKey, task.UUID) {
				return fmt.Errorf("unverified screenshot ownership")
			}
			// Exact object generations are removed through a prefix confined to its full key.
			if err := storage.purgeExactObject(ctx, shot.ObjectKey); err != nil {
				return err
			}
		}
	}
	if err := storage.purgePrefix(ctx, prefix); err != nil {
		return err
	}
	for _, base := range []string{s.cfg.Task.OutputDir, s.cfg.Task.ArchiveDir} {
		if err := removeTaskDirectory(base, task.UUID); err != nil {
			return err
		}
	}
	if err := removeTaskDirectory(filepath.Join(s.cfg.ResultQuery.ReferenceDir, "task-proofs", digestAssessment([]byte(model.TenantIDForTask(&task)))), task.UUID); err != nil {
		return err
	}
	stages, err := filepath.Glob(filepath.Join(s.cfg.ResultQuery.CacheDir, task.UUID+"-*.tmp"))
	if err != nil {
		return err
	}
	for _, stage := range stages {
		if err := os.Remove(stage); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	temporary, err := filepath.Glob(filepath.Join(os.TempDir(), task.UUID+"-octopus-*"))
	if err != nil {
		return err
	}
	for _, file := range temporary {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	var datasets []model.ResultDataset
	if err := db.Where("task_uuid=?", task.UUID).Find(&datasets).Error; err != nil {
		return err
	}
	for _, dataset := range datasets {
		if err := s.removeDatasetCache(&dataset); err != nil {
			return err
		}
	}
	if s.AI != nil {
		if err := s.AI.EraseTask(ctx, task.UUID); err != nil {
			return err
		}
	}
	// Finish external cleanup before deleting the last source identity.
	var downloadIDs []string
	if err := db.Model(&model.ResultDownload{}).Where("task_uuid=? AND org_id=?", task.UUID, task.ExternalOrgID).Pluck("id", &downloadIDs).Error; err != nil {
		return err
	}
	references := make([]string, 0, len(downloadIDs))
	for _, id := range downloadIDs {
		references = append(references, "download:"+id)
	}
	status, _, err = s.tasks.overlay.postJSON(ctx, "/api/v1/overlay/tasks/retention", map[string]interface{}{"task_uuid": task.UUID, "org_id": task.ExternalOrgID, "phase": "finalize", "billing_references": references}, nil)
	if err != nil || status < 200 || status >= 300 {
		return fmt.Errorf("overlay cleanup pending")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('research.expiry_task',?,true)", task.UUID).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id=?", task.UUID).Delete(&assistance.Request{}).Error; err != nil {
			return err
		}
		// Schema metadata covers all task-scoped result, audit, grant and delivery models.
		models := []interface{}{&model.SNVIndel{}, &model.CNVSegment{}, &model.CNVExon{}, &model.STR{}, &model.MEIVariant{}, &model.MitochondrialVariant{}, &model.UPDRegion{}, &model.ROHRegion{}, &model.QCResult{}, &model.CNVAssessment{}, &model.Report{}, &model.ReportGeneration{}, &model.ResultPackage{}, &model.ResultDownload{}, &model.RawResultPackage{}, &model.ResultImportBatch{}, &model.VariantReviewEvent{}, &model.ResultDataset{}, &model.IGVSnapshot{}, &model.ResultAssessmentContext{}, &model.ResultSavedView{}, &model.ResultLegacyRowMapping{}, &model.ResultRowAutomaticAssessment{}, &model.ResultRowAdjustment{}, &model.ResultRowAdjustmentEvent{}, &model.HistoryFocus{}, &model.TaskDataAsset{}, &model.CVMSubmission{}, &model.CVMCancellation{}}
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
				return fmt.Errorf("missing task scope for %s", stmt.Schema.Table)
			}
			if err := tx.Where(field.DBName+" IN ?", []string{task.UUID, task.ID}).Delete(value).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("DELETE FROM bam_retention_events WHERE job_id IN (SELECT id FROM bam_retention_jobs WHERE task_uuid=?)", task.UUID).Error; err != nil {
			return err
		}
		if err := tx.Where("task_uuid=?", task.UUID).Delete(&model.BAMRetentionJob{}).Error; err != nil {
			return err
		}
		if err := tx.Where("payload::jsonb->'task'->>'uuid'=? AND payload::jsonb->'task'->>'org_id'=?", task.UUID, task.ExternalOrgID).Delete(&model.ExecutionOutbox{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO research_erased_resources(digest) VALUES (md5(?)) ON CONFLICT DO NOTHING", "tasks:"+task.ExternalOrgID+":"+task.UUID).Error; err != nil {
			return err
		}
		return tx.Delete(&task).Error
	})
}

func (s *RetentionService) destroySample(db *gorm.DB, job *model.RetentionJob) error {
	return s.destroySampleForReset(db, job, false)
}

func (s *RetentionService) destroySampleForReset(db *gorm.DB, job *model.RetentionJob, reset bool) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var sample model.Sample
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uuid=? AND external_org_id=?", job.ResourceID, job.OrgID).First(&sample).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !reset && (sample.ExpiresAt == nil || time.Now().Before(*sample.ExpiresAt)) {
			return fmt.Errorf("sample not due")
		}
		if s.tasks.overlay == nil {
			return fmt.Errorf("sample audit erasure unavailable")
		}
		status, _, err := s.tasks.overlay.postJSON(db.Statement.Context, "/api/v1/overlay/samples/retention", map[string]interface{}{"sample_uuid": sample.UUID, "org_id": sample.ExternalOrgID}, nil)
		if err != nil || status < 200 || status >= 300 {
			return fmt.Errorf("sample audit erasure pending")
		}

		if err := tx.Where("sample_id=?", sample.ID).Delete(&model.SampleDataLink{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Task{}).Where("sample_id=?", sample.UUID).Updates(map[string]interface{}{"sample_id": "", "sample_id_ref": 0}).Error; err != nil {
			return err
		}
		var members []model.PedigreeMember
		if err := tx.Where("sample_id=?", sample.UUID).Find(&members).Error; err != nil {
			return err
		}
		for _, member := range members {
			updates := expiredSampleMemberUpdates(&member)
			if err := tx.Model(&member).Updates(updates).Error; err != nil {
				return err
			}
		}
		if !reset {
			if err := tx.Model(&model.UploadJob{}).Where("sample_id=? AND external_org_id=?", sample.UUID, sample.ExternalOrgID).Updates(map[string]interface{}{"sample_id": "", "internal_id": ""}).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("INSERT INTO research_erased_resources(digest) VALUES (md5(?)) ON CONFLICT DO NOTHING", "samples:"+sample.ExternalOrgID+":"+sample.UUID).Error; err != nil {
			return err
		}
		return tx.Delete(&sample).Error
	})
}

func (s *RetentionService) preserveBaselines(ctx context.Context, db *gorm.DB, storage *s3Storage, task *model.Task, prefix string) error {
	var baselines []model.CNVBaseline
	if err := db.Where("task_uuid=? AND external_org_id=?", task.UUID, task.ExternalOrgID).Find(&baselines).Error; err != nil {
		return err
	}
	for _, baseline := range baselines {
		if baseline.OutputPath == "" {
			// A crash between copying and publishing can leave an unregistered copy.
			if err := storage.purgeExactObject(ctx, "organizations/"+task.ExternalOrgID+"/baselines/"+baseline.UUID+"/reference.cnn"); err != nil {
				return err
			}
			if err := db.Where("baseline_id=?", baseline.ID).Delete(&model.CNVBaselineReadPair{}).Error; err != nil {
				return err
			}
			if err := db.Delete(&baseline).Error; err != nil {
				return err
			}
			continue
		}
		u, err := url.Parse(baseline.OutputPath)
		if err != nil {
			return err
		}
		source := strings.TrimPrefix(u.Path, "/")
		target := "organizations/" + task.ExternalOrgID + "/baselines/" + baseline.UUID + "/reference.cnn"
		if source != target {
			if !strings.HasPrefix(source, prefix) {
				return fmt.Errorf("unverified baseline source")
			}
			if err := storage.promoteBaseline(ctx, source, target); err != nil {
				return err
			}
			u.Path = "/" + target
			u.RawQuery = ""
			u.Fragment = ""
		}
		if err := db.Model(&baseline).Updates(map[string]interface{}{"output_path": u.String(), "status": model.TaskStatusCompleted, "task_uuid": nil, "start_error": ""}).Error; err != nil {
			return err
		}
		if err := db.Where("baseline_id=?", baseline.ID).Delete(&model.CNVBaselineReadPair{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *RetentionService) removeDatasetCache(dataset *model.ResultDataset) error {
	// Cache keys are content hashes. Remove only provable dataset cache entries.
	if len(dataset.ID) != 64 || strings.ContainsAny(dataset.ID, "/\\.") {
		return fmt.Errorf("invalid dataset identity")
	}
	tempBase := s.cfg.ResultQuery.TempDir
	if tempBase == "" {
		tempBase = filepath.Join(s.cfg.ResultQuery.CacheDir, "tmp")
	}
	for _, pattern := range []string{"octopus-sort-" + dataset.ID + "-*", "octopus-effective-" + dataset.ID + "-*.csv"} {
		paths, err := filepath.Glob(filepath.Join(tempBase, pattern))
		if err != nil {
			return err
		}
		for _, path := range paths {
			info, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe query temporary")
			}
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	parts, err := filepath.Glob(filepath.Join(s.cfg.ResultQuery.AssessmentDir, dataset.ID+"-acmg-*.tmp"))
	if err != nil {
		return err
	}
	for _, part := range parts {
		if err := os.Remove(part); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	names := []struct{ base, name string }{{s.cfg.ResultQuery.CacheDir, currentCacheName(dataset.ID, dataset.ObjectSHA256)}, {s.cfg.ResultQuery.AssessmentDir, dataset.ID + "-" + dataset.ObjectSHA256 + "-" + automaticACMGProfile + ".jsonl"}}
	for _, entry := range names {
		if filepath.Base(entry.name) != entry.name {
			return fmt.Errorf("invalid cache name")
		}
		if err := os.Remove(filepath.Join(entry.base, entry.name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, key := range []string{dataset.ID, dataset.ObjectSHA256} {
		if len(key) != 64 || strings.ContainsAny(key, "/\\.") {
			continue
		}
		for _, base := range []string{s.cfg.ResultQuery.CacheDir, s.cfg.ResultQuery.AssessmentDir} {
			for _, suffix := range []string{"", ".parquet", ".json"} {
				target := filepath.Join(base, key+suffix)
				if info, err := os.Lstat(target); err == nil {
					if info.Mode()&os.ModeSymlink != 0 {
						return fmt.Errorf("unsafe cache entry")
					}
					if err := os.RemoveAll(target); err != nil {
						return err
					}
				} else if !os.IsNotExist(err) {
					return err
				}
			}
		}
	}
	return nil
}
