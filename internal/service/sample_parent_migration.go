package service

import (
	"fmt"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"log"
)

// Mark each pre-existing sample once, including conflicts and intentional blanks.
// Restarting the service must not recreate relationships a user later cleared.
func BackfillSampleParents(cfg *config.Config) error {
	db := database.GetDB()
	var remaining int64
	if err := db.Model(&model.Sample{}).Where("parents_migrated = ?", false).Count(&remaining).Error; err != nil {
		return err
	}
	if remaining == 0 {
		return nil
	}
	var samples []model.Sample
	if err := db.Find(&samples).Error; err != nil {
		return err
	}
	pending := false
	byUUID := map[string]model.Sample{}
	for _, sample := range samples {
		byUUID[sample.UUID] = sample
		if !sample.ParentsMigrated {
			pending = true
		}
	}
	if !pending {
		return nil
	}
	var pedigrees []model.Pedigree
	var members []model.PedigreeMember
	if err := db.Find(&pedigrees).Error; err != nil {
		return err
	}
	if err := db.Find(&members).Error; err != nil {
		return err
	}
	byFamily := map[string][]model.PedigreeMember{}
	for _, m := range members {
		byFamily[m.PedigreeID] = append(byFamily[m.PedigreeID], m)
	}
	type candidate struct{ father, mother, fs, ms string }
	options := map[string][]candidate{}
	ss := NewSampleService(cfg)
	for _, family := range pedigrees {
		rows := byFamily[family.ID]
		byID := map[string]model.PedigreeMember{}
		var probands []model.PedigreeMember
		for _, m := range rows {
			byID[m.ID] = m
			if m.ID == family.ProbandMemberID || m.Relation == model.RelationProband {
				probands = append(probands, m)
			}
		}
		if len(probands) != 1 {
			continue
		}
		p := probands[0]
		f, fok := byID[p.FatherID]
		m, mok := byID[p.MotherID]
		if !fok || !mok || f.Relation != model.RelationFather || m.Relation != model.RelationMother {
			continue
		}
		child, cok := byUUID[p.SampleID]
		father, fok := byUUID[f.SampleID]
		mother, mok := byUUID[m.SampleID]
		if !cok || !fok || !mok || !ss.sameParentScope(&child, &father) || !ss.sameParentScope(&child, &mother) || child.ExternalOrgID != family.ExternalOrgID {
			continue
		}
		normalize := func(v model.AffectedStatus) string {
			if v == model.AffectedStatusUnknown {
				return ""
			}
			return string(v)
		}
		options[p.SampleID] = append(options[p.SampleID], candidate{f.SampleID, m.SampleID, normalize(f.AffectedStatus), normalize(m.AffectedStatus)})
	}
	migrated, conflicts := 0, 0
	for _, child := range samples {
		if child.ParentsMigrated {
			continue
		}
		updates := map[string]interface{}{"parents_migrated": true}
		if child.FatherSampleInternalID == "" && child.MotherSampleInternalID == "" && child.FatherSampleUUID == "" && child.MotherSampleUUID == "" && child.FatherAffectedStatus == "" && child.MotherAffectedStatus == "" {
			candidates := options[child.UUID]
			if len(candidates) > 0 {
				selected := candidates[0]
				unique := true
				for _, other := range candidates[1:] {
					if other != selected {
						unique = false
					}
				}
				if unique {
					father, mother := byUUID[selected.father], byUUID[selected.mother]
					err := ss.applyParents(&child, model.SampleParentUpdate{FatherSampleInternalID: &father.InternalID, MotherSampleInternalID: &mother.InternalID, FatherAffectedStatus: &selected.fs, MotherAffectedStatus: &selected.ms})
					if err == nil {
						updates["father_sample_internal_id"] = father.InternalID
						updates["mother_sample_internal_id"] = mother.InternalID
						updates["father_sample_uuid"] = father.UUID
						updates["mother_sample_uuid"] = mother.UUID
						updates["father_affected_status"] = selected.fs
						updates["mother_affected_status"] = selected.ms
						migrated++
					} else {
						unique = false
					}
				}
				if !unique {
					conflicts++
					log.Printf("sample parent migration conflict: sample UUID %s; retained existing values", child.UUID)
				}
			}
		}
		if err := db.Model(&model.Sample{}).Where("id = ? AND parents_migrated = ?", child.ID, false).Updates(updates).Error; err != nil {
			return fmt.Errorf("sample parent backfill: %w", err)
		}
	}
	log.Printf("sample parent migration: backfilled=%d conflicts=%d", migrated, conflicts)
	return nil
}
