package service

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var ErrParquetIncomplete = fmt.Errorf("PARQUET_SOURCE_ROW_COUNT_MISMATCH")

const maxCachedParquetBytes = int64(20 << 30)

type parquetQueryWireRequest struct {
	Table      string                `json:"table"`
	FilePath   string                `json:"filePath"`
	DatasetID  string                `json:"datasetId"`
	ObjectHash string                `json:"objectSha256"`
	RowID      string                `json:"rowId,omitempty"`
	RowCount   int64                 `json:"rowCount"`
	Offset     int64                 `json:"offset"`
	Limit      int64                 `json:"limit"`
	Search     string                `json:"search"`
	Sort       string                `json:"sort"`
	Direction  string                `json:"direction"`
	Filters    []model.ParquetFilter `json:"filters"`
	Overlays   []parquetOverlayWire  `json:"overlays"`
}

type parquetOverlayWire struct {
	RowID   string                 `json:"rowId"`
	Payload map[string]interface{} `json:"payload"`
	Version uint64                 `json:"version"`
}

type parquetAutomaticAssessmentRecord struct {
	RowID      string                 `json:"rowId"`
	Profile    string                 `json:"profileVersion"`
	Assessment map[string]interface{} `json:"assessment"`
}

type parquetPrepareResponse struct {
	AssessmentFile string `json:"assessmentFile"`
	Profile        string `json:"profile"`
	Rows           int64  `json:"rows"`
}

func (s *ResultService) QueryParquetTable(ctx context.Context, task *model.Task, table string, query model.ParquetQueryRequest) (*model.ParquetQueryResponse, error) {
	if task == nil || !validParquetTable(table) {
		return nil, fmt.Errorf("unsupported result table")
	}
	attempt := task.ExecutionAttemptID
	if attempt == "" {
		attempt = task.UUID
	}
	tenant := model.TenantIDForTask(task)
	dataset, err := s.ensureParquetDataset(ctx, task, tenant, attempt, table)
	if err != nil {
		return nil, err
	}
	var overlays []model.ResultRowAdjustment
	if err := database.DB.Where("tenant_id = ? AND task_uuid = ? AND execution_attempt_id = ? AND \"table\" = ?", tenant, task.UUID, attempt, table).Find(&overlays).Error; err != nil {
		return nil, err
	}
	wire := parquetQueryWireRequest{Table: table, FilePath: filepath.Join(s.cfg.ResultQuery.CacheDir, dataset.ID+"-"+dataset.ObjectSHA256+".parquet"), DatasetID: dataset.ID, ObjectHash: dataset.ObjectSHA256, RowCount: dataset.Rows, Offset: maxInt64(0, query.Offset), Limit: maxInt64(1, minInt64(200, query.Limit)), Search: query.Search, Sort: query.Sort, Direction: query.Direction, Filters: query.Filters, Overlays: make([]parquetOverlayWire, 0, len(overlays))}
	for _, overlay := range overlays {
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(overlay.PayloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("invalid row adjustment data")
		}
		wire.Overlays = append(wire.Overlays, parquetOverlayWire{RowID: overlay.RowID, Payload: payload, Version: overlay.Version})
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.ResultQuery.ServiceURL+"/v1/query", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 35 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Parquet query service unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Parquet query failed (status %d)", resp.StatusCode)
	}
	var result model.ParquetQueryResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid Parquet query response")
	}
	if dataset.ExpectedRows != nil && result.RowCount != *dataset.ExpectedRows {
		return nil, ErrParquetIncomplete
	}
	if table == "snv-indel" {
		if err := applyStoredAutomaticAssessments(ctx, task, dataset, &result); err != nil {
			return nil, err
		}
	}
	result.Version = dataset.DataVersion
	result.AttemptID = attempt
	dataset.Rows = result.RowCount
	fieldsJSON, _ := json.Marshal(result.Columns)
	dataset.FieldsJSON = string(fieldsJSON)
	if err := database.DB.WithContext(ctx).Model(&model.ResultDataset{}).
		Where("tenant_id = ? AND task_uuid = ? AND execution_attempt_id = ? AND \"table\" = ?", dataset.TenantID, dataset.TaskUUID, dataset.ExecutionAttemptID, table).
		Updates(map[string]interface{}{"rows": dataset.Rows, "fields_json": dataset.FieldsJSON}).Error; err != nil {
		return nil, fmt.Errorf("failed to store Parquet schema metadata")
	}
	for i := range result.Items {
		result.Items[i] = normalizeParquetAPIItem(table, result.Items[i])
		result.Items[i]["datasetVersion"] = dataset.DataVersion
		result.Items[i]["attemptId"] = attempt
	}
	return &result, nil
}

func (s *ResultService) ensureParquetDataset(ctx context.Context, task *model.Task, tenant, attempt, table string) (*model.ResultDataset, error) {
	if s.cfg == nil || !supportsParquetObjectStorage(s.cfg.Storage.Provider) {
		return nil, fmt.Errorf("object storage result archive is unavailable")
	}
	cacheDir := s.cfg.ResultQuery.CacheDir
	identity := sha256.Sum256([]byte(tenant + "/" + task.UUID + "/" + attempt + "/" + table))
	datasetID := hex.EncodeToString(identity[:])
	cachePath := filepath.Join(cacheDir, currentCacheName(datasetID, ""))
	var current model.ResultDataset
	lookupErr := database.DB.WithContext(ctx).Where("tenant_id = ? AND task_uuid = ? AND execution_attempt_id = ? AND \"table\" = ?", tenant, task.UUID, attempt, table).First(&current).Error
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return nil, lookupErr
	}
	cachePath = filepath.Join(cacheDir, currentCacheName(datasetID, current.ObjectSHA256))
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	resolvedTask := *task
	resolvedTask.ExecutionAttemptID = attempt
	prefix := resultPackagePrefix(&resolvedTask)
	objects, err := storage.list(ctx, prefix+"/")
	if err != nil {
		return nil, err
	}
	manifest, manifestVersion, err := readParquetResultManifest(ctx, storage, prefix, objects)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]s3ObjectInfo, len(objects))
	for _, object := range objects {
		byKey[object.Key] = object
	}
	var candidates []s3ObjectInfo
	for _, ref := range manifestParquetRefs(manifest) {
		key, err := archiveParquetRefKey(storage.bucket, prefix, ref)
		if err != nil {
			return nil, err
		}
		object, ok := byKey[key]
		if ok && parquetTableMatch(table, path.Base(key)) {
			candidates = append(candidates, object)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("this attempt has no manifest-declared Parquet table %s", table)
	}
	if len(candidates) != 1 {
		return nil, fmt.Errorf("Parquet table %s is ambiguous across multiple archive objects", table)
	}
	if lookupErr == nil && current.ManifestVersion == manifestVersion && current.ObjectKey == candidates[0].Key && current.SourceSize == candidates[0].Size && current.SourceLastModified.Equal(candidates[0].LastModified) && current.ObjectSHA256 != "" {
		if st, statErr := os.Stat(cachePath); statErr == nil && st.Size() == current.SourceSize {
			if current.ExpectedRows == nil {
				expected, err := s.archivedTextSourceRows(ctx, storage, prefix, manifest, byKey, current.ObjectKey, table)
				if err != nil {
					return nil, err
				}
				current.ExpectedRows = expected
				if expected != nil {
					if err := database.DB.WithContext(ctx).Model(&model.ResultDataset{}).Where("id=? AND data_version=?", current.ID, current.DataVersion).Update("expected_rows", expected).Error; err != nil {
						return nil, err
					}
				}
			}

			return &current, nil
		}
	}
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		return nil, err
	}
	if candidates[0].Size <= 0 || candidates[0].Size > maxCachedParquetBytes {
		return nil, fmt.Errorf("Parquet object size is invalid or exceeds the cache limit")
	}
	source, err := storage.open(ctx, candidates[0].Key)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(cacheDir, ".parquet-stage-*.tmp")
	if err != nil {
		source.Close()
		return nil, err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(source, maxCachedParquetBytes+1))
	source.Close()
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil || written != candidates[0].Size {
		os.Remove(tmp.Name())
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return nil, fmt.Errorf("downloaded Parquet size does not match the archive")
	}
	if err := os.Chmod(tmp.Name(), 0640); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	objectHash := hex.EncodeToString(hash.Sum(nil))
	cachePath = filepath.Join(cacheDir, currentCacheName(datasetID, objectHash))
	if err := os.Rename(tmp.Name(), cachePath); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	expectedRows, err := s.archivedTextSourceRows(ctx, storage, prefix, manifest, byKey, candidates[0].Key, table)
	if err != nil {
		return nil, err
	}
	fieldsJSON, _ := json.Marshal([]string{})
	dataVersion := objectHash
	item := model.ResultDataset{ID: datasetID, TenantID: tenant, TaskUUID: task.UUID, ExecutionAttemptID: attempt, Table: table, ObjectKey: candidates[0].Key, ObjectSHA256: objectHash, ManifestVersion: manifestVersion, SourceSize: candidates[0].Size, SourceLastModified: candidates[0].LastModified, FieldsJSON: string(fieldsJSON), ExpectedRows: expectedRows, Rows: 0, DataVersion: dataVersion, CreatedAt: time.Now().UTC()}
	if err := database.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "task_uuid"}, {Name: "execution_attempt_id"}, {Name: "table"}}, DoUpdates: clause.AssignmentColumns([]string{"object_key", "object_sha256", "manifest_version", "source_size", "source_last_modified", "fields_json", "expected_rows", "rows", "data_version", "automatic_assessment_profile", "automatic_assessment_ready", "created_at"})}).Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

const automaticACMGProfile = "acmg-snv-points-v2"

func (s *ResultService) ensureAutomaticAssessments(ctx context.Context, task *model.Task, tenant, attempt, table string, dataset *model.ResultDataset) error {
	if ctx.Value(browserLocalAssessmentKey{}) == true {
		return nil
	}
	if table != "snv-indel" || dataset.AutomaticAssessmentReady && dataset.AutomaticAssessmentProfile == automaticACMGProfile {
		return nil
	}
	requestBody, err := json.Marshal(map[string]string{
		"table":        table,
		"filePath":     filepath.Join(s.cfg.ResultQuery.CacheDir, dataset.ID+"-"+dataset.ObjectSHA256+".parquet"),
		"datasetId":    dataset.ID,
		"objectSha256": dataset.ObjectSHA256,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.ResultQuery.ServiceURL+"/v1/prepare", strings.NewReader(string(requestBody)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("automatic ACMG preparation service unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("automatic ACMG preparation failed (status %d)", resp.StatusCode)
	}
	var prepared parquetPrepareResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&prepared); err != nil || prepared.Profile != automaticACMGProfile {
		return fmt.Errorf("invalid automatic ACMG preparation response")
	}
	assessmentRoot, err := filepath.Abs(s.cfg.ResultQuery.AssessmentDir)
	if err != nil {
		return fmt.Errorf("invalid ACMG assessment directory")
	}
	assessmentPath, err := filepath.Abs(filepath.Clean(prepared.AssessmentFile))
	if err != nil {
		return fmt.Errorf("invalid automatic ACMG assessment path")
	}
	relative, err := filepath.Rel(assessmentRoot, assessmentPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("automatic ACMG assessment path is outside the shared cache")
	}
	expectedAssessmentPath := filepath.Join(assessmentRoot, dataset.ID+"-"+dataset.ObjectSHA256+"-"+automaticACMGProfile+".jsonl")
	if assessmentPath != expectedAssessmentPath {
		return fmt.Errorf("unexpected automatic ACMG assessment path")
	}
	info, err := os.Stat(assessmentPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 512<<20 {
		return fmt.Errorf("automatic ACMG assessment file is unavailable")
	}
	file, err := os.Open(assessmentPath)
	if err != nil {
		return fmt.Errorf("automatic ACMG assessment file is unavailable")
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	tenant = model.TenantIDForTask(task)
	err = database.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var current model.ResultDataset
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=?", tenant, task.UUID, attempt, table).First(&current).Error; err != nil {
			return err
		}
		if current.DataVersion != dataset.DataVersion {
			return ErrAdjustmentConflict
		}
		if current.AutomaticAssessmentReady && current.AutomaticAssessmentProfile == automaticACMGProfile {
			return nil
		}
		if err := tx.Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND profile_version=?", tenant, task.UUID, attempt, table, automaticACMGProfile).Delete(&model.ResultRowAutomaticAssessment{}).Error; err != nil {
			return err
		}
		batch := make([]model.ResultRowAutomaticAssessment, 0, 1000)
		count := int64(0)
		for scanner.Scan() {
			var record parquetAutomaticAssessmentRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil || record.Profile != automaticACMGProfile || !validResultRowID(record.RowID) || record.Assessment == nil {
				return fmt.Errorf("invalid automatic ACMG assessment row")
			}
			assessment, err := json.Marshal(record.Assessment)
			if err != nil {
				return err
			}
			batch = append(batch, model.ResultRowAutomaticAssessment{TenantID: tenant, TaskUUID: task.UUID, ExecutionAttemptID: attempt, Table: table, RowID: record.RowID, ProfileVersion: record.Profile, AssessmentJSON: string(assessment), CreatedAt: time.Now().UTC()})
			count++
			if len(batch) == cap(batch) {
				if err := tx.CreateInBatches(&batch, 1000).Error; err != nil {
					return err
				}
				batch = batch[:0]
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		if len(batch) > 0 {
			if err := tx.CreateInBatches(&batch, 1000).Error; err != nil {
				return err
			}
		}
		if count != prepared.Rows {
			return fmt.Errorf("automatic ACMG assessment row count is incomplete")
		}
		return tx.Model(&model.ResultDataset{}).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=?", tenant, task.UUID, attempt, table).
			Updates(map[string]interface{}{"automatic_assessment_profile": automaticACMGProfile, "automatic_assessment_ready": true}).Error
	})
	if err != nil {
		return fmt.Errorf("failed to persist automatic ACMG assessment")
	}
	dataset.AutomaticAssessmentProfile = automaticACMGProfile
	dataset.AutomaticAssessmentReady = true
	return nil
}

func applyStoredAutomaticAssessments(ctx context.Context, task *model.Task, dataset *model.ResultDataset, result *model.ParquetQueryResponse) error {
	if dataset == nil || !dataset.AutomaticAssessmentReady || dataset.AutomaticAssessmentProfile != automaticACMGProfile || len(result.Items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		if id, ok := item["__row_id"].(string); ok && validResultRowID(id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var assessments []model.ResultRowAutomaticAssessment
	if err := database.DB.WithContext(ctx).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=? AND profile_version=? AND row_id IN ?", dataset.TenantID, task.UUID, dataset.ExecutionAttemptID, "snv-indel", automaticACMGProfile, ids).Find(&assessments).Error; err != nil {
		return fmt.Errorf("failed to load automatic ACMG assessments")
	}
	byID := make(map[string]map[string]interface{}, len(assessments))
	for _, row := range assessments {
		var assessment map[string]interface{}
		if err := json.Unmarshal([]byte(row.AssessmentJSON), &assessment); err != nil {
			return fmt.Errorf("stored automatic ACMG assessment is invalid")
		}
		byID[row.RowID] = assessment
	}
	for _, item := range result.Items {
		if id, ok := item["__row_id"].(string); ok {
			if assessment, exists := byID[id]; exists {
				item["__acmg"] = assessment
			}
		}
	}
	return nil
}

func supportsParquetObjectStorage(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return provider == "s3" || provider == "cos"
}

func manifestParquetRefs(manifest map[string]interface{}) []string {
	seen := map[string]bool{}
	var walk func(interface{})
	walk = func(value interface{}) {
		switch node := value.(type) {
		case map[string]interface{}:
			for _, child := range node {
				walk(child)
			}
		case []interface{}:
			for _, child := range node {
				walk(child)
			}
		case string:
			clean := strings.TrimSpace(strings.ReplaceAll(node, "\\", "/"))
			if strings.HasSuffix(strings.ToLower(clean), ".parquet") {
				seen[clean] = true
			}
		}
	}
	walk(manifest)
	refs := make([]string, 0, len(seen))
	for key := range seen {
		refs = append(refs, key)
	}
	return refs
}

func validParquetTable(table string) bool {
	switch table {
	case "snv-indel", "cnv-segment", "cnv-exon", "str", "mei", "mt", "upd", "roh":
		return true
	}
	return false
}

func parquetTableMatch(table, filename string) bool {
	name := strings.ToLower(filename)
	name = strings.TrimSuffix(name, ".parquet")
	compact := strings.NewReplacer("_", "", "-", "", ".", "").Replace(name)
	switch table {
	case "snv-indel":
		return strings.Contains(compact, "snvindel")
	case "cnv-segment":
		return strings.Contains(compact, "cnvsegment") || strings.Contains(compact, "regioncnvanno")
	case "cnv-exon":
		return strings.Contains(compact, "cnvexon") || strings.Contains(compact, "genecnvanno")
	case "str":
		return strings.Contains(compact, "str")
	case "mei":
		return strings.Contains(compact, "mei")
	case "mt":
		return strings.Contains(compact, "mitochond") || strings.Contains(compact, "mtvariant") || strings.Contains(compact, "mtreport")
	case "upd":
		return strings.Contains(compact, "upd")
	case "roh":
		return strings.Contains(compact, "roh")
	}
	return false
}

func normalizeParquetAPIItem(table string, item map[string]interface{}) map[string]interface{} {
	row := make(map[string]interface{}, len(item)+8)
	for key, value := range item {
		if !strings.HasPrefix(key, "__") {
			row[parquetJSONFieldName(key)] = value
		}
	}
	commonAliases := map[string]string{
		"STR_Status": "status", "Repeat_Unit": "repeatUnit", "Ref_Repeats": "refRepeats", "Allele1_Repeats": "allele1Repeats",
		"Allele2_Repeats": "allele2Repeats", "Repeat_Display": "repeatDisplay", "Normal_Max": "normalRangeMax", "Pathologic_Min": "pathologicMin",
		"Spanning_Reads": "spanningReads", "Flanking_Reads": "flankingReads", "InRepeat_Reads": "inRepeatReads", "SweGen_Mean": "swegenMean", "SweGen_Std": "swegenStd",
		"MEI_ID": "meiId", "TE_Type": "teType", "TE_Family": "teFamily", "Direction": "direction", "Support_Reads": "supportingReads", "Avg_SoftClip_Length": "avgSoftClipLength",
		"MT_Gene": "mtGene", "MT_Gene_Type": "mtGeneType", "Mitophen_Variant": "mitophenVariant", "Mitophen_Phenotypes": "mitophenPhenotypes", "MT_HGVS": "mtHgvs",
		"Heteroplasmy_Class": "heteroplasmyClass", "Protein_Position": "proteinPosition", "Size(Mb)": "sizeMb", "Size_Mb_": "sizeMb", "Nb_variants": "nbVariants", "Percentage_homozygosity": "percentageHomozygosity", "Recessive_Genes": "recessiveGenes",
	}
	for source, target := range commonAliases {
		if value, exists := item[source]; exists {
			row[target] = value
		}
	}
	rowID, _ := item["__row_id"].(string)
	row["id"] = rowID
	row["rowOrdinal"] = item["__ordinal"]
	row["rowId"] = rowID
	row["adjustments"] = item["__adjustments"]
	row["adjustmentVersion"] = item["__adjustment_version"]
	if row["adjustments"] == nil {
		row["adjustments"] = map[string]interface{}{}
	}
	row["reviewed"] = false
	row["reported"] = false
	if adjustments, ok := row["adjustments"].(map[string]interface{}); ok {
		for key, value := range adjustments {
			row[key] = value
		}
	}
	if table == "snv-indel" {
		aliases := map[string]string{"Chromosome": "chromosome", "Position": "position", "Variant_ID": "variantId", "Ref": "ref", "Alt": "alt", "Type": "variantType", "Quality": "quality", "Filter": "filter", "Genotype": "genotype", "Zygosity": "zygosity", "PhaseSet": "phaseSet", "Depth": "depth", "AD": "ad", "VAF": "vaf", "Gene": "gene", "Transcript": "transcript", "Location": "location", "Consequence": "consequence", "Impact": "impact", "HGVS_c": "hgvsc", "HGVS_p": "hgvsp", "Amino_Acids": "aminoAcids", "Cytoband": "cytoband", "ClinVar_Sig": "clinvarSignificance", "ClinVar_RevStat": "clinvarRevStat", "ClinVar_DN": "clinvarDn", "ClinVar_Star": "clinvarStar", "GnomAD_AF": "gnomadAF", "GnomAD_AF_EAS": "gnomadEasAF", "GnomAD_nhomalt_XX": "gnomadNhomaltXX", "GnomAD_nhomalt_XY": "gnomadNhomaltXY", "Pangolin_Gain": "pangolinGain", "Pangolin_Loss": "pangolinLoss", "Pangolin_AN": "pangolinAN", "EVOScore": "evoScore", "EVOScore_AN": "evoScoreAN", "AlphaMissense_AM": "alphaMissenseAM", "AlphaMissense_AMC": "alphaMissenseAMC", "HGNC_ID": "hgncId", "dbSNP": "rsId", "MAX_AF": "maxAF", "GenCC_moi_curie": "genccMoi", "GenCC_disease_title": "genccDiseaseTitle", "GenCC_moi_title": "genccMoiTitle", "GenCC_disease_original_curie": "genccDiseaseOriginalCurie", "GenCC_assertion_criteria_url": "genccAssertionCriteriaUrl"}
		annotations := make(map[string]string)
		for source, target := range aliases {
			if value, exists := item[source]; exists {
				row[target] = value
				annotations[source] = fmt.Sprint(value)
			}
		}
		row["annotationValues"] = annotations
		if acmg, ok := item["__acmg"].(map[string]interface{}); ok {
			row["automaticAcmg"] = acmg
			row["acmgClassification"] = acmg["classification"]
			row["acmgAssessmentSource"] = "automatic"
		}
		if adjustment, ok := item["__adjustments"].(map[string]interface{}); ok {
			for key, value := range adjustment {
				row[key] = value
			}
			if classification, ok := adjustment["acmgClassification"]; ok {
				row["acmgClassification"] = classification
			}
			if override, ok := adjustment["acmgOverride"].(string); ok && override != "" {
				row["acmgAssessmentSource"] = "manual_override"
			} else if _, ok := adjustment["acmgEvidence"]; ok {
				row["acmgAssessmentSource"] = "manual_evidence"
			}
		}
		row["vaf"] = item["VAF"]
		row["alleleFrequency"] = item["VAF"]
		row["reviewStatus"] = map[string]interface{}{"reviewed": row["reviewed"], "reported": row["reported"]}
		if adjustment, ok := item["__adjustments"].(map[string]interface{}); ok {
			if computed, exists := adjustment["acmgClassificationComputed"]; exists {
				row["acmgClassificationComputed"] = computed
			} else if _, manual := adjustment["acmgEvidence"]; manual {
				row["acmgClassificationComputed"] = adjustment["acmgClassification"]
			}
		}
		if _, exists := row["acmgClassificationComputed"]; !exists {
			if auto, ok := item["__acmg"].(map[string]interface{}); ok {
				row["acmgClassificationComputed"] = auto["classification"]
			}
		}
	}
	if table == "cnv-segment" || table == "cnv-exon" {
		for source, target := range map[string]string{"Start": "startPosition", "End": "endPosition", "Dosage_Genes": "dosageGenes", "GenCC_AD_Genes": "genccADGenes", "Copy_Number": "copyNumber", "Copy_Ratio": "copyRatio", "Log2_Ratio": "log2Ratio", "ISCN": "iscn"} {
			if value, exists := item[source]; exists {
				row[target] = value
			}
		}
	}
	if _, exists := row["chromosome"]; !exists {
		if value, ok := item["Chr"]; ok {
			row["chromosome"] = value
		}
	}
	if _, exists := row["startPosition"]; !exists {
		for _, source := range []string{"Start", "Start_Position", "Begin"} {
			if value, ok := item[source]; ok {
				row["startPosition"] = value
				break
			}
		}
	}
	if _, exists := row["endPosition"]; !exists {
		for _, source := range []string{"End", "End_Position"} {
			if value, ok := item[source]; ok {
				row["endPosition"] = value
				break
			}
		}
	}
	if table == "cnv-exon" {
		for source, target := range map[string]string{"Col4": "type", "Col5": "gene", "Col6": "transcript", "Col7": "ensemblTranscript", "Col8": "exonCount", "Col9": "log2Ratio", "Col10": "log2Median", "Col11": "log2Std", "Col12": "copyNumber", "Col13": "depth", "Col14": "weight", "Col15": "binCount", "Col16": "segmentCount", "Col17": "coverageRatio", "Col18": "confidenceLabel", "Col19": "pValue"} {
			if value, ok := item[source]; ok {
				row[target] = value
			}
		}
	}
	if table == "cnv-segment" {
		for source, target := range map[string]string{"Col4": "type", "Col5": "log2Ratio", "Col6": "depth", "Col7": "weight", "Col8": "copyNumber", "Dosage_Genes": "dosageGenes", "GenCC_AD_Genes": "genccADGenes"} {
			if value, ok := item[source]; ok {
				row[target] = value
			}
		}
	}
	if _, exists := row["clinvarSignificance"]; !exists {
		if value, ok := item["ClinVar_Sig"]; ok {
			row["clinvarSignificance"] = value
		}
	}
	// A false manual value also overrides an automatically important variant.
	classification, _ := row["acmgClassification"].(string)
	row["pinned"] = table == "snv-indel" && (classification == "Pathogenic" || classification == "Likely_Pathogenic")
	if adjustment, ok := item["__adjustments"].(map[string]interface{}); ok {
		if pinned, exists := adjustment["pinned"].(bool); exists {
			row["pinned"] = pinned
			row["pinSource"] = "manual"
		}
	}
	if row["pinned"] == true && row["pinSource"] == nil {
		row["pinSource"] = "automatic"
	}
	row["reviewStatus"] = map[string]interface{}{"pinned": row["pinned"], "reviewed": row["reviewed"], "reported": row["reported"]}
	return row
}

func parquetJSONFieldName(name string) string {
	if name == "" {
		return ""
	}
	if !strings.Contains(name, "_") {
		return strings.ToLower(name[:1]) + name[1:]
	}
	parts := strings.Split(name, "_")
	for i, part := range parts {
		if i == 0 {
			parts[i] = strings.ToLower(part[:1]) + part[1:]
		} else if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func newResultRowID(datasetID, objectSHA256 string, ordinal int64) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", datasetID, objectSHA256, ordinal)))
	return hex.EncodeToString(hash[:])
}

func validResultRowID(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func newAdjustmentEventID() string { return uuid.NewString() }

func currentCacheName(id, hash string) string { return id + "-" + hash + ".parquet" }

// Resolve a stable row against the current immutable object; hashes alone are not membership proof.
func (s *ResultService) verifyParquetRow(ctx context.Context, task *model.Task, table, rowID string) (*model.ResultDataset, error) {
	dataset, err := s.ensureParquetDataset(ctx, task, model.TenantIDForTask(task), executionAttempt(task), table)
	if err != nil {
		return nil, err
	}
	wire := parquetQueryWireRequest{Table: table, FilePath: filepath.Join(s.cfg.ResultQuery.CacheDir, currentCacheName(dataset.ID, dataset.ObjectSHA256)), DatasetID: dataset.ID, ObjectHash: dataset.ObjectSHA256, RowID: rowID, Limit: 1}
	body, _ := json.Marshal(wire)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.ResultQuery.ServiceURL+"/v1/query", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 35 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("row identity verification unavailable")
	}
	defer resp.Body.Close()
	var result model.ParquetQueryResponse
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil {
		return nil, fmt.Errorf("row identity verification failed")
	}
	if dataset.ExpectedRows != nil && result.RowCount != *dataset.ExpectedRows {
		return nil, ErrParquetIncomplete
	}
	if result.Total != 1 {
		return nil, ErrAdjustmentConflict
	}
	return dataset, nil
}

// Export an effective table through the same structured query engine as the workspace.
func (s *ResultService) ExportParquetTable(ctx context.Context, task *model.Task, table string, query model.ParquetQueryRequest) (*http.Response, error) {
	if !validParquetTable(table) {
		return nil, fmt.Errorf("invalid result table")
	}
	if _, err := s.QueryParquetTable(ctx, task, table, model.ParquetQueryRequest{Limit: 1}); err != nil {
		return nil, err
	}
	dataset, err := s.ensureParquetDataset(ctx, task, model.TenantIDForTask(task), executionAttempt(task), table)
	if err != nil {
		return nil, err
	}
	if query.DatasetVersion != "" && dataset.DataVersion != query.DatasetVersion || query.AttemptID != "" && query.AttemptID != executionAttempt(task) {
		return nil, ErrAdjustmentConflict
	}
	var overlays []model.ResultRowAdjustment
	if err := database.DB.WithContext(ctx).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=?", model.TenantIDForTask(task), task.UUID, executionAttempt(task), table).Find(&overlays).Error; err != nil {
		return nil, err
	}
	wire := parquetQueryWireRequest{Table: table, FilePath: filepath.Join(s.cfg.ResultQuery.CacheDir, currentCacheName(dataset.ID, dataset.ObjectSHA256)), DatasetID: dataset.ID, ObjectHash: dataset.ObjectSHA256, Search: query.Search, Sort: query.Sort, Direction: query.Direction, Filters: query.Filters}
	for _, row := range overlays {
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(row.PayloadJSON), &payload); err != nil {
			return nil, err
		}
		wire.Overlays = append(wire.Overlays, parquetOverlayWire{RowID: row.RowID, Payload: payload, Version: row.Version})
	}
	body, _ := json.Marshal(wire)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.ResultQuery.ServiceURL+"/v1/export", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 35 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("effective result export unavailable")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("effective result export failed")
	}
	return resp, nil
}

func archiveParquetRefKey(bucket, prefix, ref string) (string, error) {
	key := strings.TrimSpace(ref)
	if strings.Contains(key, "://") {
		parsed, err := url.Parse(key)
		ownedHost := err == nil && ((parsed.Scheme == "cos" || parsed.Scheme == "s3") && parsed.Host == bucket || parsed.Scheme == "https" && regexp.MustCompile("^"+regexp.QuoteMeta(bucket)+`\.cos\.[a-z0-9-]+\.myqcloud\.com$`).MatchString(parsed.Host))
		if !ownedHost || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("invalid archive object reference")
		}
		key = strings.TrimPrefix(parsed.Path, "/")
	} else if !strings.HasPrefix(key, prefix+"/") {
		if strings.HasPrefix(key, "/") {
			return "", fmt.Errorf("absolute archive object reference")
		}
		key = prefix + "/" + key
	}
	if _, err := safeResultPackageRelativePath(prefix, key); err != nil {
		return "", err
	}
	return key, nil
}
