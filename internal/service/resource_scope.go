package service

import (
	"errors"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"strings"
)

var ErrResourceConflict = errors.New("resource changed; reload before saving")
var ErrResourceForbidden = errors.New("resource is not available for maintenance")

func resourceQuery(db *gorm.DB, orgColumn, ownerColumn string, a model.OverlayActor) *gorm.DB {
	if a.OrgID != "" {
		return db.Where("("+orgColumn+" = ? OR ("+orgColumn+" = '' AND "+ownerColumn+" = ?))", a.OrgID, a.UserID)
	}
	if a.Role == string(model.SystemRoleSuperAdmin) {
		return db
	}
	return db.Where(orgColumn+" = '' AND "+ownerColumn+" = ?", a.UserID)
}

func normalizedGenes(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		v := strings.ToUpper(strings.TrimSpace(value))
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func geneListResponse(g *model.GeneList, a model.OverlayActor) model.GeneListResponse {
	r := g.ToResponse()
	r.CanMaintain = a.ResourceMaintenance(g.CreatedBy)
	return r
}
