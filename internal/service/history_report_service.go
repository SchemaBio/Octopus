package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

func historyReportID(d *model.ResultDataset, rowID string) string {
	h := sha256.Sum256([]byte(d.TenantID + "/" + d.ID + "/" + d.DataVersion + "/" + rowID))
	return hex.EncodeToString(h[:])
}

// Only these small, provenance-bearing fields are copied from immutable data.
var historyFieldKeys = []string{"chromosome", "position", "startPosition", "endPosition", "ref", "alt", "gene", "transcript", "hgvsc", "hgvsp", "variantType", "type", "consequence", "gnomadAF", "clinvarSignificance", "repeatUnit", "repeatDisplay", "status", "teType", "mtGene", "mtHgvs", "copyNumber", "exonCount", "iscn", "member", "role", "memberId", "memberRole", "sampleId"}

func compactHistoryFields(raw map[string]interface{}, table string) map[string]interface{} {
	row := normalizeParquetAPIItem(table, raw)
	result := map[string]interface{}{}
	for _, key := range historyFieldKeys {
		if value, ok := row[key]; ok && value != nil {
			if text := fmt.Sprint(value); text != "" && text != "." && text != "<nil>" {
				if len(text) > 2000 {
					switch key {
					case "chromosome", "position", "startPosition", "endPosition", "ref", "alt", "type", "repeatUnit", "teType", "gene", "transcript":
						result["_identityIncomplete"] = "true"
					}
					text = text[:2000]
				}
				result[key] = text
			}
		}
	}
	return result
}

func historyField(fields map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := fields[key]; ok && value != nil {
			v := strings.TrimSpace(fmt.Sprint(value))
			if v != "" && v != "." && v != "<nil>" {
				return v
			}
		}
	}
	return ""
}

func historyGroupIdentity(table, reference, sourceID string, fields map[string]interface{}) (string, bool) {
	chrom := strings.TrimPrefix(strings.ToLower(historyField(fields, "chromosome")), "chr")
	if chrom == "m" {
		chrom = "mt"
	}
	parts := []string{table, reference, chrom}
	known := reference != "" && chrom != "" && historyField(fields, "_identityIncomplete") != "true"
	position := historyField(fields, "position")
	validCoordinate := func(v string) bool { n, e := strconv.ParseInt(v, 10, 64); return e == nil && n >= 0 }
	switch table {
	case "snv-indel", "mt":
		ref, alt := strings.ToUpper(historyField(fields, "ref")), strings.ToUpper(historyField(fields, "alt"))
		point, _ := strconv.ParseInt(position, 10, 64)
		known = known && validCoordinate(position) && point > 0 && ref != "" && alt != ""
		parts = append(parts, position, ref, alt)
	case "str", "mei":
		kind := historyField(fields, "repeatUnit")
		if table == "mei" {
			kind = historyField(fields, "teType")
		}
		known = known && validCoordinate(position) && kind != ""
		parts = append(parts, position, kind)
	default:
		start, end := historyField(fields, "startPosition"), historyField(fields, "endPosition")
		known = known && validCoordinate(start) && validCoordinate(end)
		a, _ := strconv.ParseInt(start, 10, 64)
		b, _ := strconv.ParseInt(end, 10, 64)
		known = known && b >= a
		parts = append(parts, start, end)
		if table != "roh" {
			kind := strings.ToLower(historyField(fields, "type"))
			switch kind {
			case "gain", "duplication", "dup", "amplification":
				kind = "dup"
			case "loss", "deletion", "del":
				kind = "del"
			}
			known = known && kind != ""
			parts = append(parts, kind)
		}
		if table == "cnv-exon" {
			gene, transcript := historyField(fields, "gene"), historyField(fields, "transcript")
			known = known && gene != "" && transcript != ""
			parts = append(parts, gene, transcript)
		}
	}
	if !known {
		parts = []string{"unresolved", sourceID}
	}
	encoded, _ := json.Marshal(parts)
	h := sha256.Sum256(encoded)
	return hex.EncodeToString(h[:]), known
}

// An assembly label alone does not identify the analyzed FASTA. Keep such
// records separate until an input snapshot identifies the sequence resource.
func historyReferenceIdentity(raw string) (string, string) {
	declared := declaredReferenceID(raw)
	var input interface{}
	if json.Unmarshal([]byte(raw), &input) != nil {
		return declared, ""
	}
	resources := []string{}
	var walk func(interface{})
	walk = func(value interface{}) {
		switch node := value.(type) {
		case map[string]interface{}:
			for key, value := range node {
				lower := strings.ToLower(key)
				if text, ok := value.(string); ok && (strings.Contains(lower, "fasta") || strings.Contains(lower, "reference_sha") || strings.Contains(lower, "reference_md5") || strings.Contains(lower, "reference_sequence_id")) {
					text = strings.TrimSpace(text)
					if parsed, err := url.Parse(text); err == nil && parsed.Host != "" {
						parsed.RawQuery = ""
						parsed.Fragment = ""
						parsed.User = nil
						text = parsed.String()
					}
					if text != "" {
						resources = append(resources, text)
					}
				} else {
					walk(value)
				}
			}
		case []interface{}:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(input)
	if declared == "" || len(resources) == 0 {
		return declared, ""
	}
	sort.Strings(resources)
	unique := resources[:0]
	for _, resource := range resources {
		if len(unique) == 0 || unique[len(unique)-1] != resource {
			unique = append(unique, resource)
		}
	}
	encoded, _ := json.Marshal(append([]string{declared}, unique...))
	hash := sha256.Sum256(encoded)
	return declared, hex.EncodeToString(hash[:])
}

// Read one source row once at initial reporting. No browser-provided annotation
// can become an authoritative historical record.
func (s *ResultService) historySource(ctx context.Context, d *model.ResultDataset, rowID string, ordinal *int64) (*model.HistoryReport, error) {
	if ordinal == nil {
		return nil, fmt.Errorf("reporting requires the immutable source row ordinal")
	}
	if !browserOrdinalMatches(d, rowID, *ordinal) {
		return nil, ErrAdjustmentConflict
	}
	cache, err := s.historyDatasetCache(ctx, d)
	if err != nil {
		return nil, err
	}
	page, err := NewParquetReader().ReadPage(cache, *ordinal, 1)
	if err != nil {
		return nil, fmt.Errorf("history source read failed")
	}
	if page.TotalRows != d.Rows || len(page.Rows) != 1 {
		return nil, ErrParquetIncomplete
	}
	fields := compactHistoryFields(page.Rows[0], d.Table)
	encoded, _ := json.Marshal(fields)
	return &model.HistoryReport{ID: historyReportID(d, rowID), TenantID: d.TenantID, Table: d.Table, TaskUUID: d.TaskUUID, AttemptID: d.ExecutionAttemptID, DatasetID: d.ID, DatasetVersion: d.DataVersion, RowID: rowID, RowOrdinal: *ordinal, FieldsJSON: string(encoded)}, nil
}

func (s *ResultService) historyDatasetCache(ctx context.Context, d *model.ResultDataset) (string, error) {
	cache := filepath.Join(s.cfg.ResultQuery.CacheDir, currentCacheName(d.ID, d.ObjectSHA256))
	if stat, err := os.Stat(cache); err == nil && stat.Size() == d.SourceSize {
		return cache, nil
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return "", err
	}
	// The dataset catalogue, not a client path, selects the authorized object.
	source, err := storage.open(ctx, d.ObjectKey)
	if err != nil {
		return "", fmt.Errorf("history source object unavailable")
	}
	defer source.Close()
	if err = os.MkdirAll(s.cfg.ResultQuery.CacheDir, 0750); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(s.cfg.ResultQuery.CacheDir, ".history-source-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(source, maxCachedParquetBytes+1))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || n != d.SourceSize || hex.EncodeToString(h.Sum(nil)) != d.ObjectSHA256 {
		return "", fmt.Errorf("history source checksum mismatch")
	}
	if err = os.Chmod(f.Name(), 0640); err != nil {
		return "", err
	}
	if err = os.Rename(f.Name(), cache); err != nil {
		return "", err
	}
	return cache, nil
}

func historyEffectiveClassification(tx *gorm.DB, d *model.ResultDataset, rowID string, payload map[string]interface{}) (string, error) {
	if override := stringAdjustment(payload, "acmgOverride"); override != "" {
		return override, nil
	}
	if _, has := payload["acmgEvidence"]; has {
		return stringAdjustment(payload, "acmgClassification"), nil
	}
	if cnv, ok := payload["cnvAssessment"].(map[string]interface{}); ok {
		return historyField(cnv, "classification", "effectiveClassification"), nil
	}
	if d.Table != "snv-indel" {
		return "", nil
	}
	var auto model.ResultRowAutomaticAssessment
	err := tx.Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND row_id=? AND profile_version=?", d.TenantID, d.TaskUUID, d.ExecutionAttemptID, d.Table, rowID, d.AutomaticAssessmentProfile).First(&auto).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var values map[string]interface{}
	if err = json.Unmarshal([]byte(auto.AssessmentJSON), &values); err != nil {
		return "", err
	}
	return historyField(values, "classification"), nil
}

// Invoked inside the adjustment's transaction, after its append-only event.
func persistHistoryReport(tx *gorm.DB, task *model.Task, d *model.ResultDataset, saved *model.ResultRowAdjustment, event *model.ResultRowAdjustmentEvent, source *model.HistoryReport) error {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(saved.PayloadJSON), &payload); err != nil {
		return err
	}
	id := historyReportID(d, saved.RowID)
	var row model.HistoryReport
	err := tx.Where("id=?", id).First(&row).Error
	newReport := errors.Is(err, gorm.ErrRecordNotFound)
	if newReport {
		if payload["reported"] != true {
			return nil
		}
		if source == nil {
			return fmt.Errorf("verified history source required")
		}
		row = *source
		// Only the current attempt's input snapshot is evidence of its reference.
		if executionAttempt(task) == d.ExecutionAttemptID {
			row.Reference, row.ReferenceIdentity = historyReferenceIdentity(task.InputJSON)
		}
		var fields map[string]interface{}
		_ = json.Unmarshal([]byte(row.FieldsJSON), &fields)
		row.GroupKey, row.IdentityKnown = historyGroupIdentity(row.Table, row.ReferenceIdentity, row.ID, fields)
	} else if err != nil {
		return err
	}
	if row.Deleted {
		return ErrAdjustmentConflict
	}
	previousClassification := row.Classification
	row.Classification, err = historyEffectiveClassification(tx, d, saved.RowID, payload)
	if err != nil {
		return err
	}
	var before map[string]interface{}
	_ = json.Unmarshal([]byte(event.BeforeJSON), &before)
	wasReported := row.Reported
	row.Reported = payload["reported"] == true
	if row.Revision > 0 && row.Reported == wasReported && row.Classification == previousClassification {
		return tx.Model(&row).Update("adjustment_version", saved.Version).Error
	}
	if row.Reported && !wasReported {
		now := event.CreatedAt
		row.LastReportedAt = &now
		if row.FirstReportedAt == nil {
			row.FirstReportedAt = &now
		}
		row.ReportedClassification = row.Classification
		row.ReportedBy = event.Actor
	}
	row.AdjustmentVersion = saved.Version
	row.UpdatedAt = event.CreatedAt
	row.Revision, err = model.NextHistoryRevision(tx, row.TenantID)
	if err != nil {
		return err
	}
	if newReport {
		return tx.Create(&row).Error
	}
	return tx.Save(&row).Error
}

// Cursor uses a decimal revision plus a stable id. '~' finishes a watermark;
// row ids are lowercase hex, so it sorts after every id at that revision.
func parseHistoryCursor(cursor string) (uint64, string, error) {
	if cursor == "" {
		return 0, "", nil
	}
	parts := strings.Split(cursor, ":")
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid history cursor")
	}
	n, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || parts[1] != "~" && !validResultRowID(parts[1]) {
		return 0, "", fmt.Errorf("invalid history cursor")
	}
	return n, parts[1], nil
}

type HistoryTaskSummary struct {
	UUID           string `json:"uuid"`
	Name           string `json:"name"`
	SampleID       string `json:"sampleId"`
	InternalID     string `json:"internalId"`
	CurrentAttempt string `json:"currentAttempt"`
	Pipeline       string `json:"pipeline"`
}
type HistorySyncResponse struct {
	Scope     string                        `json:"scope"`
	Watermark uint64                        `json:"watermark"`
	Cursor    string                        `json:"cursor"`
	Complete  bool                          `json:"complete"`
	Rows      []model.HistoryReport         `json:"rows"`
	Tasks     map[string]HistoryTaskSummary `json:"tasks"`
}

func SyncHistoryReports(ctx context.Context, tenant, table, cursor string, watermark *uint64, limit int) (*HistorySyncResponse, error) {
	if tenant == "" || !validParquetTable(table) {
		return nil, fmt.Errorf("invalid history scope or type")
	}
	after, id, err := parseHistoryCursor(cursor)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	var clock model.HistoryScopeRevision
	err = database.DB.WithContext(ctx).Where("tenant_id=?", tenant).First(&clock).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	high := clock.Revision
	if watermark != nil {
		high = *watermark
	}
	if after > high || high > clock.Revision {
		return nil, ErrAdjustmentConflict
	}
	var rows []model.HistoryReport
	query := database.DB.WithContext(ctx).Where("tenant_id=? AND \"table\"=? AND revision<=?", tenant, table, high)
	if id == "~" {
		query = query.Where("revision>?", after)
	} else {
		query = query.Where("revision>? OR (revision=? AND id>?)", after, after, id)
	}
	err = query.Order("revision ASC,id ASC").Limit(limit + 1).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	complete := len(rows) <= limit
	if !complete {
		rows = rows[:limit]
	}
	tasks := map[string]HistoryTaskSummary{}
	ids := []string{}
	for _, row := range rows {
		if !row.Deleted {
			ids = append(ids, row.TaskUUID)
		}
	}
	if len(ids) > 0 {
		var found []model.Task
		if err = database.DB.WithContext(ctx).Where("uuid IN ?", ids).Find(&found).Error; err != nil {
			return nil, err
		}
		for _, task := range found {
			if model.TenantIDForTask(&task) == tenant {
				tasks[task.UUID] = HistoryTaskSummary{task.UUID, task.Name, task.SampleID, task.InternalID, executionAttempt(&task), task.Pipeline}
			}
		}
		for i := range rows {
			if !rows[i].Deleted {
				if _, ok := tasks[rows[i].TaskUUID]; !ok {
					rows[i].Deleted = true
				}
			}
		}
	}
	next := fmt.Sprintf("%d:~", high)
	if !complete {
		last := rows[len(rows)-1]
		next = fmt.Sprintf("%d:%s", last.Revision, last.ID)
	}
	return &HistorySyncResponse{tenant, high, next, complete, rows, tasks}, nil
}

func HistoryReportDetail(ctx context.Context, tenant, id string) (*model.HistoryReport, []model.ResultRowAdjustmentEvent, error) {
	var row model.HistoryReport
	if !validResultRowID(id) {
		return nil, nil, gorm.ErrRecordNotFound
	}
	if err := database.DB.WithContext(ctx).Where("id=? AND tenant_id=? AND deleted=false", id, tenant).First(&row).Error; err != nil {
		return nil, nil, err
	}
	var task model.Task
	if err := database.DB.WithContext(ctx).Where("uuid=?", row.TaskUUID).First(&task).Error; err != nil {
		return nil, nil, err
	}
	if model.TenantIDForTask(&task) != tenant {
		return nil, nil, gorm.ErrRecordNotFound
	}
	current := false
	if executionAttempt(&task) == row.AttemptID {
		var count int64
		if err := database.DB.WithContext(ctx).Model(&model.ResultDataset{}).Where("id=? AND tenant_id=? AND data_version=?", row.DatasetID, tenant, row.DatasetVersion).Count(&count).Error; err != nil {
			return nil, nil, err
		}
		current = count == 1
	}
	row.CurrentSource = &current
	var events []model.ResultRowAdjustmentEvent
	err := database.DB.WithContext(ctx).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND row_id=? AND dataset_version=?", tenant, row.TaskUUID, row.AttemptID, row.Table, row.RowID, row.DatasetVersion).Order("created_at DESC,id DESC").Limit(200).Find(&events).Error
	return &row, events, err
}
