package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SchemaBio/Octopus/internal/config"
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
	counts, err := s.repo.CountWorkspaceResults(task.UUID, tenantID, attemptID)
	if err != nil {
		return nil, err
	}
	reference := resultReferenceForTask(s.cfg, task)
	members, qc := resultMembersAndQC(qcs)
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
	return &model.ResultContextResponse{
		TaskUUID:           task.UUID,
		ExecutionAttemptID: attemptID,
		ImportBatchID:      importBatchID,
		ImportStatus:       task.ResultImportStatus,
		State:              resultWorkspaceState(task),
		Version:            resultWorkspaceVersion(task, attemptID),
		Reference:          reference,
		Members:            members,
		Types:              counts,
		QC:                 qc,
		Permissions:        model.ResultPermissions{CanReview: true, CanReport: true},
	}, nil
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
	if strings.TrimSpace(reference.FASTAURL) == "" || strings.TrimSpace(reference.FAIURL) == "" {
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
	return []model.QCMetric{
		metric("totalReads", float64(row.TotalReads), "reads", "fastp.after_filtering"),
		metric("mappedReads", float64(row.MappedReads), "reads", "xamdst"),
		metric("mappedReadsFraction", row.MappedReadsFraction, "fraction", "xamdst"),
		metric("averageDepth", row.AverageDepth, "×", "xamdst"),
		metric("dedupDepth", row.DedupDepth, "×", "xamdst"),
		metric("coverageGte30x", row.CoverageGte30x, "fraction", "xamdst"),
		metric("meanTargetCoverage", row.MeanTargetCoverage, "×", "hs_metrics"),
		metric("pctTargetBases30x", row.PctTargetBases30x, "fraction", "hs_metrics"),
		metric("duplicateRate", row.DuplicateRate, "fraction", "sambamba"),
		metric("q30Rate", row.Q30Rate, "fraction", "fastp.after_filtering"),
		metric("gcContent", row.GcContent, "fraction", "fastp.after_filtering"),
		metric("insertSizeMedian", float64(row.InsertSizeMedian), "bp", "xamdst"),
		metric("targetDataFraction", row.TargetDataFraction, "fraction", "xamdst"),
		metric("mtAverageDepth", row.MtAverageDepth, "×", "mt_xamdst"),
		metric("mtCoverageGt0x", row.MtCoverageGt0x, "fraction", "mt_xamdst"),
	}
}
