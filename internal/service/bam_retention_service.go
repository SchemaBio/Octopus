package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BAMRetentionService struct{ cfg *config.Config }
type BAMRetentionObject struct {
	Key  string `json:"key"`
	Size int64  `json:"size_bytes"`
	ETag string `json:"etag"`
}
type BAMRetentionPlan struct {
	Job     model.BAMRetentionJob `json:"job"`
	Objects []BAMRetentionObject  `json:"objects"`
	Due     bool                  `json:"due"`
}

func NewBAMRetentionService(cfg *config.Config) *BAMRetentionService {
	return &BAMRetentionService{cfg: cfg}
}

// Errors fail closed: storage/DB outages must not issue a grant beyond expiry.
func bamRetentionDeadline(ctx context.Context, cfg *config.Config, task *model.Task) (*time.Time, error) {
	if cfg == nil || cfg.Storage.BAMRetentionDays != 7 {
		return nil, nil
	}
	if task == nil {
		return nil, fmt.Errorf("BAM retention identity missing")
	}
	var job model.BAMRetentionJob
	err := database.DB.WithContext(ctx).Where("task_uuid = ? AND org_id = ? AND attempt_id = ?", task.UUID, task.ExternalOrgID, task.ExecutionAttemptID).First(&job).Error
	if err == nil {
		return &job.ExpiresAt, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("BAM retention state unavailable")
	}
	if task.Status != model.TaskStatusCompleted || task.FinishedAt == nil || task.FinishedAt.IsZero() {
		return nil, fmt.Errorf("BAM completion timestamp unavailable")
	}
	deadline := task.FinishedAt.UTC().Add(7 * 24 * time.Hour)
	return &deadline, nil
}

func (s *BAMRetentionService) Start(ctx context.Context) {
	if s.cfg.Storage.BAMRetentionDays != 7 {
		return
	}
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			cycle, cancel := context.WithTimeout(ctx, 4*time.Minute)
			if err := s.backfill(cycle); err != nil {
				log.Print("BAM retention: completion snapshot backfill unavailable")
			}
			if s.cfg.Storage.BAMCleanupEnabled {
				var jobs []model.BAMRetentionJob
				if database.DB.WithContext(cycle).Where("expires_at <= ? AND status NOT IN ? AND (next_retry_at IS NULL OR next_retry_at <= ?) AND (lease_until IS NULL OR lease_until < ?)", time.Now(), []string{"deleted", "no_bam"}, time.Now(), time.Now()).Order("expires_at,id").Limit(10).Find(&jobs).Error == nil {
					for _, job := range jobs {
						if cycle.Err() != nil {
							break
						}
						if err := s.Execute(cycle, job.ID); err != nil {
							log.Printf("BAM retention: attempt=%s cleanup pending", job.AttemptID)
						}
					}
				}
			}
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

func (s *BAMRetentionService) backfill(ctx context.Context) error {
	// Capture only provable current completed attempts, including soft-deleted tasks.
	// Historic overwritten attempts without a completion timestamp are not guessed.
	cursor := ""
	for {
		var tasks []model.Task
		if err := database.DB.WithContext(ctx).Unscoped().Where("id > ? AND status = ? AND executor = ? AND finished_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM bam_retention_jobs j WHERE j.task_uuid=tasks.uuid AND j.org_id=tasks.external_org_id AND j.attempt_id=tasks.execution_attempt_id)", cursor, model.TaskStatusCompleted, model.ExecutorCVM).Order("id").Limit(100).Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}
		for _, task := range tasks {
			if err := repository.RegisterBAMRetention(database.DB.WithContext(ctx), &task, s.cfg.Storage.S3Bucket); err != nil {
				return err
			}
		}
		cursor = tasks[len(tasks)-1].ID
	}
}

func missingBAMObject(err error) bool {
	var api smithy.APIError
	return errors.As(err, &api) && (api.ErrorCode() == "NotFound" || api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "404")
}

func (s *BAMRetentionService) head(ctx context.Context, storage *s3Storage, key string) (*BAMRetentionObject, error) {
	meta, err := storage.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(storage.bucket), Key: aws.String(key)})
	if missingBAMObject(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("OBJECT_HEAD_FAILED")
	}
	return &BAMRetentionObject{Key: key, Size: aws.ToInt64(meta.ContentLength), ETag: aws.ToString(meta.ETag)}, nil
}

func (s *BAMRetentionService) prepare(ctx context.Context, job *model.BAMRetentionJob) ([]BAMRetentionObject, error) {
	if job.Bucket != s.cfg.Storage.S3Bucket {
		return nil, fmt.Errorf("BUCKET_CHANGED")
	}
	for _, id := range []string{job.TaskUUID, job.OrgID, job.AttemptID} {
		if _, err := uuid.Parse(id); err != nil {
			return nil, fmt.Errorf("IDENTITY_INVALID")
		}
	}
	task := model.Task{UUID: job.TaskUUID, ExternalOrgID: job.OrgID, ExecutionAttemptID: job.AttemptID}
	prefix := resultPackagePrefix(&task)
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("STORAGE_UNAVAILABLE")
	}
	if job.ManifestKey == "" {
		objects, err := storage.list(ctx, prefix+"/")
		if err != nil {
			return nil, fmt.Errorf("ARCHIVE_LIST_FAILED")
		}
		manifestKey, byBase, _, err := igvArchiveObjectIndex(prefix, objects)
		if err != nil {
			return nil, fmt.Errorf("MANIFEST_UNAVAILABLE")
		}
		manifest, err := readIGVManifest(ctx, storage, manifestKey)
		if err != nil {
			return nil, fmt.Errorf("MANIFEST_READ_FAILED")
		}
		raw, _ := json.Marshal(manifest)
		sum := sha256.Sum256(raw)
		plan := []BAMRetentionObject{}
		seen := map[string]bool{}
		bamRefs := manifestFileRefs(manifest, "bam")
		baiRefs := manifestFileRefs(manifest, "bai")
		for _, ref := range bamRefs {
			object, ok := archiveObjectForRef(ref, byBase)
			if !ok || !strings.HasSuffix(strings.ToLower(object.Key), ".bam") {
				return nil, fmt.Errorf("BAM_MAPPING_AMBIGUOUS")
			}
			keys := []string{object.Key}
			if index, ok := matchingIndexForRef(ref, baiRefs, byBase); ok {
				keys = append(keys, index)
			}
			for _, key := range keys {
				if _, err := safeResultPackageRelativePath(prefix, key); err != nil || !strings.HasPrefix(key, prefix+"/") || (!strings.HasSuffix(strings.ToLower(key), ".bam") && !strings.HasSuffix(strings.ToLower(key), ".bai")) {
					return nil, fmt.Errorf("OBJECT_SCOPE_INVALID")
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				meta, err := s.head(ctx, storage, key)
				if err != nil {
					return nil, err
				}
				if meta == nil {
					return nil, fmt.Errorf("OBJECT_DISAPPEARED_DURING_PLAN")
				}
				plan = append(plan, *meta)
			}
		}
		// Unmatched declared BAI is an error rather than a wildcard deletion.
		for _, ref := range baiRefs {
			obj, ok := archiveObjectForRef(ref, byBase)
			if !ok || !seen[obj.Key] {
				return nil, fmt.Errorf("INDEX_MAPPING_AMBIGUOUS")
			}
		}
		job.ManifestKey = manifestKey
		job.ManifestSHA256 = hex.EncodeToString(sum[:])
		encoded, _ := json.Marshal(plan)
		job.PlanJSON = string(encoded)
		return plan, nil
	}
	if _, err := safeResultPackageRelativePath(prefix, job.ManifestKey); err != nil {
		return nil, fmt.Errorf("MANIFEST_SCOPE_INVALID")
	}
	manifest, err := readIGVManifest(ctx, storage, job.ManifestKey)
	if err != nil {
		return nil, fmt.Errorf("MANIFEST_READ_FAILED")
	}
	raw, _ := json.Marshal(manifest)
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != job.ManifestSHA256 {
		return nil, fmt.Errorf("MANIFEST_CHANGED")
	}
	var plan []BAMRetentionObject
	if json.Unmarshal([]byte(job.PlanJSON), &plan) != nil {
		return nil, fmt.Errorf("PLAN_INVALID")
	}
	for _, obj := range plan {
		lower := strings.ToLower(obj.Key)
		if !strings.HasPrefix(obj.Key, prefix+"/") || (!strings.HasSuffix(lower, ".bam") && !strings.HasSuffix(lower, ".bai")) {
			return nil, fmt.Errorf("PLAN_SCOPE_INVALID")
		}
		if _, err := safeResultPackageRelativePath(prefix, obj.Key); err != nil {
			return nil, fmt.Errorf("PLAN_SCOPE_INVALID")
		}
	}
	return plan, nil
}

// Inspect performs no DB/storage mutations, even when the completion has not yet been backfilled.
func (s *BAMRetentionService) Inspect(ctx context.Context, taskID, attempt string) (*BAMRetentionPlan, error) {
	if s.cfg.Storage.BAMRetentionDays != 7 {
		return nil, fmt.Errorf("seven-day BAM policy disabled")
	}
	var job model.BAMRetentionJob
	err := database.DB.WithContext(ctx).Where("task_uuid = ? AND attempt_id = ?", taskID, attempt).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var task model.Task
		if err := database.DB.WithContext(ctx).Unscoped().Where("uuid = ? AND execution_attempt_id = ? AND status = ? AND executor = ?", taskID, attempt, model.TaskStatusCompleted, model.ExecutorCVM).First(&task).Error; err != nil {
			return nil, fmt.Errorf("completed attempt unavailable")
		}
		if task.FinishedAt == nil {
			return nil, fmt.Errorf("completion timestamp missing")
		}
		job = model.BAMRetentionJob{TaskUUID: task.UUID, OrgID: task.ExternalOrgID, AttemptID: attempt, Bucket: s.cfg.Storage.S3Bucket, CompletedAt: *task.FinishedAt, ExpiresAt: task.FinishedAt.Add(7 * 24 * time.Hour), Status: "pending", PlanJSON: "[]"}
	} else if err != nil {
		return nil, fmt.Errorf("retention state unavailable")
	}
	objects, err := s.prepare(ctx, &job)
	if err != nil {
		return nil, err
	}
	return &BAMRetentionPlan{Job: job, Objects: objects, Due: !time.Now().Before(job.ExpiresAt)}, nil
}

func (s *BAMRetentionService) Execute(ctx context.Context, id string) error {
	if s.cfg.Storage.BAMRetentionDays != 7 || !s.cfg.Storage.BAMCleanupEnabled {
		return fmt.Errorf("cleanup not enabled")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	lease := uuid.NewString()
	var job model.BAMRetentionJob
	err := database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("id = ?", id).First(&job).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if now.Before(job.ExpiresAt) || job.Status == "deleted" || job.Status == "no_bam" || (job.LeaseUntil != nil && now.Before(*job.LeaseUntil)) {
			return fmt.Errorf("cleanup not due or already claimed")
		}
		until := now.Add(5 * time.Minute)
		job.LeaseID = lease
		job.LeaseUntil = &until
		job.Status = "processing"
		job.Attempts++
		return tx.Save(&job).Error
	})
	if err != nil {
		return err
	}
	plan, runErr := s.prepare(ctx, &job)
	if runErr == nil {
		// Commit exact targets BEFORE the first deletion, so a crash can resume partial cleanup.
		saved := database.DB.WithContext(ctx).Model(&model.BAMRetentionJob{}).Where("id = ? AND lease_id = ?", job.ID, lease).Updates(map[string]interface{}{"manifest_key": job.ManifestKey, "manifest_sha256": job.ManifestSHA256, "plan_json": job.PlanJSON})
		runErr = saved.Error
		if runErr == nil && saved.RowsAffected != 1 {
			runErr = fmt.Errorf("cleanup lease changed")
		}
	}
	if runErr == nil {
		storage, e := newS3Storage(ctx, s.cfg.Storage)
		if e != nil {
			runErr = fmt.Errorf("STORAGE_UNAVAILABLE")
		} else {
			// Validate ALL remaining identities before deleting the first object.
			for _, obj := range plan {
				meta, e := s.head(ctx, storage, obj.Key)
				if e != nil {
					runErr = e
					break
				}
				if meta != nil && (meta.Size != obj.Size || meta.ETag != obj.ETag) {
					runErr = fmt.Errorf("OBJECT_CHANGED")
					break
				}
			}
			if runErr == nil {
				for _, obj := range plan {
					meta, e := s.head(ctx, storage, obj.Key)
					if e != nil {
						runErr = e
						break
					}
					if meta != nil {
						if meta.Size != obj.Size || meta.ETag != obj.ETag {
							runErr = fmt.Errorf("OBJECT_CHANGED")
							break
						}
						if storage.delete(ctx, obj.Key) != nil {
							runErr = fmt.Errorf("OBJECT_DELETE_FAILED")
							break
						}
					}
					meta, e = s.head(ctx, storage, obj.Key)
					if e != nil {
						runErr = e
						break
					}
					if meta != nil {
						runErr = fmt.Errorf("DELETE_NOT_CONFIRMED")
						break
					}
					if database.DB.WithContext(ctx).Create(&model.BAMRetentionEvent{JobID: job.ID, Action: "object_absent_confirmed", ObjectKey: obj.Key}).Error != nil {
						runErr = fmt.Errorf("AUDIT_WRITE_FAILED")
						break
					}
				}
			}
		}
	}
	// Independent bounded context releases a lease even when storage timed out.
	finalCtx, finalCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer finalCancel()
	status := "deleted"
	code := ""
	now := time.Now().UTC()
	var retry *time.Time
	var deleted *time.Time
	if runErr != nil {
		status = "retry"
		code = "CLEANUP_FAILED"
		switch runErr.Error() {
		case "BUCKET_CHANGED", "IDENTITY_INVALID", "OBJECT_CHANGED", "MANIFEST_CHANGED", "INDEX_MAPPING_AMBIGUOUS", "BAM_MAPPING_AMBIGUOUS", "OBJECT_HEAD_FAILED", "OBJECT_DELETE_FAILED", "DELETE_NOT_CONFIRMED", "AUDIT_WRITE_FAILED", "MANIFEST_UNAVAILABLE", "MANIFEST_READ_FAILED", "ARCHIVE_LIST_FAILED", "STORAGE_UNAVAILABLE", "OBJECT_SCOPE_INVALID", "PLAN_INVALID", "PLAN_SCOPE_INVALID", "MANIFEST_SCOPE_INVALID", "OBJECT_DISAPPEARED_DURING_PLAN":
			code = runErr.Error()
		}
		next := now.Add(time.Hour)
		retry = &next
	} else {
		deleted = &now
		if len(plan) == 0 {
			status = "no_bam"
		}
	}
	finishErr := database.DB.WithContext(finalCtx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.BAMRetentionJob{}).Where("id = ? AND lease_id = ?", job.ID, lease).Updates(map[string]interface{}{"status": status, "error_code": code, "next_retry_at": retry, "deleted_at": deleted, "lease_until": nil, "lease_id": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("cleanup lease changed")
		}
		return tx.Create(&model.BAMRetentionEvent{JobID: job.ID, Action: status, ErrorCode: code}).Error
	})
	if finishErr != nil {
		return fmt.Errorf("cleanup final audit unavailable")
	}
	return runErr
}
