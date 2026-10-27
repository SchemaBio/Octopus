package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/exampledata"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const exampleTemplatePrefix = "platform/example-data/"

// Switching the offered version never alters queued grants or their deadlines.
func ActivateExampleTemplate(ctx context.Context, cfg *config.Config, version string) error {
	if !cfg.IsSaaS() {
		return errors.New("example templates are SaaS-only")
	}
	return database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if version == "" {
			return tx.Where("id=?", 1).Delete(&model.ExampleTemplateActive{}).Error
		}
		var template model.ExampleTemplate
		if err := tx.First(&template, "version=?", version).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"version"})}).Create(&model.ExampleTemplateActive{ID: 1, Version: version}).Error
	})
}

func MigrateExampleData(cfg *config.Config) error {
	if !cfg.IsSaaS() {
		return nil
	}
	return database.DB.AutoMigrate(&model.ExampleTemplate{}, &model.ExampleTemplateActive{}, &model.ExampleDataGrant{}, &model.ExampleDataObject{})
}

func exampleAvailable(cfg *config.Config) bool {
	return cfg.IsSaaS() && cfg.Storage.Provider == "s3" && cfg.Storage.RetentionDays == 7 && cfg.Storage.BAMRetentionDays == 7 && cfg.Storage.BAMCleanupEnabled && cfg.RetentionCleanupEnabled && cfg.RetentionBackupPolicyVerified
}

type ExampleTemplateInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
}

func ExampleTemplate(ctx context.Context, cfg *config.Config, version string) (ExampleTemplateInfo, error) {
	info := ExampleTemplateInfo{}
	if !exampleAvailable(cfg) {
		return info, nil
	}
	if version == "" {
		var active model.ExampleTemplateActive
		if err := database.DB.WithContext(ctx).First(&active, 1).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return info, nil
			}
			return info, err
		}
		version = active.Version
	}
	var t model.ExampleTemplate
	if err := database.DB.WithContext(ctx).First(&t, "version=?", version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return info, nil
		}
		return info, err
	}
	var m exampledata.Manifest
	if json.Unmarshal([]byte(t.ManifestJSON), &m) != nil || m.Validate() != nil {
		return info, errors.New("invalid template")
	}
	info.Version = t.Version
	store, err := newS3Storage(ctx, cfg.Storage)
	if err != nil {
		return info, nil
	}
	for _, object := range m.Objects {
		size, err := store.stat(ctx, exampleTemplatePrefix+t.Version+"/"+object.Path)
		if err != nil || size != object.Size {
			return info, nil
		}
	}
	info.Available = true
	return info, nil
}

// PublishExampleTemplate publishes a previously curated local package. The
// database lock prevents replacement of any published version, even when inactive.
func PublishExampleTemplate(ctx context.Context, cfg *config.Config, root string, m exampledata.Manifest, activate bool) error {
	if !cfg.IsSaaS() || cfg.Storage.Provider != "s3" {
		return errors.New("example templates are SaaS-only")
	}
	if err := m.ValidateFiles(root); err != nil {
		return err
	}
	store, err := newS3Storage(ctx, cfg.Storage)
	if err != nil {
		return err
	}
	return database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "example-template:"+m.Version).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.ExampleTemplate{}).Where("version=?", m.Version).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errors.New("template version already published")
		}
		for _, o := range m.Objects {
			f, err := os.Open(filepath.Join(root, filepath.FromSlash(o.Path)))
			if err != nil {
				return err
			}
			err = store.uploadExampleFile(ctx, exampleTemplatePrefix+m.Version+"/"+o.Path, f, o.Size)
			f.Close()
			if err != nil {
				return err
			}
			r, err := store.open(ctx, exampleTemplatePrefix+m.Version+"/"+o.Path)
			if err != nil {
				return err
			}
			err = exampledata.VerifyObject(o, r)
			r.Close()
			if err != nil {
				return err
			}
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		if err := tx.Create(&model.ExampleTemplate{Version: m.Version, ManifestJSON: string(raw), ManifestSHA256: hex.EncodeToString(sum[:])}).Error; err != nil {
			return err
		}
		if activate {
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"version"})}).Create(&model.ExampleTemplateActive{ID: 1, Version: m.Version}).Error
		}
		return nil
	})
}

// Multipart uploads support FASTQs/BAM larger than S3's single-PUT limit.
func (s *s3Storage) uploadExampleFile(ctx context.Context, key string, f *os.File, size int64) error {
	if size <= 64<<20 {
		return s.putReaderUnchecked(ctx, key, "application/octet-stream", f, size)
	}
	id, err := s.createMultipartUnchecked(ctx, key, "application/octet-stream")
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_ = s.abortMultipart(cleanup, key, id)
		}
	}()
	var parts []s3types.CompletedPart
	for offset, number := int64(0), int32(1); offset < size; offset, number = offset+(64<<20), number+1 {
		length := int64(64 << 20)
		if size-offset < length {
			length = size - offset
		}
		out, err := s.client.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: aws.String(id), PartNumber: aws.Int32(number), Body: io.NewSectionReader(f, offset, length), ContentLength: aws.Int64(length)})
		if err != nil {
			return err
		}
		parts = append(parts, s3types.CompletedPart{PartNumber: aws.Int32(number), ETag: out.ETag})
	}
	_, err = s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: aws.String(id), MultipartUpload: &s3types.CompletedMultipartUpload{Parts: parts}})
	complete = err == nil
	return err
}

// ExportExampleTemplate isolates one source sample and one archived single-sample
// analysis into a portable package. It copies no billing, audit or AI records.
func ExportExampleTemplate(ctx context.Context, cfg *config.Config, sampleID, taskID, version, root string) (exampledata.Manifest, error) {
	var m exampledata.Manifest
	if !cfg.IsSaaS() {
		return m, errors.New("example templates are SaaS-only")
	}
	var sample model.Sample
	var task model.Task
	if err := database.DB.WithContext(ctx).Where("uuid=?", sampleID).First(&sample).Error; err != nil {
		return m, err
	}
	if err := database.DB.WithContext(ctx).Where("uuid=? AND sample_id=? AND external_org_id=? AND status=?", taskID, sampleID, sample.ExternalOrgID, model.TaskStatusCompleted).First(&task).Error; err != nil {
		return m, err
	}
	var link model.SampleDataLink
	if err := database.DB.WithContext(ctx).Where("sample_id=? AND external_org_id=?", sample.ID, sample.ExternalOrgID).Order("match_mode DESC").First(&link).Error; err != nil {
		return m, err
	}
	var reads []model.DataAsset
	if err := database.DB.WithContext(ctx).Where("id IN ? AND external_org_id=? AND status=? AND expires_at>?", []uint{link.Read1AssetID, link.Read2AssetID}, sample.ExternalOrgID, model.FileStatusCompleted, time.Now().UTC()).Find(&reads).Error; err != nil || len(reads) != 2 {
		return m, errors.New("source read pair unavailable")
	}
	m = exampledata.Manifest{Version: version, SampleID: sample.InternalID, ReferenceGenome: TaskReferenceGenome(&task), Pipeline: task.Template, PipelineVersion: task.PipelineVersion, Sample: exampledata.Sample{Gender: string(sample.Gender), Age: sample.Age, SampleType: string(sample.SampleType), ClinicalDiagnosis: sample.ClinicalDiagnosis, HPOTerms: json.RawMessage(sample.HPOTerms)}}
	if len(m.Sample.HPOTerms) == 0 {
		m.Sample.HPOTerms = json.RawMessage("[]")
	}
	store, err := newS3Storage(ctx, cfg.Storage)
	if err != nil {
		return m, err
	}
	prefix := resultPackagePrefix(&task)
	objects, err := store.list(ctx, prefix+"/")
	if err != nil {
		return m, err
	}
	manifestKey, _, _, err := igvArchiveObjectIndex(prefix, objects)
	if err != nil {
		return m, err
	}
	reader, err := store.open(ctx, manifestKey)
	if err != nil {
		return m, err
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxArchiveOutputsJSONBytes+1))
	reader.Close()
	if err != nil || len(raw) > maxArchiveOutputsJSONBytes {
		return m, errors.New("source manifest unavailable")
	}
	var outputs interface{}
	if err := json.Unmarshal(raw, &outputs); err != nil {
		return m, err
	}
	declared := map[string]bool{}
	var collect func(interface{})
	collect = func(v interface{}) {
		switch x := v.(type) {
		case string:
			declared[path.Base(x)] = true
		case []interface{}:
			for _, y := range x {
				collect(y)
			}
		case map[string]interface{}:
			for _, y := range x {
				collect(y)
			}
		}
	}
	collect(outputs)
	refs := map[string]string{}
	baseRefs := map[string]string{}
	copyObject := func(key, relative string, role exampledata.Role) error {
		if key == "" || !strings.HasPrefix(key, "organizations/"+sample.ExternalOrgID+"/") {
			return errors.New("source object outside organization")
		}
		file := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		r, err := store.open(ctx, key)
		if err != nil {
			f.Close()
			return err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(f, h), r)
		r.Close()
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		m.Objects = append(m.Objects, exampledata.Object{Path: relative, Role: role, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
		refs[key] = "example://objects/" + relative
		refs["cos://"+store.bucket+"/"+key] = refs[key]
		base := path.Base(key)
		if _, exists := baseRefs[base]; exists {
			return errors.New("ambiguous archive basename")
		}
		baseRefs[base] = refs[key]
		return nil
	}
	for _, r := range reads {
		role := exampledata.Read1
		if r.ID == link.Read2AssetID {
			role = exampledata.Read2
		}
		if err := copyObject(r.StorageKey, "data/"+path.Base(r.StorageKey), role); err != nil {
			return m, err
		}
	}
	for _, o := range objects {
		if o.Key == manifestKey {
			continue
		}
		if !declared[path.Base(o.Key)] && !isStructuredResultFile(path.Base(o.Key)) {
			continue
		}
		rel, err := safeResultPackageRelativePath(prefix, o.Key)
		if err != nil {
			return m, err
		}
		lower := strings.ToLower(rel)
		role := exampledata.Result
		if strings.HasSuffix(lower, ".bam") {
			role = exampledata.BAM
		} else if strings.HasSuffix(lower, ".bai") {
			role = exampledata.BAMIndex
		} else if strings.HasSuffix(lower, ".zip") {
			continue
		}
		if err := copyObject(o.Key, "results/"+rel, role); err != nil {
			return m, err
		}
	}
	var portable func(interface{}) (interface{}, error)
	portable = func(v interface{}) (interface{}, error) {
		switch x := v.(type) {
		case string:
			if strings.HasSuffix(strings.ToLower(x), ".zip") {
				return nil, nil
			}
			if target, ok := refs[x]; ok {
				return target, nil
			}
			if target, ok := baseRefs[path.Base(x)]; ok && (strings.Contains(x, "/") || path.Ext(x) != "") {
				return target, nil
			}
			if strings.Contains(x, "://") || strings.HasPrefix(x, "/") || strings.Contains(x, "organizations/") {
				return nil, fmt.Errorf("unmapped source reference")
			}
			return x, nil
		case []interface{}:
			for i, y := range x {
				z, e := portable(y)
				if e != nil {
					return nil, e
				}
				x[i] = z
			}
			return x, nil
		case map[string]interface{}:
			for k, y := range x {
				z, e := portable(y)
				if e != nil {
					return nil, e
				}
				x[k] = z
			}
			return x, nil
		default:
			return v, nil
		}
	}
	outputs, err = portable(outputs)
	if err != nil {
		return m, err
	}
	m.Outputs, err = json.Marshal(outputs)
	if err != nil {
		return m, err
	}
	return m, m.ValidateFiles(root)
}
