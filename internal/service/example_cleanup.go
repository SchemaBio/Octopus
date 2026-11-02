package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Both a durable approval identity and the resource origin are required.
// Never authorize destruction from an object prefix or origin alone.
func exampleCleanupScope(db *gorm.DB, kind, id, org string) (bool, error) {
	db = db.Session(&gorm.Session{NewDB: true}).Unscoped()
	var count int64
	switch kind {
	case "task":
		err := db.Table("tasks AS r").Joins("JOIN example_data_grants g ON g.task_uuid=r.uuid AND g.org_id=r.external_org_id AND g.attempt_uuid=r.execution_attempt_id").Where("r.uuid=? AND r.external_org_id=? AND r.origin=? AND r.created_by=g.user_id", id, org, "example").Count(&count).Error
		return count == 1, err
	case "sample":
		err := db.Table("samples AS r").Joins("JOIN example_data_grants g ON g.sample_uuid=r.uuid AND g.org_id=r.external_org_id").Where("r.uuid=? AND r.external_org_id=? AND r.origin=? AND r.created_by=g.user_id", id, org, "example").Count(&count).Error
		return count == 1, err
	default:
		return false, fmt.Errorf("invalid cleanup kind")
	}
}

func validateExampleGrantPaths(g *model.ExampleDataGrant) error {
	for _, id := range []string{g.ID, g.OrgID, g.SampleUUID, g.TaskUUID, g.AttemptUUID, g.Read1UUID, g.Read2UUID} {
		v, err := uuid.Parse(id)
		if err != nil || v.String() != id {
			return fmt.Errorf("invalid example identity")
		}
	}
	return nil
}

func validateExampleAsset(ctx context.Context, asset *model.DataAsset) error {
	var g model.ExampleDataGrant
	if err := database.DB.WithContext(ctx).Where("org_id=? AND (read1_uuid=? OR read2_uuid=?)", asset.ExternalOrgID, asset.UUID, asset.UUID).First(&g).Error; err != nil {
		return err
	}
	if err := validateExampleGrantPaths(&g); err != nil {
		return err
	}
	if asset.Source != "example" || asset.Provider != model.UploadProviderS3 || asset.CreatedBy != g.UserID || !strings.HasPrefix(asset.StorageKey, exampleDataPrefix(&g)) || asset.StorageKey != exampleDataPrefix(&g)+asset.FileName || strings.ContainsAny(asset.FileName, "/\\") {
		return fmt.Errorf("example asset scope mismatch")
	}
	if (asset.UUID == g.Read1UUID && asset.ReadType != model.ReadTypeRead1) || (asset.UUID == g.Read2UUID && asset.ReadType != model.ReadTypeRead2) {
		return fmt.Errorf("example read identity mismatch")
	}
	return nil
}
