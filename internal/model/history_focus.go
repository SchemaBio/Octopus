package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HistoryFocus is a compact projection of one focused immutable source row.
// A tombstone keeps only the identity needed to evict a browser's cached row.
type HistoryFocus struct {
	ID                    string     `json:"id" gorm:"primaryKey;size:64;index:idx_history_sync,priority:4"`
	TenantID              string     `json:"-" gorm:"size:160;index:idx_history_sync,priority:1"`
	Table                 string     `json:"table" gorm:"size:32;index:idx_history_sync,priority:2"`
	Revision              uint64     `json:"revision" gorm:"index:idx_history_sync,priority:3"`
	TaskUUID              string     `json:"taskUuid" gorm:"size:36;index"`
	AttemptID             string     `json:"attemptId" gorm:"size:36"`
	DatasetID             string     `json:"-" gorm:"size:64"`
	DatasetVersion        string     `json:"-" gorm:"size:64"`
	RowID                 string     `json:"-" gorm:"size:64"`
	RowOrdinal            int64      `json:"-"`
	GroupKey              string     `json:"groupKey" gorm:"size:64"`
	Reference             string     `json:"reference" gorm:"size:100"`
	ReferenceIdentity     string     `json:"-" gorm:"size:64"`
	IdentityKnown         bool       `json:"identityKnown"`
	FieldsJSON            string     `json:"-" gorm:"type:jsonb;not null"`
	Focused               bool       `json:"focused"`
	Deleted               bool       `json:"deleted"`
	Classification        string     `json:"classification" gorm:"size:40"`
	FocusedClassification string     `json:"focusedClassification" gorm:"size:40"`
	ACMGVersion           string     `json:"acmgVersion,omitempty" gorm:"size:16"`
	VusSubclass           string     `json:"vusSubclass,omitempty" gorm:"size:16"`
	FocusedACMGVersion    string     `json:"focusedAcmgVersion,omitempty" gorm:"size:16"`
	FocusedVusSubclass    string     `json:"focusedVusSubclass,omitempty" gorm:"size:16"`
	FirstFocusedAt        *time.Time `json:"firstFocusedAt"`
	LastFocusedAt         *time.Time `json:"lastFocusedAt"`
	FocusedBy             string     `json:"focusedBy" gorm:"size:160"`
	AdjustmentVersion     uint64     `json:"adjustmentVersion"`
	CurrentSource         *bool      `json:"currentSource,omitempty" gorm:"-"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

func (r HistoryFocus) MarshalJSON() ([]byte, error) {
	if r.Deleted {
		return json.Marshal(map[string]interface{}{"id": r.ID, "revision": r.Revision, "deleted": true})
	}
	type alias HistoryFocus
	return json.Marshal(struct {
		alias
		Fields json.RawMessage `json:"fields"`
	}{alias(r), json.RawMessage(r.FieldsJSON)})
}

// Serializing per-scope revision allocation prevents committed writes from
// becoming visible below a cursor the browser has already consumed.
type HistoryScopeRevision struct {
	TenantID string `gorm:"primaryKey;size:160"`
	Revision uint64 `gorm:"not null;default:0"`
}

func NextHistoryRevision(tx *gorm.DB, tenant string) (uint64, error) {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&HistoryScopeRevision{TenantID: tenant}).Error; err != nil {
		return 0, err
	}
	var clock HistoryScopeRevision
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=?", tenant).First(&clock).Error; err != nil {
		return 0, err
	}
	clock.Revision++
	return clock.Revision, tx.Model(&clock).Update("revision", clock.Revision).Error
}

// DeleteHistoryFocuses must share the task's deletion transaction.
func DeleteHistoryFocuses(tx *gorm.DB, task *Task) error {
	var count int64
	if err := tx.Model(&HistoryFocus{}).Where("task_uuid=? AND deleted=false", task.UUID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	revision, err := NextHistoryRevision(tx, TenantIDForTask(task))
	if err != nil {
		return err
	}
	return tx.Model(&HistoryFocus{}).Where("task_uuid=? AND deleted=false", task.UUID).Updates(map[string]interface{}{
		"revision": revision, "deleted": true, "focused": false, "fields_json": "{}", "classification": "", "focused_classification": "", "focused_by": "", "updated_at": time.Now().UTC(),
	}).Error
}

func (HistoryFocus) TableName() string { return "history_focuses" }
