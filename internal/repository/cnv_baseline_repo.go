package repository

import (
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

type CNVBaselineRepository struct{}

func NewCNVBaselineRepository() *CNVBaselineRepository { return &CNVBaselineRepository{} }

func (r *CNVBaselineRepository) Create(baseline *model.CNVBaseline, pairs []model.CNVBaselineReadPair) error {
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(baseline).Error; err != nil {
			return err
		}
		for i := range pairs {
			pairs[i].BaselineID = baseline.ID
		}
		if len(pairs) > 0 {
			return tx.Create(&pairs).Error
		}
		return nil
	})
}

func (r *CNVBaselineRepository) List(actor model.OverlayActor) ([]model.CNVBaseline, error) {
	db := database.GetDB().Model(&model.CNVBaseline{})
	if actor.Role != string(model.SystemRoleSuperAdmin) {
		if actor.OrgID != "" {
			db = db.Where("external_org_id = ?", actor.OrgID)
		} else {
			db = db.Where("external_org_id = '' AND created_by = ?", actor.UserID)
		}
	}
	var baselines []model.CNVBaseline
	err := db.Order("created_at DESC").Find(&baselines).Error
	return baselines, err
}

func (r *CNVBaselineRepository) FindScopedByUUID(uuid string, actor model.OverlayActor) (*model.CNVBaseline, error) {
	db := database.GetDB().Where("uuid = ?", uuid)
	if actor.Role != string(model.SystemRoleSuperAdmin) {
		if actor.OrgID != "" {
			db = db.Where("external_org_id = ?", actor.OrgID)
		} else {
			db = db.Where("external_org_id = '' AND created_by = ?", actor.UserID)
		}
	}
	var baseline model.CNVBaseline
	if err := db.First(&baseline).Error; err != nil {
		return nil, err
	}
	return &baseline, nil
}

func (r *CNVBaselineRepository) FindPairs(baselineID uint) ([]model.CNVBaselineReadPair, error) {
	var pairs []model.CNVBaselineReadPair
	err := database.GetDB().Where("baseline_id = ?", baselineID).Order("pair_index").Find(&pairs).Error
	return pairs, err
}
