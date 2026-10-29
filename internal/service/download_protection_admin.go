package service

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrDownloadRevision = errors.New("download_revision_conflict")

func NewDownloadProtectionAdminService(cfg *config.Config) *ResultDownloadService {
	return &ResultDownloadService{cfg: cfg}
}

func RecordDownloadSyncAction(ctx context.Context, actor uint, success bool) error {
	action := "admin_sync_failed"
	if success {
		action = "admin_sync"
	}
	return protectionEvent(database.DB.WithContext(ctx), "", action, actor, "")
}

type DownloadProtectionDetail struct {
	Grant     model.ResultDownload            `json:"grant"`
	Requests  []model.DownloadNetworkRequest  `json:"requests"`
	Events    []model.DownloadProtectionEvent `json:"events"`
	Issuances []model.DownloadIssuance        `json:"issuances"`
}

func DownloadProtectionDetailFor(ctx context.Context, id string) (DownloadProtectionDetail, error) {
	d := DownloadProtectionDetail{Requests: []model.DownloadNetworkRequest{}, Events: []model.DownloadProtectionEvent{}, Issuances: []model.DownloadIssuance{}}
	db := database.DB.WithContext(ctx)
	if err := db.First(&d.Grant, "id = ?", id).Error; err != nil {
		return d, err
	}
	if err := db.Where("grant_id = ?", id).Order("created_at DESC").Limit(100).Find(&d.Requests).Error; err != nil {
		return d, err
	}
	if err := db.Where("grant_id = ?", id).Order("created_at DESC").Limit(100).Find(&d.Events).Error; err != nil {
		return d, err
	}
	err := db.Where("grant_id = ?", id).Order("created_at DESC").Limit(100).Find(&d.Issuances).Error
	return d, err
}

// The administrator can pause or rebind, never erase usage or extend a grant.
func (s *ResultDownloadService) ManageProtection(ctx context.Context, id, action, requestID, reason string, revision uint64, actor uint) error {
	if utf8.RuneCountInString(reason) < 3 || utf8.RuneCountInString(reason) > 500 {
		return errors.New("download_reason_required")
	}
	if action != "pause" && action != "approve" && action != "reject" {
		return errors.New("download_action_invalid")
	}
	var snapshot model.ResultDownload
	if action == "approve" {
		if err := database.DB.WithContext(ctx).First(&snapshot, "id = ?", id).Error; err != nil {
			return err
		}
		var task model.Task
		if err := database.DB.WithContext(ctx).First(&task, "uuid = ?", snapshot.TaskUUID).Error; err != nil {
			return err
		}
		if task.ExecutionAttemptID != snapshot.AttemptID {
			return errors.New("download_object_changed")
		}
		if task.ExpiresAt != nil && !time.Now().Before(*task.ExpiresAt) {
			return errors.New("download_grant_unavailable")
		}
		if snapshot.Kind == "bam" {
			deadline, err := bamRetentionDeadline(ctx, s.cfg, &task)
			if err != nil {
				return err
			}
			if deadline != nil && !time.Now().Before(*deadline) {
				return errors.New("download_grant_unavailable")
			}
		}
		if err := s.verifyDownloadETag(ctx, snapshot); err != nil {
			return err
		}
	}
	return database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var q model.ResultDownload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&q, "id = ?", id).Error; err != nil {
			return err
		}
		if q.Revision != revision {
			return ErrDownloadRevision
		}
		if q.ProtectionVersion == 0 {
			return errors.New("download_legacy_grant")
		}
		if action == "pause" {
			if q.IssuanceState != "threshold" {
				q.IssuanceState = "paused"
			}
		} else {
			var req model.DownloadNetworkRequest
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND grant_id = ? AND status = ?", requestID, q.ID, "pending").First(&req).Error; err != nil {
				return errors.New("download_network_pending")
			}
			if action == "reject" {
				req.Status = "rejected"
			} else {
				now := time.Now().UTC()
				if q.LinkExpiresAt == nil || !now.Before(*q.LinkExpiresAt) || q.RefundedAt != nil || q.ObservedBytes >= q.TrafficThresholdBytes || q.IssuanceState != "active" {
					return errors.New("download_grant_unavailable")
				}
				var latest model.DownloadIssuance
				err := tx.Where("grant_id = ?", q.ID).Order("credential_expires_at DESC").First(&latest).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				wait := now
				if latest.CredentialExpiresAt.After(wait) {
					wait = latest.CredentialExpiresAt
				}
				if latest.ExpiresAt.After(wait) {
					wait = latest.ExpiresAt
				}
				wait = wait.Add(30 * time.Second)
				if !wait.Before(*q.LinkExpiresAt) {
					return errors.New("download_rebind_no_time")
				}
				req.NotBefore = &wait
				req.Status = "approved_wait"
				q.IssuanceState = "rebind_wait"
			}
			req.Reason = reason
			req.Revision++
			if err := tx.Save(&req).Error; err != nil {
				return err
			}
		}
		q.Revision++
		if err := tx.Save(&q).Error; err != nil {
			return err
		}
		return protectionEvent(tx, q.ID, "admin_"+action, actor, reason)
	})
}

func (s *ResultDownloadService) verifyDownloadETag(ctx context.Context, q model.ResultDownload) error {
	store, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return errors.New("download_object_unavailable")
	}
	m, err := store.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(q.ObjectKey)})
	if err != nil || aws.ToInt64(m.ContentLength) != q.SizeBytes || aws.ToString(m.ETag) != q.ETag {
		return errors.New("download_object_changed")
	}
	return nil
}
