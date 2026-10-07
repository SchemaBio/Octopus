package service

import (
	"errors"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
)

type ResourceReference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ResourceInUseError struct{ References []ResourceReference }

func (e *ResourceInUseError) Error() string {
	return "BED is referenced by a workflow, baseline, or active task; remove the references first"
}

func lockTaskAssets(tx *gorm.DB, links []model.TaskDataAsset) error {
	ids := map[uint]bool{}
	for _, l := range links {
		ids[l.AssetID] = true
	}
	ordered := make([]int, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, int(id))
	}
	sort.Ints(ordered)
	for _, id := range ordered {
		var a model.DataAsset
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&a, id).Error; e != nil {
			return e
		}
		if a.Status == model.FileStatusDeleted || a.Status == model.FileStatusDeleting {
			return errors.New("referenced asset was deleted")
		}
		if a.ReadType == model.ReadTypeBed && !bedUsable(&a) {
			return errors.New("BED content validation must pass before task submission")
		}
	}
	return nil
}
func lockPipelineBED(tx *gorm.DB, p *model.Pipeline) error {
	if p.BEDAssetID == nil {
		return nil
	}
	return lockTaskAssets(tx, []model.TaskDataAsset{{AssetID: *p.BEDAssetID}})
}

func checkBEDReferences(tx *gorm.DB, a *model.DataAsset) error {
	if a.ReadType != model.ReadTypeBed {
		return nil
	}
	refs := make([]ResourceReference, 0)
	var pipelines []model.Pipeline
	if e := tx.Where("bed_asset_id=?", a.ID).Find(&pipelines).Error; e != nil {
		return e
	}
	for _, p := range pipelines {
		refs = append(refs, ResourceReference{"pipeline", p.ID, p.Name})
	}
	var baselines []model.CNVBaseline
	if e := tx.Where("bed_asset_id=?", a.ID).Find(&baselines).Error; e != nil {
		return e
	}
	for _, b := range baselines {
		refs = append(refs, ResourceReference{"baseline", b.UUID, b.Name})
	}
	var tasks []model.Task
	if e := tx.Model(&model.Task{}).Joins("JOIN task_data_assets AS a ON a.task_uuid=tasks.uuid").Where("a.asset_id=? AND tasks.status NOT IN ?", a.ID, []model.TaskStatus{model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusCancelled, model.TaskStatusPendingInterpretation}).Distinct("tasks.*").Find(&tasks).Error; e != nil {
		return e
	}
	for _, t := range tasks {
		refs = append(refs, ResourceReference{"task", t.UUID, t.Name})
	}
	if len(refs) > 0 {
		return &ResourceInUseError{refs}
	}
	return nil
}

func validateSavedTaskResources(tx *gorm.DB, t *model.Task) error {
	var links []model.TaskDataAsset
	if e := tx.Where("task_uuid=?", t.UUID).Find(&links).Error; e != nil {
		return e
	}
	if e := lockTaskAssets(tx, links); e != nil {
		return e
	}
	if t.PipelineSnapshotJSON != "" && t.PipelineSnapshotJSON != "{}" {
		var snap pipelineResourceSnapshot
		if e := decodePipelineSnapshot(t.PipelineSnapshotJSON, &snap); e != nil {
			return e
		}
		if snap.BEDID != 0 {
			var bed model.DataAsset
			if e := tx.First(&bed, snap.BEDID).Error; e != nil {
				return e
			}
			if snap.BEDSHA256 != "" && bed.ValidationSHA256 != snap.BEDSHA256 {
				return errors.New("saved BED content identity changed")
			}
		}
		if snap.BaselineID != 0 {
			var b model.CNVBaseline
			if e := tx.First(&b, snap.BaselineID).Error; e != nil || b.OutputPath == "" {
				return fmt.Errorf("saved CNV baseline is unavailable")
			}
		}
	}
	return nil
}
