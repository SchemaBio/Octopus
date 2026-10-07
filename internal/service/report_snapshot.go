package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"io"
	"path"
	"strings"
	"time"
)

type reportSampleSnapshot struct {
	MemberID          string             `json:"member_id"`
	Role              string             `json:"role"`
	SampleID          string             `json:"sample_id"`
	InternalID        string             `json:"internal_id,omitempty"`
	Gender            model.SampleGender `json:"gender,omitempty"`
	Age               *int               `json:"age,omitempty"`
	ClinicalDiagnosis string             `json:"clinical_diagnosis,omitempty"`
	HPOTerms          string             `json:"hpo_terms,omitempty"`
	Available         bool               `json:"available"`
}
type reportSnapshot struct {
	Samples         []reportSampleSnapshot  `json:"samples"`
	Contract        string                  `json:"contract"`
	TaskUUID        string                  `json:"task_uuid"`
	AttemptID       string                  `json:"attempt_id"`
	Pipeline        string                  `json:"pipeline"`
	PipelineVersion string                  `json:"pipeline_version"`
	Reference       model.ResultReference   `json:"reference"`
	Members         []model.ResultMember    `json:"members"`
	QC              []model.QCMemberSummary `json:"qc"`
	Datasets        []reportDatasetVersion  `json:"datasets"`
	Reported        []reportSnapshotRow     `json:"reported_variants"`
}
type reportDatasetVersion struct {
	ID                 string `json:"id"`
	Table              string `json:"table"`
	Version            string `json:"version"`
	AdjustmentRevision uint64 `json:"adjustment_revision"`
}
type reportSnapshotRow struct {
	Table             string                 `json:"table"`
	RowID             string                 `json:"row_id"`
	DatasetVersion    string                 `json:"dataset_version"`
	AdjustmentVersion uint64                 `json:"adjustment_version"`
	Original          map[string]interface{} `json:"original"`
	Interpretation    map[string]interface{} `json:"interpretation"`
	Automatic         json.RawMessage        `json:"automatic_assessment,omitempty"`
}

// Preview verifies the same source proof used by generation without invoking a
// report provider or exposing raw annotations and signed object addresses.
func (s *ReportService) PreviewReport(ctx context.Context, t *model.Task) (map[string]interface{}, error) {
	raw, hash, e := s.prepareReportSnapshot(ctx, t)
	if e != nil {
		return nil, e
	}
	var snap reportSnapshot
	if e = json.Unmarshal([]byte(raw), &snap); e != nil {
		return nil, e
	}
	counts := map[string]int{}
	for _, r := range snap.Reported {
		counts[r.Table]++
	}
	return map[string]interface{}{"taskUuid": snap.TaskUUID, "attemptId": snap.AttemptID, "snapshotSha256": hash, "datasets": snap.Datasets, "reportedCounts": counts, "reportedTotal": len(snap.Reported), "memberCount": len(snap.Members), "qcMemberCount": len(snap.QC)}, nil
}

func (s *ReportService) prepareReportSnapshot(ctx context.Context, task *model.Task) (string, string, error) {
	rs := NewResultService(s.cfg)
	resultContext, e := rs.GetContext(ctx, task)
	if e != nil {
		return "", "", e
	}
	if resultContext.State != "ready" {
		return "", "", ErrResultPackageNotReady
	}
	snapshot := reportSnapshot{Contract: "report-snapshot-v2", TaskUUID: task.UUID, AttemptID: task.ExecutionAttemptID, Pipeline: task.Pipeline, PipelineVersion: task.PipelineVersion, Reference: resultContext.Reference, Members: resultContext.Members, QC: resultContext.QC, Datasets: []reportDatasetVersion{}, Reported: []reportSnapshotRow{}}
	e = database.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.Task
		if e := tx.Where("uuid=? AND execution_attempt_id=?", task.UUID, task.ExecutionAttemptID).First(&current).Error; e != nil {
			return ErrResourceConflict
		}
		for _, member := range snapshot.Members {
			entry := reportSampleSnapshot{MemberID: member.ID, Role: member.Role, SampleID: member.SampleID}
			if member.SampleID != "" {
				var sample model.Sample
				q := tx.Where("uuid=?", member.SampleID)
				if task.ExternalOrgID != "" {
					q = q.Where("external_org_id=?", task.ExternalOrgID)
				} else {
					q = q.Where("external_org_id='' AND created_by=?", task.CreatedBy)
				}
				err := q.First(&sample).Error
				if err == nil {
					entry.InternalID = sample.InternalID
					entry.Gender = sample.Gender
					entry.Age = sample.Age
					entry.ClinicalDiagnosis = sample.ClinicalDiagnosis
					entry.HPOTerms = sample.HPOTerms
					entry.Available = true
				} else if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
			}
			snapshot.Samples = append(snapshot.Samples, entry)
		}
		var datasets []model.ResultDataset
		if e := tx.Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=?", model.TenantIDForTask(task), task.UUID, task.ExecutionAttemptID).Order("\"table\",id").Find(&datasets).Error; e != nil {
			return e
		}
		for _, d := range datasets {
			snapshot.Datasets = append(snapshot.Datasets, reportDatasetVersion{d.ID, d.Table, d.DataVersion, d.AdjustmentRevision})
			var adjustments []model.ResultRowAdjustment
			if e := tx.Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND dataset_version=? AND payload_json->>'reported'='true'", d.TenantID, task.UUID, task.ExecutionAttemptID, d.Table, d.DataVersion).Order("row_id").Limit(10001).Find(&adjustments).Error; e != nil {
				return e
			}
			if len(snapshot.Reported)+len(adjustments) > 10000 {
				return errors.New("report snapshot exceeds 10000 selected variants")
			}
			if len(adjustments) == 0 {
				continue
			}
			cache, e := rs.historyDatasetCache(ctx, &d)
			if e != nil {
				return e
			}
			for _, a := range adjustments {
				var source model.HistoryReport
				if e := tx.Where("tenant_id=? AND task_uuid=? AND attempt_id=? AND dataset_id=? AND dataset_version=? AND row_id=? AND reported=true AND deleted=false", d.TenantID, task.UUID, task.ExecutionAttemptID, d.ID, d.DataVersion, a.RowID).First(&source).Error; e != nil {
					return errors.New("reported source identity is unavailable; run controlled history backfill")
				}
				if !browserOrdinalMatches(&d, a.RowID, source.RowOrdinal) {
					return ErrAdjustmentConflict
				}
				page, e := NewParquetReader().ReadPage(cache, source.RowOrdinal, 1)
				if e != nil || len(page.Rows) != 1 || page.TotalRows != d.Rows {
					return ErrParquetIncomplete
				}
				interpretation := map[string]interface{}{}
				if e = json.Unmarshal([]byte(a.PayloadJSON), &interpretation); e != nil {
					return e
				}
				row := reportSnapshotRow{Table: d.Table, RowID: a.RowID, DatasetVersion: d.DataVersion, AdjustmentVersion: a.Version, Original: page.Rows[0], Interpretation: interpretation}
				var auto model.ResultRowAutomaticAssessment
				e = tx.Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND row_id=? AND profile_version=?", d.TenantID, task.UUID, task.ExecutionAttemptID, d.Table, a.RowID, d.AutomaticAssessmentProfile).First(&auto).Error
				if e == nil {
					row.Automatic = json.RawMessage(auto.AssessmentJSON)
				} else if !errors.Is(e, gorm.ErrRecordNotFound) {
					return e
				}
				snapshot.Reported = append(snapshot.Reported, row)
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return "", "", e
	}
	data, e := json.Marshal(snapshot)
	if e != nil {
		return "", "", e
	}
	if len(data) > 10<<20 {
		return "", "", errors.New("report snapshot exceeds 10 MiB")
	}
	hash := sha256.Sum256(data)
	return string(data), hex.EncodeToString(hash[:]), nil
}

func (s *ReportService) GenerateScopedReportDownload(ctx context.Context, a model.OverlayActor, task *model.Task, req *model.ReportCreateRequest) (*ReportDownload, error) {
	if _, e := uuid.Parse(req.ClientRequestID); e != nil {
		return nil, errors.New("clientRequestId must be a UUID")
	}
	tmpl, e := s.templateRepo.FindScoped(req.TemplateID, a)
	if e != nil {
		return nil, e
	}
	if tmpl == nil || !tmpl.IsActive {
		return nil, ErrReportTemplateNotFound
	}
	tenant := model.TenantIDForTask(task)
	var saved model.ReportGeneration
	e = database.GetDB().WithContext(ctx).Where("tenant_id=? AND client_request_id=?", tenant, req.ClientRequestID).First(&saved).Error
	if e == nil {
		if saved.TaskUUID != task.UUID || saved.AttemptID != task.ExecutionAttemptID || saved.TemplateID != tmpl.ID {
			return nil, ErrResourceConflict
		}
		if saved.State == "ready" {
			return s.openGeneratedReport(ctx, &saved)
		}
		if saved.State == "generating" || saved.State == "unknown" {
			return nil, errors.New("report request is still running or its remote outcome is unknown; check generation records")
		}
		if saved.ServiceRevision != tmpl.Revision {
			return nil, ErrResourceConflict
		}
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	if saved.ID == "" {
		raw, hash, e := s.prepareReportSnapshot(ctx, task)
		if e != nil {
			return nil, e
		}
		saved = model.ReportGeneration{ID: uuid.NewString(), TenantID: tenant, TaskUUID: task.UUID, AttemptID: task.ExecutionAttemptID, ClientRequestID: req.ClientRequestID, TemplateID: tmpl.ID, ServiceRevision: tmpl.Revision, ContractVersion: tmpl.ContractVersion, SnapshotSHA256: hash, SnapshotJSON: raw, State: "pending", CreatedBy: a.UserID}
		if e = database.GetDB().Create(&saved).Error; e != nil {
			return nil, ErrResourceConflict
		}
	}
	claim := database.GetDB().Model(&model.ReportGeneration{}).Where("id=? AND state IN ?", saved.ID, []string{"pending", "failed"}).Updates(map[string]interface{}{"state": "generating", "error_code": ""})
	if claim.Error != nil {
		return nil, claim.Error
	}
	if claim.RowsAffected != 1 {
		return nil, ErrResourceConflict
	}
	req.GenerationID = saved.ID
	req.SnapshotJSON = saved.SnapshotJSON
	req.SnapshotSHA256 = saved.SnapshotSHA256
	download, e := s.generateReportDownload(ctx, tmpl, task, req, a.Email)
	if e != nil {
		state := "failed"
		if ctx.Err() != nil || strings.Contains(e.Error(), "report API request failed") {
			state = "unknown"
		}
		_ = database.GetDB().Model(&saved).Updates(map[string]interface{}{"state": state, "error_code": "REPORT_SERVICE_FAILED"}).Error
		return nil, e
	}
	defer download.Body.Close()
	content, e := io.ReadAll(io.LimitReader(download.Body, maxReportDownloadBytes+1))
	if e != nil || len(content) == 0 || len(content) > maxReportDownloadBytes {
		_ = database.GetDB().Model(&saved).Updates(map[string]interface{}{"state": "failed", "error_code": "REPORT_FILE_INVALID"}).Error
		return nil, errors.New("report response is empty, interrupted or exceeds size limit")
	}
	storage, e := newS3Storage(ctx, s.cfg.Storage)
	if e != nil {
		_ = database.GetDB().Model(&saved).Updates(map[string]interface{}{"state": "unknown", "error_code": "REPORT_STORAGE_FAILED"}).Error
		return nil, errors.New("generated report could not be saved")
	}
	key := path.Join(resultPackagePrefix(task), "_generated-reports", saved.ID, download.FileName)
	if e = storage.put(ctx, key, download.ContentType, content); e != nil {
		_ = database.GetDB().Model(&saved).Updates(map[string]interface{}{"state": "unknown", "error_code": "REPORT_STORAGE_FAILED"}).Error
		return nil, errors.New("generated report could not be saved")
	}
	if e = database.GetDB().Model(&saved).Updates(map[string]interface{}{"state": "ready", "file_name": download.FileName, "content_type": download.ContentType, "object_key": key, "error_code": ""}).Error; e != nil {
		return nil, e
	}
	return &ReportDownload{FileName: download.FileName, ContentType: download.ContentType, ContentLength: int64(len(content)), Body: io.NopCloser(bytes.NewReader(content))}, nil
}
func (s *ReportService) openGeneratedReport(ctx context.Context, g *model.ReportGeneration) (*ReportDownload, error) {
	store, e := newS3Storage(ctx, s.cfg.Storage)
	if e != nil {
		return nil, e
	}
	body, e := store.open(ctx, g.ObjectKey)
	if e != nil {
		return nil, errors.New("generated report object unavailable")
	}
	return &ReportDownload{FileName: g.FileName, ContentType: g.ContentType, ContentLength: -1, Body: body}, nil
}
func (s *ReportService) PublishTemplate(id string, a model.OverlayActor, expected uint64) (*model.ReportTemplateAdminResponse, error) {
	t, e := s.templateRepo.FindScoped(id, a)
	if e != nil {
		return nil, e
	}
	if t == nil || t.ExternalOrgID != "" || a.OrgID == "" || !a.ResourceMaintenance(t.OwnerUserID) {
		return nil, ErrReportTemplateNotFound
	}
	res := database.GetDB().Model(&model.ReportTemplate{}).Where("id=? AND revision=? AND external_org_id=''", id, expected).Updates(map[string]interface{}{"external_org_id": a.OrgID, "revision": gorm.Expr("revision+1")})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, ErrResourceConflict
	}
	t.ExternalOrgID = a.OrgID
	t.Revision++
	r := t.ToAdminResponse()
	r.CanMaintain = true
	return &r, nil
}
func (s *ReportService) ListGenerations(ctx context.Context, task *model.Task) ([]model.ReportGeneration, error) {
	rows := []model.ReportGeneration{}
	_ = expireReportGenerationLease(database.GetDB().WithContext(ctx))
	e := database.GetDB().WithContext(ctx).Where("tenant_id=? AND task_uuid=?", model.TenantIDForTask(task), task.UUID).Order("created_at DESC").Limit(100).Find(&rows).Error
	return rows, e
}

// Mark interrupted generation explicitly instead of silently reissuing an
// external operation whose outcome cannot be established.
func expireReportGenerationLease(tx *gorm.DB) error {
	return tx.Model(&model.ReportGeneration{}).Where("state='generating' AND updated_at<?", time.Now().Add(-10*time.Minute)).Updates(map[string]interface{}{"state": "unknown", "error_code": "REPORT_PROCESS_INTERRUPTED"}).Error
}
