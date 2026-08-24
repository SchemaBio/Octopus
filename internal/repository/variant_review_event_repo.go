package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReviewMutation describes the result of an idempotent review transition.
type ReviewMutation struct {
	Reviewed   bool
	Changed    bool
	ReviewedBy string
	ReviewedAt *time.Time
	Event      *model.VariantReviewEvent
}

// ReviewVariantWithEvent updates the fast result projection and appends its
// audit event in one transaction. The task is already authorized by the
// handler; tenant_id is still checked at the row boundary for defense in depth.
func (r *ResultRepository) ReviewVariantWithEvent(task *model.Task, variantType, id string, reviewed bool, actorID uint, actorEmail string) (*ReviewMutation, error) {
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	m, ok := variantModel[variantType]
	if !ok {
		return nil, fmt.Errorf("unknown variant type: %s", variantType)
	}
	tx := r.db.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	row := newVariant(variantType)
	tenantID := model.TenantIDForTask(task)
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_id = ? AND tenant_id = ?", id, task.UUID, tenantID).First(row)
	if query.Error != nil {
		_ = tx.Rollback().Error
		if query.Error == gorm.ErrRecordNotFound {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, query.Error
	}
	current, currentBy, currentAt := reviewFields(row)
	mutation := &ReviewMutation{Reviewed: current, ReviewedBy: currentBy, ReviewedAt: currentAt}
	if current == reviewed {
		_ = tx.Rollback().Error
		return mutation, nil
	}

	now := time.Now().UTC()
	updates := map[string]interface{}{"reviewed": reviewed}
	if reviewed {
		updates["reviewed_by"] = actorEmail
		updates["reviewed_at"] = now
	} else {
		updates["reviewed_by"] = ""
		updates["reviewed_at"] = nil
	}
	if err := tx.Model(m).Where("id = ? AND task_id = ? AND tenant_id = ?", id, task.UUID, tenantID).Updates(updates).Error; err != nil {
		_ = tx.Rollback().Error
		return nil, err
	}
	setReviewFields(row, reviewed, actorEmail, func() *time.Time {
		if !reviewed {
			return nil
		}
		return &now
	}())

	identity := reviewIdentity(variantType, row, task)
	snapshot, err := json.Marshal(row)
	if err != nil {
		_ = tx.Rollback().Error
		return nil, fmt.Errorf("marshal review snapshot: %w", err)
	}
	event := &model.VariantReviewEvent{
		ID: uuid.New().String(), TenantID: tenantID, TaskUUID: task.UUID,
		ExecutionAttemptID: executionAttempt(task), ImportBatchID: resultImportBatchID(row),
		VariantType: variantType, VariantID: id, VariantFingerprint: identity.Fingerprint,
		HistoryGroupKey: identity.GroupKey, Action: model.VariantReviewActionRevoked,
		ActorUserID: actorID, ActorEmail: actorEmail, ReferenceGenome: identity.ReferenceGenome,
		TaskName: task.Name, Pipeline: task.Pipeline, PipelineVersion: task.PipelineVersion,
		SampleID: task.SampleID, InternalID: task.InternalID, OccurredAt: &now,
		TimestampKnown: true, RecordedAt: now, VariantSnapshotJSON: string(snapshot),
	}
	if reviewed {
		event.Action = model.VariantReviewActionReviewed
	}
	if err := tx.Create(event).Error; err != nil {
		_ = tx.Rollback().Error
		return nil, err
	}
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	mutation.Reviewed, mutation.Changed, mutation.ReviewedBy, mutation.ReviewedAt, mutation.Event = reviewed, true, actorEmail, func() *time.Time {
		if !reviewed {
			return nil
		}
		return &now
	}(), event
	return mutation, nil
}

func newVariant(variantType string) interface{} {
	switch variantType {
	case "snv-indel":
		return &model.SNVIndel{}
	case "cnv-segment":
		return &model.CNVSegment{}
	case "cnv-exon":
		return &model.CNVExon{}
	case "str":
		return &model.STR{}
	case "mei":
		return &model.MEIVariant{}
	case "mt":
		return &model.MitochondrialVariant{}
	case "upd":
		return &model.UPDRegion{}
	case "roh":
		return &model.ROHRegion{}
	default:
		return nil
	}
}

func reviewFields(row interface{}) (bool, string, *time.Time) {
	switch v := row.(type) {
	case *model.SNVIndel:
		return v.Reviewed, v.ReviewedBy, v.ReviewedAt
	case *model.CNVSegment:
		return v.Reviewed, v.ReviewedBy, v.ReviewedAt
	case *model.CNVExon:
		return v.Reviewed, v.ReviewedBy, v.ReviewedAt
	case *model.STR:
		return v.Reviewed, v.ReviewedBy, v.ReviewedAt
	case *model.MEIVariant:
		return v.Reviewed, v.ReviewedBy, v.ReviewedAt
	case *model.MitochondrialVariant:
		return v.Reviewed, v.ReviewedBy, v.ReviewedAt
	case *model.UPDRegion:
		return v.Reviewed, v.ReviewedBy, v.ReviewedAt
	case *model.ROHRegion:
		return v.Reviewed, v.ReviewedBy, v.ReviewedAt
	}
	return false, "", nil
}

func setReviewFields(row interface{}, reviewed bool, by string, at *time.Time) {
	switch v := row.(type) {
	case *model.SNVIndel:
		v.Reviewed, v.ReviewedBy, v.ReviewedAt = reviewed, by, at
	case *model.CNVSegment:
		v.Reviewed, v.ReviewedBy, v.ReviewedAt = reviewed, by, at
	case *model.CNVExon:
		v.Reviewed, v.ReviewedBy, v.ReviewedAt = reviewed, by, at
	case *model.STR:
		v.Reviewed, v.ReviewedBy, v.ReviewedAt = reviewed, by, at
	case *model.MEIVariant:
		v.Reviewed, v.ReviewedBy, v.ReviewedAt = reviewed, by, at
	case *model.MitochondrialVariant:
		v.Reviewed, v.ReviewedBy, v.ReviewedAt = reviewed, by, at
	case *model.UPDRegion:
		v.Reviewed, v.ReviewedBy, v.ReviewedAt = reviewed, by, at
	case *model.ROHRegion:
		v.Reviewed, v.ReviewedBy, v.ReviewedAt = reviewed, by, at
	}
}

type reviewIdentityValue struct{ Fingerprint, GroupKey, ReferenceGenome string }

func reviewIdentity(variantType string, row interface{}, task *model.Task) reviewIdentityValue {
	genome := referenceGenome(task)
	parts := []string{variantType, genome}
	group := []string{variantType, genome}
	switch v := row.(type) {
	case *model.SNVIndel:
		parts = append(parts, v.Chromosome, fmt.Sprint(v.Position), strings.ToUpper(v.Ref), strings.ToUpper(v.Alt))
		group = append(group, v.Gene, v.HGVSc, v.HGVSp)
	case *model.CNVSegment:
		parts = append(parts, v.Chromosome, fmt.Sprint(v.StartPosition), fmt.Sprint(v.EndPosition), v.Type)
		group = append(group, v.Chromosome, fmt.Sprint(v.StartPosition), fmt.Sprint(v.EndPosition), v.Type)
	case *model.CNVExon:
		parts = append(parts, v.Chromosome, fmt.Sprint(v.StartPosition), fmt.Sprint(v.EndPosition), v.Gene, v.Transcript, v.Type)
		group = append(group, v.Gene, v.Transcript, fmt.Sprint(v.ExonCount), v.Type)
	case *model.STR:
		parts = append(parts, v.Chromosome, fmt.Sprint(v.Position), v.RepeatUnit)
		group = append(group, v.Gene, v.Chromosome, fmt.Sprint(v.Position), v.RepeatUnit, v.Status)
	case *model.MEIVariant:
		parts = append(parts, v.Chromosome, fmt.Sprint(v.Position), v.TEType)
		group = append(group, v.Chromosome, fmt.Sprint(v.Position), v.Gene, v.TEType)
	case *model.MitochondrialVariant:
		parts = append(parts, fmt.Sprint(v.Position), strings.ToUpper(v.Ref), strings.ToUpper(v.Alt))
		group = append(group, fmt.Sprint(v.Position), v.Ref, v.Alt)
	case *model.UPDRegion:
		parts = append(parts, v.Chromosome, fmt.Sprint(v.StartPosition), fmt.Sprint(v.EndPosition), string(v.Type))
		group = append(group, v.Chromosome, fmt.Sprint(v.StartPosition), fmt.Sprint(v.EndPosition), string(v.Type))
	case *model.ROHRegion:
		parts = append(parts, v.Chr, fmt.Sprint(v.Begin), fmt.Sprint(v.End))
		group = append(group, v.Chr, fmt.Sprint(v.Begin), fmt.Sprint(v.End))
	}
	return reviewIdentityValue{Fingerprint: hashIdentity(parts), GroupKey: strings.Join(group, "|"), ReferenceGenome: genome}
}

func hashIdentity(parts []string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func referenceGenome(task *model.Task) string {
	if task == nil {
		return ""
	}
	var inputs map[string]interface{}
	if json.Unmarshal([]byte(task.InputJSON), &inputs) == nil {
		value, _ := inputs["reference_genome"].(string)
		return strings.TrimSpace(value)
	}
	return ""
}

func executionAttempt(task *model.Task) string {
	if task != nil && task.ExecutionAttemptID != "" {
		return task.ExecutionAttemptID
	}
	if task != nil {
		return task.UUID
	}
	return ""
}

func resultImportBatchID(row interface{}) uint {
	switch v := row.(type) {
	case *model.SNVIndel:
		return v.ImportBatchID
	case *model.CNVSegment:
		return v.ImportBatchID
	case *model.CNVExon:
		return v.ImportBatchID
	case *model.STR:
		return v.ImportBatchID
	case *model.MEIVariant:
		return v.ImportBatchID
	case *model.MitochondrialVariant:
		return v.ImportBatchID
	case *model.UPDRegion:
		return v.ImportBatchID
	case *model.ROHRegion:
		return v.ImportBatchID
	}
	return 0
}
