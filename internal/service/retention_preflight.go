package service

import (
	"context"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gorm.io/gorm"
	"time"
)

type RetentionInventory struct {
	Kind      string            `json:"kind"`
	UUID      string            `json:"uuid"`
	OrgID     string            `json:"org_id"`
	ExpiresAt time.Time         `json:"expires_at"`
	Prefix    string            `json:"prefix,omitempty"`
	Current   []RetentionObject `json:"current,omitempty"`
	Versions  []RetentionObject `json:"versions,omitempty"`
	Multipart []RetentionObject `json:"multipart,omitempty"`
}
type RetentionObject struct {
	Key          string `json:"key"`
	Version      string `json:"version,omitempty"`
	UploadID     string `json:"upload_id,omitempty"`
	Size         int64  `json:"size,omitempty"`
	DeleteMarker bool   `json:"delete_marker,omitempty"`
}
type RetentionPreflight struct {
	ReadOnly                     bool                 `json:"read_only"`
	Cutoff                       time.Time            `json:"cutoff"`
	Edition                      string               `json:"edition"`
	SchemaCreateAllowed          bool                 `json:"schema_create_allowed"`
	CleanupEnabled               bool                 `json:"cleanup_enabled"`
	BackupVerificationConfigured bool                 `json:"backup_verification_configured"`
	CloudBackupVerification      string               `json:"cloud_backup_verification"`
	BucketVersioning             string               `json:"bucket_versioning,omitempty"`
	Resources                    []RetentionInventory `json:"resources"`
	Queue                        struct {
		Pending int64      `json:"pending"`
		Failed  int64      `json:"failed"`
		Leased  int64      `json:"leased"`
		Oldest  *time.Time `json:"oldest_due_at"`
	} `json:"queue"`
}

// Works against the old schema as well. Deadlines are always derived from
// original creation rather than a mutable client input or completion time.
func ResearchRetentionPreflight(ctx context.Context, db *gorm.DB, cfg *config.Config, now time.Time, objects bool) (*RetentionPreflight, error) {
	out := &RetentionPreflight{ReadOnly: true, Cutoff: now.UTC(), Edition: cfg.Edition, CleanupEnabled: cfg.RetentionCleanupEnabled, BackupVerificationConfigured: cfg.RetentionBackupPolicyVerified, CloudBackupVerification: "external verification required", Resources: []RetentionInventory{}}
	if err := db.Raw("SELECT has_schema_privilege(current_user,'public','CREATE')").Scan(&out.SchemaCreateAllowed).Error; err != nil {
		return nil, err
	}
	for _, item := range [][2]string{{"tasks", "task"}, {"samples", "sample"}} {
		var rows []RetentionInventory
		if err := db.Raw("SELECT uuid,external_org_id AS org_id,created_at+interval '720 hours' AS expires_at FROM "+item[0]+" WHERE created_at+interval '720 hours'<=? ORDER BY created_at,uuid", now.UTC()).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			row.Kind = item[1]
			if row.Kind == "task" {
				prefix, err := retentionPrefix(row.OrgID, row.UUID)
				if err != nil {
					return nil, err
				}
				row.Prefix = prefix
			}
			out.Resources = append(out.Resources, row)
		}
	}
	if db.Migrator().HasTable("retention_jobs") {
		if err := db.Raw("SELECT count(*) AS pending,count(*) FILTER(WHERE last_error<>'') AS failed,count(*) FILTER(WHERE lease_until>?) AS leased,min(due_at) AS oldest FROM retention_jobs", now.UTC()).Scan(&out.Queue).Error; err != nil {
			return nil, err
		}
	}
	if !objects {
		return out, nil
	}
	storage, err := newS3Storage(ctx, cfg.Storage)
	if err != nil {
		return nil, err
	}
	versioning, err := storage.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(storage.bucket)})
	if err != nil {
		return nil, err
	}
	out.BucketVersioning = string(versioning.Status)
	if out.BucketVersioning == "" {
		out.BucketVersioning = "unversioned"
	}
	for index := range out.Resources {
		row := &out.Resources[index]
		if row.Prefix == "" {
			continue
		}
		current := s3.NewListObjectsV2Paginator(storage.client, &s3.ListObjectsV2Input{Bucket: aws.String(storage.bucket), Prefix: aws.String(row.Prefix)})
		for current.HasMorePages() {
			page, err := current.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, v := range page.Contents {
				row.Current = append(row.Current, RetentionObject{Key: aws.ToString(v.Key), Size: aws.ToInt64(v.Size)})
			}
		}
		versions := s3.NewListObjectVersionsPaginator(storage.client, &s3.ListObjectVersionsInput{Bucket: aws.String(storage.bucket), Prefix: aws.String(row.Prefix)})
		for versions.HasMorePages() {
			page, err := versions.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, v := range page.Versions {
				row.Versions = append(row.Versions, RetentionObject{Key: aws.ToString(v.Key), Version: aws.ToString(v.VersionId), Size: aws.ToInt64(v.Size)})
			}
			for _, v := range page.DeleteMarkers {
				row.Versions = append(row.Versions, RetentionObject{Key: aws.ToString(v.Key), Version: aws.ToString(v.VersionId), DeleteMarker: true})
			}
		}
		uploads := s3.NewListMultipartUploadsPaginator(storage.client, &s3.ListMultipartUploadsInput{Bucket: aws.String(storage.bucket), Prefix: aws.String(row.Prefix)})
		for uploads.HasMorePages() {
			page, err := uploads.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, v := range page.Uploads {
				row.Multipart = append(row.Multipart, RetentionObject{Key: aws.ToString(v.Key), UploadID: aws.ToString(v.UploadId)})
			}
		}
	}
	return out, nil
}
