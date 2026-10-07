package service

import (
	"encoding/json"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

type pipelineResourceSnapshot struct {
	BEDUUID              string `json:"bed_uuid,omitempty"`
	BEDName              string `json:"bed_name,omitempty"`
	BEDSHA256            string `json:"bed_sha256,omitempty"`
	ReferenceIndexSHA256 string `json:"reference_index_sha256,omitempty"`
	BaselineUUID         string `json:"baseline_uuid,omitempty"`
	BaselineName         string `json:"baseline_name,omitempty"`
	PipelineID           string `json:"pipeline_id"`
	Version              string `json:"version"`
	Reference            string `json:"reference"`
	BEDID                uint   `json:"bed_id,omitempty"`
	BaselineID           uint   `json:"baseline_id,omitempty"`
}

func makePipelineSnapshot(p *model.Pipeline, r *model.TaskCreateRequest) string {
	snap := pipelineResourceSnapshot{PipelineID: r.PipelineID, Version: r.PipelineVersion}
	if value, ok := r.Inputs["reference_genome"].(string); ok {
		snap.Reference = value
	}
	if p != nil {
		snap.PipelineID = p.ID
		snap.Version = p.Version
		snap.Reference = p.ReferenceGenome
		if p.BEDAssetID != nil {
			snap.BEDID = *p.BEDAssetID
		}
		if p.CNVBaselineID != nil {
			snap.BaselineID = *p.CNVBaselineID
		}
	}
	data, _ := json.Marshal(snap)
	return string(data)
}

func freezeTaskResourceIdentities(tx *gorm.DB, t *model.Task, links []model.TaskDataAsset) error {
	var snap pipelineResourceSnapshot
	if e := decodePipelineSnapshot(t.PipelineSnapshotJSON, &snap); e != nil {
		return e
	}
	for _, l := range links {
		var a model.DataAsset
		if e := tx.First(&a, l.AssetID).Error; e != nil {
			return e
		}
		if a.ReadType == model.ReadTypeBed {
			snap.BEDID = a.ID
			snap.BEDUUID = a.UUID
			snap.BEDName = a.FileName
			snap.BEDSHA256 = a.ValidationSHA256
			snap.ReferenceIndexSHA256 = a.ValidationReferenceSHA256
		}
	}
	if snap.BaselineID != 0 {
		var b model.CNVBaseline
		if e := tx.First(&b, snap.BaselineID).Error; e != nil {
			return e
		}
		snap.BaselineUUID = b.UUID
		snap.BaselineName = b.Name
	}
	data, e := json.Marshal(snap)
	if e != nil {
		return e
	}
	t.PipelineSnapshotJSON = string(data)
	return nil
}
func decodePipelineSnapshot(raw string, snap *pipelineResourceSnapshot) error {
	return json.Unmarshal([]byte(raw), snap)
}
