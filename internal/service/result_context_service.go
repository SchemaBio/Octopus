package service

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
)

// GetContext returns one self-consistent result view for an authorized task.
// Every count and QC row is constrained to the task's active execution
// attempt, which prevents a retry or re-import from leaking stale evidence.
func (s *ResultService) GetContext(ctx context.Context, task *model.Task) (*model.ResultContextResponse, error) {
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	attemptID := task.ExecutionAttemptID
	if attemptID == "" {
		attemptID = task.UUID
	}
	tenantID := model.TenantIDForTask(task)
	qcs, err := s.repo.FindQCsByTaskScope(task.UUID, tenantID, attemptID)
	if err != nil {
		return nil, err
	}
	var counts map[string]model.ResultCount
	reference := resultReferenceForTask(s.cfg, task)
	parquet := model.ParquetResultState{Tables: []string{}}
	if task.Status != model.TaskStatusRunning && task.Status != model.TaskStatusQueued && task.Status != model.TaskStatusWaitingData {
		parquet = s.parquetArchiveState(ctx, task)
	}
	if parquet.Available {
		// Once Parquet is available it is the only source of result counts. Do
		// not leak the previous relational-import snapshot into this workspace.
		counts = make(map[string]model.ResultCount, len(parquet.Tables))
		for _, table := range parquet.Tables {
			counts[table] = model.ResultCount{}
		}
		var datasets []model.ResultDataset
		if err := database.DB.WithContext(ctx).Where("tenant_id = ? AND task_uuid = ? AND execution_attempt_id = ?", tenantID, task.UUID, attemptID).Find(&datasets).Error; err != nil {
			return nil, err
		}
		for _, dataset := range datasets {
			parquet.PreparedTables = append(parquet.PreparedTables, dataset.Table)
			if dataset.Table == "snv-indel" && dataset.AutomaticAssessmentReady {
				parquet.AutomaticAssessmentProfile = dataset.AutomaticAssessmentProfile
			}
			count := counts[dataset.Table]
			if dataset.Rows > 0 {
				count.Total = dataset.Rows
			}
			if err := database.DB.WithContext(ctx).Model(&model.ResultRowAdjustment{}).
				Where("tenant_id = ? AND task_uuid = ? AND execution_attempt_id = ? AND \"table\" = ? AND dataset_version = ? AND payload_json->>'reviewed' = 'true'", tenantID, task.UUID, attemptID, dataset.Table, dataset.DataVersion).Count(&count.Reviewed).Error; err != nil {
				return nil, err
			}
			if err := database.DB.WithContext(ctx).Model(&model.ResultRowAdjustment{}).
				Where("tenant_id = ? AND task_uuid = ? AND execution_attempt_id = ? AND \"table\" = ? AND dataset_version = ? AND payload_json->>'reported' = 'true'", tenantID, task.UUID, attemptID, dataset.Table, dataset.DataVersion).Count(&count.Reported).Error; err != nil {
				return nil, err
			}
			counts[dataset.Table] = count
		}
		sort.Strings(parquet.PreparedTables)
	} else {
		counts, err = s.repo.CountWorkspaceResults(task.UUID, tenantID, attemptID)
		if err != nil {
			return nil, err
		}
	}
	qcs = s.enrichArchivedQC(task, qcs)
	members, qc := resultMembersAndQC(qcs)
	if err := s.addQCGenderComparison(ctx, task, qc); err != nil {
		return nil, err
	}
	importBatchID := uint(0)
	if len(qcs) > 0 {
		importBatchID = qcs[0].ImportBatchID
	}
	if importBatchID == 0 && s.importBatchRepo != nil {
		batches, batchErr := s.importBatchRepo.FindLatestByTaskUUID(task.UUID, 20)
		if batchErr != nil {
			return nil, batchErr
		}
		for _, batch := range batches {
			if batch.ExecutionAttemptID == attemptID {
				importBatchID = batch.ID
				break
			}
		}
	}
	if parquet.PreparedTables == nil {
		parquet.PreparedTables = []string{}
	}
	state := resultWorkspaceState(task)
	version := resultWorkspaceVersion(task, attemptID)
	if parquet.Available {
		state = "ready"
		version = attemptID + ":parquet:" + parquet.ManifestVersion
		if parquet.AutomaticAssessmentProfile != "" {
			version += ":" + parquet.AutomaticAssessmentProfile
		}
	}
	return &model.ResultContextResponse{
		TaskUUID:           task.UUID,
		ExecutionAttemptID: attemptID,
		ImportBatchID:      importBatchID,
		ImportStatus:       task.ResultImportStatus,
		State:              state,
		Version:            version,
		Reference:          reference,
		Members:            members,
		Types:              counts,
		QC:                 qc,
		Permissions:        model.ResultPermissions{CanReview: true, CanReport: true},
		Parquet:            parquet,
	}, nil
}

func (s *ResultService) parquetArchiveState(ctx context.Context, task *model.Task) model.ParquetResultState {
	state := model.ParquetResultState{Tables: []string{}, FieldProfileVersion: "parquet-fields-v2"}
	if s.cfg == nil || !supportsParquetObjectStorage(s.cfg.Storage.Provider) {
		state.Reason = "结果对象存储未配置"
		return state
	}
	resolvedTask := *task
	resolvedTask.ExecutionAttemptID = executionAttempt(task)
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		state.Reason = "结果对象存储暂不可用"
		return state
	}
	prefix := resultPackagePrefix(&resolvedTask)
	objects, err := storage.list(ctx, prefix+"/")
	if err != nil {
		state.Reason = "无法读取当前执行的归档清单"
		return state
	}
	manifest, version, err := readParquetResultManifest(ctx, storage, prefix, objects)
	if err != nil {
		state.Reason = "当前执行的 Parquet 清单不可读"
		return state
	}
	byKey := make(map[string]s3ObjectInfo, len(objects))
	for _, object := range objects {
		byKey[object.Key] = object
	}
	tableSet := map[string]bool{}
	for _, ref := range manifestParquetRefs(manifest) {
		key, err := archiveParquetRefKey(storage.bucket, prefix, ref)
		if err != nil {
			continue
		}
		object, ok := byKey[key]
		if !ok || object.Size <= 0 {
			continue
		}
		for _, table := range []string{"snv-indel", "cnv-segment", "cnv-exon", "str", "mei", "mt", "upd", "roh"} {
			if parquetTableMatch(table, path.Base(key)) {
				tableSet[table] = true
			}
		}
	}
	for table := range tableSet {
		state.Tables = append(state.Tables, table)
	}
	sort.Strings(state.Tables)
	state.Available = len(state.Tables) > 0
	state.ManifestVersion = version
	if !state.Available {
		state.Reason = "归档中未找到已声明的结果 Parquet 文件"
	}
	return state
}

func resultWorkspaceState(task *model.Task) string {
	switch task.ResultImportStatus {
	case model.ResultImportStatusSuccess:
		return "ready"
	case model.ResultImportStatusRunning:
		return "importing"
	case model.ResultImportStatusFailed:
		return "import_failed"
	case model.ResultImportStatusPending:
		if task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusWaitingData {
			return "workflow_running"
		}
		return "not_imported"
	default:
		return "not_imported"
	}
}

func resultWorkspaceVersion(task *model.Task, attemptID string) string {
	if task.ResultImportFingerprint != "" {
		return attemptID + ":" + task.ResultImportFingerprint
	}
	if task.ResultImportedAt != nil {
		return fmt.Sprintf("%s:%d", attemptID, task.ResultImportedAt.UTC().UnixNano())
	}
	return attemptID + ":pending"
}

func resultReferenceForTask(cfg *config.Config, task *model.Task) model.ResultReference {
	if cfg == nil {
		return model.ResultReference{Reason: "IGV 参考资源配置不可用"}
	}
	declared := declaredReferenceID(task.InputJSON)
	if declared == "" {
		return model.ResultReference{Reason: "该执行的输入快照未提供参考序列身份"}
	}
	var reference config.IGVReferenceConfig
	switch declared {
	case "hg19":
		reference = cfg.IGV.HG19
	case "hg38":
		reference = cfg.IGV.HG38
	default:
		return model.ResultReference{DeclaredID: declared, Reason: "该参考序列未配置为可判读的 IGV 参考资源"}
	}
	if cfg.IGV.ReferenceProxyBaseURL == "" && (strings.TrimSpace(reference.FASTAURL) == "" || strings.TrimSpace(reference.FAIURL) == "") {
		return model.ResultReference{DeclaredID: declared, Reason: "该执行的参考 FASTA 与 FAI 尚未配置"}
	}
	return model.ResultReference{DeclaredID: declared, Available: true}
}

func declaredReferenceID(raw string) string {
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return ""
	}
	return normalizeReferenceID(findReferenceString(value))
}

func findReferenceString(value interface{}) string {
	switch node := value.(type) {
	case map[string]interface{}:
		for _, key := range []string{"reference_genome", "referenceGenome", "assembly", "genome", "fasta"} {
			if text, ok := node[key].(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
		for _, child := range node {
			if found := findReferenceString(child); found != "" {
				return found
			}
		}
	case []interface{}:
		for _, child := range node {
			if found := findReferenceString(child); found != "" {
				return found
			}
		}
	}
	return ""
}

func normalizeReferenceID(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(normalized, "hg19"), strings.Contains(normalized, "grch37"), strings.Contains(normalized, "hs37"):
		return "hg19"
	case strings.Contains(normalized, "hg38"), strings.Contains(normalized, "grch38"):
		return "hg38"
	default:
		return ""
	}
}

func resultMembersAndQC(rows []model.QCResult) ([]model.ResultMember, []model.QCMemberSummary) {
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := memberRoleOrder(rows[i].MemberRole), memberRoleOrder(rows[j].MemberRole)
		if left != right {
			return left < right
		}
		return rows[i].MemberID < rows[j].MemberID
	})
	members := make([]model.ResultMember, 0, len(rows))
	qc := make([]model.QCMemberSummary, 0, len(rows))
	for _, row := range rows {
		memberID := row.MemberID
		if memberID == "" {
			memberID = "legacy:unknown"
		}
		role := row.MemberRole
		if role == "" {
			role = "unknown"
		}
		members = append(members, model.ResultMember{ID: memberID, Role: role, SampleID: row.SampleID})
		qc = append(qc, model.QCMemberSummary{
			MemberID: memberID, MemberRole: role, SampleID: row.SampleID, Metrics: qcMetrics(row),
			PredictedGender: normalizeQCGender(row.PredictedGender),
		})
	}
	return members, qc
}

func memberRoleOrder(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "proband", "patient", "single":
		return 0
	case "father", "paternal":
		return 1
	case "mother", "maternal":
		return 2
	default:
		return 3
	}
}

func qcMetrics(row model.QCResult) []model.QCMetric {
	available := map[string]bool{}
	_ = json.Unmarshal([]byte(row.MetricAvailability), &available)
	metric := func(key string, value float64, unit, source string) model.QCMetric {
		var output *float64
		if available[key] {
			copy := value
			output = &copy
		}
		return model.QCMetric{Key: key, Value: output, Unit: unit, Source: source}
	}
	// xamdst stores percentage points (0–100); fastp and Picard store fractions.
	// Preserve archived values and describe their actual unit at the API boundary.
	return []model.QCMetric{
		metric("beforeTotalReads", float64(row.BeforeTotalReads), "reads", "fastp.before_filtering"),
		metric("totalReads", float64(row.TotalReads), "reads", "fastp.after_filtering"),
		metric("mappedReads", float64(row.MappedReads), "reads", "xamdst"),
		metric("mappedReadsFraction", row.MappedReadsFraction, "percent", "xamdst"),
		metric("averageDepth", row.AverageDepth, "×", "xamdst"),
		metric("dedupDepth", row.DedupDepth, "×", "xamdst"),
		metric("coverageGt02Avg", row.CoverageGt02Avg, "percent", "xamdst"),
		metric("coverageGte30x", row.CoverageGte30x, "percent", "xamdst"),
		metric("meanTargetCoverage", row.MeanTargetCoverage, "×", "hs_metrics"),
		metric("pctTargetBases30x", row.PctTargetBases30x, "fraction", "hs_metrics"),
		metric("duplicateRate", row.DuplicateRate, "fraction", "sambamba"),
		metric("q30Rate", row.Q30Rate, "fraction", "fastp.after_filtering"),
		metric("gcContent", row.GcContent, "fraction", "fastp.after_filtering"),
		metric("insertSizeMedian", float64(row.InsertSizeMedian), "bp", "xamdst"),
		metric("targetDataFraction", row.TargetDataFraction, "percent", "xamdst"),
		metric("sryCount", float64(row.SryCount), "reads", "SRY"),
		metric("mtAverageDepth", row.MtAverageDepth, "×", "mt_xamdst"),
		metric("mtCoverageGt0x", row.MtCoverageGt0x, "percent", "mt_xamdst"),
	}
}
