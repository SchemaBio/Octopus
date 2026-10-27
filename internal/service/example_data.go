package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/exampledata"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ExampleDataService struct {
	cfg   *config.Config
	clock func() time.Time
}

func (s *ExampleDataService) now() time.Time {
	if s.clock != nil {
		return s.clock().UTC()
	}
	return time.Now().UTC()
}

func NewExampleDataService(cfg *config.Config) *ExampleDataService {
	return &ExampleDataService{cfg: cfg}
}

func (s *ExampleDataService) Enqueue(ctx context.Context, in model.ExampleDataGrant) (*model.ExampleDataGrant, error) {
	if !s.cfg.IsSaaS() {
		return nil, errors.New("example provisioning unavailable")
	}
	for _, id := range []string{in.ID, in.OrgID} {
		v, err := uuid.Parse(id)
		if err != nil || v.String() != id {
			return nil, errors.New("invalid identity")
		}
	}
	if in.UserID == 0 || in.ApprovedAt.IsZero() || in.ApprovedAt.After(time.Now().Add(time.Minute)) {
		return nil, errors.New("invalid approval")
	}
	if in.TemplateVersion == "" || len(in.TemplateVersion) > 64 {
		return nil, errors.New("invalid template version")
	}
	var existing model.ExampleDataGrant
	err := database.DB.WithContext(ctx).First(&existing, "org_id=?", in.OrgID).Error
	if err == nil {
		if existing.ID != in.ID || existing.UserID != in.UserID || existing.TemplateVersion != in.TemplateVersion || !existing.ApprovedAt.Equal(in.ApprovedAt) {
			return nil, errors.New("grant identity conflict")
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	expired := !s.now().Before(in.ApprovedAt.Add(168 * time.Hour))
	if !expired {
		info, err := ExampleTemplate(ctx, s.cfg, in.TemplateVersion)
		if err != nil || !info.Available || info.Version != in.TemplateVersion {
			return nil, errors.New("template unavailable")
		}
	}
	grant := model.ExampleDataGrant{ID: in.ID, OrgID: in.OrgID, UserID: in.UserID, TemplateVersion: in.TemplateVersion, ApprovedAt: in.ApprovedAt.UTC(), Status: "pending", SampleUUID: uuid.NewString(), TaskUUID: uuid.NewString(), AttemptUUID: uuid.NewString(), Read1UUID: uuid.NewString(), Read2UUID: uuid.NewString(), NextRetryAt: time.Now().UTC()}
	if expired {
		grant.Status = "expired"
	}
	err = database.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&grant).Error
	if err != nil {
		return nil, err
	}
	if err := database.DB.WithContext(ctx).First(&grant, "org_id=?", in.OrgID).Error; err != nil {
		return nil, err
	}
	if grant.ID != in.ID || grant.UserID != in.UserID || grant.TemplateVersion != in.TemplateVersion || !grant.ApprovedAt.Equal(in.ApprovedAt) {
		return nil, errors.New("grant identity conflict")
	}
	return &grant, nil
}

func (s *ExampleDataService) Start(ctx context.Context) {
	if !s.cfg.IsSaaS() {
		return
	}
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			var grants []model.ExampleDataGrant
			if database.DB.WithContext(ctx).Where("status NOT IN ? AND next_retry_at<=? AND (lease_until IS NULL OR lease_until<=?)", []string{"ready", "expired"}, time.Now(), time.Now()).Order("next_retry_at").Limit(5).Find(&grants).Error == nil {
				for _, g := range grants {
					s.process(ctx, g)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

func (s *ExampleDataService) eligible(ctx context.Context, g model.ExampleDataGrant) (bool, error) {
	c := NewOverlayClient(s.cfg.Overlay)
	if c == nil {
		return false, errors.New("control plane unavailable")
	}
	var response struct {
		Active bool                   `json:"active"`
		Grant  model.ExampleDataGrant `json:"grant"`
	}
	status, _, err := c.postJSON(ctx, "/api/v1/overlay/example-data/eligibility", map[string]string{"id": g.ID, "org_id": g.OrgID}, &response)
	if err != nil || status != 200 {
		return false, errors.New("eligibility unavailable")
	}
	r := response.Grant
	if r.ID != g.ID || r.OrgID != g.OrgID || r.UserID != g.UserID || r.TemplateVersion != g.TemplateVersion || !r.ApprovedAt.Equal(g.ApprovedAt) {
		return false, errors.New("approval identity mismatch")
	}
	return response.Active, nil
}

func (s *ExampleDataService) process(parent context.Context, g model.ExampleDataGrant) {
	lease := uuid.NewString()
	until := time.Now().UTC().Add(5 * time.Minute)
	claim := database.DB.WithContext(parent).Model(&model.ExampleDataGrant{}).Where("id=? AND status NOT IN ? AND (lease_until IS NULL OR lease_until<=?)", g.ID, []string{"ready", "expired"}, time.Now()).Updates(map[string]interface{}{"lease_id": lease, "lease_until": until, "status": "copying"})
	if claim.Error != nil || claim.RowsAffected != 1 {
		return
	}
	g.LeaseID = lease
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				r := database.DB.WithContext(ctx).Model(&model.ExampleDataGrant{}).Where("id=? AND lease_id=?", g.ID, lease).Update("lease_until", time.Now().UTC().Add(5*time.Minute))
				if r.Error != nil || r.RowsAffected != 1 {
					cancel()
					return
				}
			}
		}
	}()
	status, code := s.provision(ctx, &g)
	_ = database.DB.WithContext(parent).Model(&model.ExampleDataGrant{}).Where("id=? AND lease_id=? AND status<>?", g.ID, lease, "ready").Updates(map[string]interface{}{"status": status, "last_error": code, "lease_id": "", "lease_until": nil, "next_retry_at": time.Now().UTC().Add(time.Minute)}).Error
}

func exampleDataPrefix(g *model.ExampleDataGrant) string {
	return path.Join("organizations", g.OrgID, "uploads/fastq/paired", g.ID) + "/"
}
func exampleObjectKey(g *model.ExampleDataGrant, o exampledata.Object) string {
	if o.Role == exampledata.Read1 || o.Role == exampledata.Read2 {
		return exampleDataPrefix(g) + path.Base(o.Path)
	}
	return resultPackagePrefix(&model.Task{UUID: g.TaskUUID, ExternalOrgID: g.OrgID, ExecutionAttemptID: g.AttemptUUID}) + "/" + path.Base(o.Path)
}

func (s *ExampleDataService) provision(ctx context.Context, g *model.ExampleDataGrant) (string, string) {
	store, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return "failed", "storage_unavailable"
	}
	if !s.now().Before(g.ApprovedAt.Add(168 * time.Hour)) {
		prefix, _ := retentionPrefix(g.OrgID, g.TaskUUID)
		if store.purgePrefix(ctx, prefix) != nil || store.purgePrefix(ctx, exampleDataPrefix(g)) != nil {
			return "failed", "cleanup_pending"
		}
		if database.DB.WithContext(ctx).Where("grant_id=?", g.ID).Delete(&model.ExampleDataObject{}).Error != nil {
			return "failed", "cleanup_pending"
		}
		return "expired", ""
	}
	if !exampleAvailable(s.cfg) {
		return "paused", "cleanup_policy_unavailable"
	}
	active, err := s.eligible(ctx, *g)
	if err != nil {
		return "failed", "eligibility_unavailable"
	}
	if !active {
		return "paused", ""
	}
	var template model.ExampleTemplate
	if database.DB.WithContext(ctx).First(&template, "version=?", g.TemplateVersion).Error != nil {
		return "failed", "template_unavailable"
	}
	var m exampledata.Manifest
	if json.Unmarshal([]byte(template.ManifestJSON), &m) != nil || m.Validate() != nil {
		return "failed", "template_invalid"
	}
	refs := map[string]string{}
	seen := map[string]bool{}
	for _, o := range m.Objects {
		target := exampleObjectKey(g, o)
		if seen[target] {
			return "failed", "duplicate_target"
		}
		seen[target] = true
		refs[o.Path] = "cos://" + store.bucket + "/" + target
		if err := s.copyObject(ctx, store, g, o, target); err != nil {
			return "failed", "copy_verification_failed"
		}
	}
	outputs, err := exampledata.RewriteOutputs(m.Outputs, refs)
	if err != nil {
		return "failed", "outputs_invalid"
	}
	manifestKey := resultPackagePrefix(&model.Task{UUID: g.TaskUUID, ExternalOrgID: g.OrgID, ExecutionAttemptID: g.AttemptUUID}) + "/outputs.resolved.json"
	if err := store.putUnchecked(ctx, manifestKey, "application/json", outputs); err != nil {
		return "failed", "manifest_write_failed"
	}
	// Only small textual importer inputs are staged locally; no BAM/FASTQ cache.
	dir, err := os.MkdirTemp("", g.TaskUUID+"-octopus-example-*")
	if err != nil {
		return "failed", "staging_failed"
	}
	defer os.RemoveAll(dir)
	if os.WriteFile(filepath.Join(dir, "outputs.resolved.json"), outputs, 0600) != nil {
		return "failed", "staging_failed"
	}
	for _, o := range m.Objects {
		if o.Role != exampledata.Result || !isStructuredResultFile(path.Base(o.Path)) {
			continue
		}
		if o.Size > 128<<20 {
			return "failed", "structured_file_too_large"
		}
		r, err := store.open(ctx, exampleObjectKey(g, o))
		if err != nil {
			return "failed", "staging_failed"
		}
		f, err := os.OpenFile(filepath.Join(dir, path.Base(o.Path)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			r.Close()
			return "failed", "staging_failed"
		}
		_, err = io.Copy(f, io.LimitReader(r, o.Size+1))
		r.Close()
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return "failed", "staging_failed"
		}
	}
	active, err = s.eligible(ctx, *g)
	if err != nil {
		return "failed", "eligibility_unavailable"
	}
	if !active {
		return "paused", ""
	}
	if err := s.publish(ctx, g, m, dir); err != nil {
		return "failed", "publish_failed"
	}
	return "ready", ""
}

func (s *ExampleDataService) copyObject(ctx context.Context, store *s3Storage, g *model.ExampleDataGrant, o exampledata.Object, target string) error {
	row := model.ExampleDataObject{GrantID: g.ID, Path: o.Path, ObjectKey: target, SHA256: o.SHA256}
	if err := database.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	if err := database.DB.WithContext(ctx).First(&row, "grant_id=? AND path=?", g.ID, o.Path).Error; err != nil {
		return err
	}
	if row.ObjectKey != target || row.SHA256 != o.SHA256 {
		return errors.New("copy identity changed")
	}
	verify := func() error {
		r, e := store.open(ctx, target)
		if e != nil {
			return e
		}
		defer r.Close()
		return exampledata.VerifyObject(o, r)
	}
	if row.VerifiedAt != nil {
		return verify()
	}
	source := exampleTemplatePrefix + g.TemplateVersion + "/" + o.Path
	if !s.now().Before(g.ApprovedAt.Add(168 * time.Hour)) {
		return errors.New("grant expired")
	}
	if o.Size <= 5*1024*1024*1024 {
		_, err := store.client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(target), CopySource: aws.String(url.PathEscape(store.bucket + "/" + source))})
		if err != nil {
			return err
		}
	} else {
		id, err := store.createMultipartUnchecked(ctx, target, "application/octet-stream")
		if err != nil {
			return err
		}
		complete := false
		defer func() {
			if !complete {
				cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				_ = store.abortMultipart(cleanup, target, id)
			}
		}()
		var parts []s3types.CompletedPart
		for offset, number := int64(0), int32(1); offset < o.Size; offset, number = offset+(256<<20), number+1 {
			end := offset + (256 << 20) - 1
			if end >= o.Size {
				end = o.Size - 1
			}
			out, err := store.client.UploadPartCopy(ctx, &s3.UploadPartCopyInput{Bucket: aws.String(store.bucket), Key: aws.String(target), UploadId: aws.String(id), PartNumber: aws.Int32(number), CopySource: aws.String(url.PathEscape(store.bucket + "/" + source)), CopySourceRange: aws.String(fmt.Sprintf("bytes=%d-%d", offset, end))})
			if err != nil {
				return err
			}
			if out.CopyPartResult == nil {
				return errors.New("missing copy result")
			}
			parts = append(parts, s3types.CompletedPart{PartNumber: aws.Int32(number), ETag: out.CopyPartResult.ETag})
		}
		_, err = store.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String(store.bucket), Key: aws.String(target), UploadId: aws.String(id), MultipartUpload: &s3types.CompletedMultipartUpload{Parts: parts}})
		if err != nil {
			return err
		}
		complete = true
	}
	if err := verify(); err != nil {
		return err
	}
	return database.DB.WithContext(ctx).Model(&row).Update("verified_at", time.Now().UTC()).Error
}

func (s *ExampleDataService) publish(ctx context.Context, g *model.ExampleDataGrant, m exampledata.Manifest, dir string) error {
	return database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked model.ExampleDataGrant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id=? AND lease_id=?", g.ID, g.LeaseID).Error; err != nil {
			return err
		}
		if locked.Status == "ready" {
			return nil
		}
		if !s.now().Before(g.ApprovedAt.Add(168 * time.Hour)) {
			return errors.New("grant expired")
		}
		d, _ := exampledata.ApprovalDeadlines(g.ApprovedAt)
		sample := model.Sample{UUID: g.SampleUUID, ExternalOrgID: g.OrgID, InternalID: m.SampleID, Gender: model.SampleGender(m.Sample.Gender), Age: m.Sample.Age, SampleType: model.SampleTypeFrontend(m.Sample.SampleType), ClinicalDiagnosis: m.Sample.ClinicalDiagnosis, HPOTerms: string(m.Sample.HPOTerms), CreatedBy: g.UserID, CreatedAt: g.ApprovedAt, ExpiresAt: &d.Sample, Status: model.SampleStatusCompleted, MatchStatus: model.SampleMatchMatched, MatchMode: model.SampleMatchModeManual, Origin: "example"}
		pair := &model.MatchedPair{}
		for _, o := range m.Objects {
			if o.Role == exampledata.Read1 {
				pair.R1Path = exampleObjectKey(g, o)
			}
			if o.Role == exampledata.Read2 {
				pair.R2Path = exampleObjectKey(g, o)
			}
		}
		sample.SetMatchedPair(pair)
		sample.SetSubmissionInfo(model.SubmissionInfo{})
		sample.SetProjectInfo(model.ProjectInfo{})
		sample.SetFamilyHistory(model.FamilyHistoryInfo{})
		if sample.ClinicalDiagnosis == "" {
			sample.ClinicalDiagnosis = "[]"
		}
		if sample.HPOTerms == "" {
			sample.HPOTerms = "[]"
		}
		if err := tx.Create(&sample).Error; err != nil {
			return err
		}
		var left, right uint
		for _, o := range m.Objects {
			if o.Role != exampledata.Read1 && o.Role != exampledata.Read2 {
				continue
			}
			id, read := g.Read1UUID, model.ReadTypeRead1
			if o.Role == exampledata.Read2 {
				id, read = g.Read2UUID, model.ReadTypeRead2
			}
			asset := model.DataAsset{UUID: id, ExternalOrgID: g.OrgID, CreatedBy: g.UserID, Provider: model.UploadProviderS3, StorageKey: exampleObjectKey(g, o), FileName: path.Base(o.Path), InternalID: m.SampleID, FileSize: o.Size, ReadType: read, ReferenceGenome: m.ReferenceGenome, Status: model.FileStatusCompleted, Source: model.DataAssetSource("example"), ExpiresAt: &d.Data, CreatedAt: g.ApprovedAt, ValidationStatus: "valid", ValidationSHA256: o.SHA256}
			if err := tx.Create(&asset).Error; err != nil {
				return err
			}
			if read == model.ReadTypeRead1 {
				left = asset.ID
			} else {
				right = asset.ID
			}
		}
		if err := tx.Create(&model.SampleDataLink{SampleID: sample.ID, ExternalOrgID: g.OrgID, Read1AssetID: left, Read2AssetID: right, MatchMode: model.SampleMatchModeManual, MatchedBy: g.UserID, MatchedAt: g.ApprovedAt}).Error; err != nil {
			return err
		}
		inputs, _ := json.Marshal(map[string]string{"reference_genome": m.ReferenceGenome, "sample_name": m.SampleID})
		task := model.Task{ID: g.TaskUUID, UUID: g.TaskUUID, Name: "Example: " + m.SampleID, SampleID: g.SampleUUID, SampleIDRef: sample.ID, InternalID: m.SampleID, Pipeline: m.Pipeline, Template: m.Pipeline, PipelineVersion: m.PipelineVersion, Executor: model.ExecutorExample, InputJSON: string(inputs), Status: model.TaskStatusCompleted, Progress: 100, ExternalOrgID: g.OrgID, TenantID: "org:" + g.OrgID, CreatedBy: g.UserID, CreatedAt: g.ApprovedAt, FinishedAt: &g.ApprovedAt, ExpiresAt: &d.Result, ExecutionAttemptID: g.AttemptUUID, ExecutionPhase: "terminal", ResultImportStatus: model.ResultImportStatusSuccess, Origin: "example"}
		task.TenantID = model.TenantIDForTask(&task)
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		batch := model.ResultImportBatch{TaskUUID: task.UUID, TenantID: task.TenantID, ExecutionAttemptID: g.AttemptUUID, Source: "example", Status: model.ResultImportBatchStatusRunning, Fingerprint: g.ID, StartedAt: time.Now().UTC()}
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		imp := NewImporter(s.cfg)
		imp.repo = repository.NewResultRepository().WithDB(tx)
		imp.task = &task
		imp.tenantID = task.TenantID
		imp.attemptID = g.AttemptUUID
		imp.importBatchID = batch.ID
		result, err := imp.importFromArchive(task.UUID, dir)
		if err != nil {
			return err
		}
		if result == nil || !result.Success {
			return errors.New("result import failed")
		}
		counts, _ := json.Marshal(result.Counts)
		now := time.Now().UTC()
		if err := tx.Model(&batch).Updates(map[string]interface{}{"status": model.ResultImportBatchStatusSuccess, "counts_json": string(counts), "finished_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&task).Updates(map[string]interface{}{"result_imported_at": now, "result_import_fingerprint": g.ID}).Error; err != nil {
			return err
		}
		if err := repository.RegisterBAMRetention(tx, &task, s.cfg.Storage.S3Bucket); err != nil {
			return err
		}
		if err := tx.Where("grant_id=?", g.ID).Delete(&model.ExampleDataObject{}).Error; err != nil {
			return err
		}
		return tx.Model(&locked).Updates(map[string]interface{}{"status": "ready", "lease_id": "", "lease_until": nil, "last_error": ""}).Error
	})
}
