package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

func (s *SampleService) parentScope(db *gorm.DB, child *model.Sample) *gorm.DB {
	if child.ExternalOrgID != "" {
		return db.Where("external_org_id = ?", child.ExternalOrgID)
	}
	db = db.Where("COALESCE(external_org_id, '') = ''")
	if s.cfg == nil || s.cfg.Edition != "opensource" {
		db = db.Where("created_by = ?", child.CreatedBy)
	}
	return db
}

func (s *SampleService) sameParentScope(a, b *model.Sample) bool {
	if a.ExternalOrgID != b.ExternalOrgID {
		return false
	}
	return a.ExternalOrgID != "" || (s.cfg != nil && s.cfg.Edition == "opensource") || a.CreatedBy == b.CreatedBy
}

func resolveParent(child *model.Sample, raw, bound, role string, candidates []model.Sample, allowed func(*model.Sample, *model.Sample) bool) (*model.ParentSampleInfo, *model.Sample) {
	info := &model.ParentSampleInfo{State: "empty"}
	if raw == "" && bound == "" {
		return info, nil
	}
	var matches []*model.Sample
	for i := range candidates {
		item := &candidates[i]
		if !allowed(child, item) {
			continue
		}
		if (bound != "" && item.UUID == bound) || (bound == "" && item.InternalID == raw) {
			matches = append(matches, item)
		}
	}
	info.State = "missing"
	if len(matches) > 1 {
		info.State = "ambiguous"
		return info, nil
	}
	if len(matches) == 0 {
		return info, nil
	}
	parent := matches[0]
	if parent.ExpiresAt != nil && !parent.ExpiresAt.After(time.Now()) {
		info.State = "unavailable"
		return info, nil
	}
	info.State = "found"
	info.ID = parent.UUID
	info.InternalID = parent.InternalID
	info.Gender = parent.Gender
	info.DataReady = matchedPairComplete(parent.GetMatchedPair())
	if (role == "father" && parent.Gender == model.SampleGenderFemale) || (role == "mother" && parent.Gender == model.SampleGenderMale) {
		info.Warning = "parent_gender_mismatch"
	}
	return info, parent
}

// One candidate query covers all rows, including parents outside the current page.
func (s *SampleService) enrichParents(samples []model.Sample) error {
	if len(samples) == 0 {
		return nil
	}
	var names, ids []string
	for _, x := range samples {
		if x.FatherSampleUUID == "" && x.FatherSampleInternalID != "" {
			names = append(names, x.FatherSampleInternalID)
		}
		if x.MotherSampleUUID == "" && x.MotherSampleInternalID != "" {
			names = append(names, x.MotherSampleInternalID)
		}
		for _, id := range []string{x.FatherSampleUUID, x.MotherSampleUUID} {
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	if len(names) == 0 && len(ids) == 0 {
		return nil
	}
	if database.GetDB() == nil {
		return fmt.Errorf("database not initialized")
	}
	var candidates []model.Sample
	var scopes []string
	var scopeArgs []interface{}
	seenScopes := map[string]bool{}
	for _, child := range samples {
		key := child.ExternalOrgID
		if key != "" {
			if !seenScopes["org:"+key] {
				scopes = append(scopes, "external_org_id = ?")
				scopeArgs = append(scopeArgs, key)
				seenScopes["org:"+key] = true
			}
		} else if s.cfg != nil && s.cfg.Edition == "opensource" {
			if !seenScopes["shared"] {
				scopes = append(scopes, "COALESCE(external_org_id, '') = ''")
				seenScopes["shared"] = true
			}
		} else {
			key = fmt.Sprintf("user:%d", child.CreatedBy)
			if !seenScopes[key] {
				scopes = append(scopes, "(COALESCE(external_org_id, '') = '' AND created_by = ?)")
				scopeArgs = append(scopeArgs, child.CreatedBy)
				seenScopes[key] = true
			}
		}
	}
	if err := database.GetDB().Where("internal_id IN ? OR uuid IN ?", names, ids).Where(strings.Join(scopes, " OR "), scopeArgs...).Find(&candidates).Error; err != nil {
		return err
	}
	for i := range samples {
		child := &samples[i]
		for _, v := range []struct {
			role       string
			raw, bound *string
			target     **model.ParentSampleInfo
		}{{"father", &child.FatherSampleInternalID, &child.FatherSampleUUID, &child.Father}, {"mother", &child.MotherSampleInternalID, &child.MotherSampleUUID, &child.Mother}} {
			info, parent := resolveParent(child, *v.raw, *v.bound, v.role, candidates, s.sameParentScope)
			*v.target = info
			if parent != nil {
				if *v.bound == "" {
					result := database.GetDB().Model(&model.Sample{}).Where("id = ? AND "+v.role+"_sample_uuid = '' AND "+v.role+"_sample_internal_id = ?", child.ID, *v.raw).Update(v.role+"_sample_uuid", parent.UUID)
					if result.Error != nil {
						return result.Error
					}
					*v.bound = parent.UUID
				}
				*v.raw = parent.InternalID
			}
		}
	}
	return nil
}

func (s *SampleService) applyParents(child *model.Sample, update model.SampleParentUpdate) error {
	if update.FatherSampleInternalID == nil && update.MotherSampleInternalID == nil && update.FatherAffectedStatus == nil && update.MotherAffectedStatus == nil && child.FatherSampleInternalID == "" && child.MotherSampleInternalID == "" && child.FatherSampleUUID == "" && child.MotherSampleUUID == "" {
		return nil
	}
	for _, v := range []struct {
		input  *string
		target *string
	}{{update.FatherAffectedStatus, &child.FatherAffectedStatus}, {update.MotherAffectedStatus, &child.MotherAffectedStatus}} {
		if v.input == nil {
			continue
		}
		value := strings.TrimSpace(*v.input)
		if value != "" && value != "affected" && value != "unaffected" && value != "carrier" {
			return fmt.Errorf("invalid parent affected status")
		}
		*v.target = value
	}
	for _, v := range []struct {
		input      *string
		raw, bound *string
	}{{update.FatherSampleInternalID, &child.FatherSampleInternalID, &child.FatherSampleUUID}, {update.MotherSampleInternalID, &child.MotherSampleInternalID, &child.MotherSampleUUID}} {
		if v.input == nil {
			continue
		}
		value := strings.TrimSpace(*v.input)
		if len(value) > 100 {
			return fmt.Errorf("parent internal ID exceeds 100 characters")
		}
		if value == "" {
			*v.raw = ""
			*v.bound = ""
			continue
		}
		if value != *v.raw && *v.bound != "" {
			var parent model.Sample
			err := s.parentScope(database.GetDB(), child).Where("uuid = ?", *v.bound).First(&parent).Error
			if err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
			if err != nil || parent.InternalID != value {
				*v.bound = ""
			}
		}
		*v.raw = value
		if value == "" {
			*v.bound = ""
		}
	}
	if child.FatherSampleInternalID != "" && child.FatherSampleInternalID == child.MotherSampleInternalID {
		return fmt.Errorf("father and mother must be different samples")
	}
	if (child.FatherSampleInternalID != "" && child.FatherSampleInternalID == child.InternalID) || (child.MotherSampleInternalID != "" && child.MotherSampleInternalID == child.InternalID) || (child.FatherSampleUUID != "" && child.FatherSampleUUID == child.UUID) || (child.MotherSampleUUID != "" && child.MotherSampleUUID == child.UUID) {
		return fmt.Errorf("sample parent relationship contains a cycle")
	}
	if child.FatherSampleInternalID == "" && child.MotherSampleInternalID == "" && child.FatherSampleUUID == "" && child.MotherSampleUUID == "" {
		return nil
	}
	// Load the reachable ancestry in the child's scope. Resolve pending names too,
	// so importing the last sample in a cycle cannot make that cycle valid.
	var rows []model.Sample
	if err := s.parentScope(database.GetDB(), child).Find(&rows).Error; err != nil {
		return err
	}
	replaced := false
	for i := range rows {
		if rows[i].UUID == child.UUID {
			rows[i] = *child
			replaced = true
		}
	}
	if !replaced {
		rows = append(rows, *child)
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var walk func(*model.Sample) error
	walk = func(node *model.Sample) error {
		if visiting[node.UUID] {
			return fmt.Errorf("sample parent relationship contains a cycle")
		}
		if visited[node.UUID] {
			return nil
		}
		visiting[node.UUID] = true
		for _, v := range []struct{ role, raw, bound string }{{"father", node.FatherSampleInternalID, node.FatherSampleUUID}, {"mother", node.MotherSampleInternalID, node.MotherSampleUUID}} {
			_, parent := resolveParent(node, v.raw, v.bound, v.role, rows, s.sameParentScope)
			if parent != nil {
				if err := walk(parent); err != nil {
					return err
				}
			}
		}
		delete(visiting, node.UUID)
		visited[node.UUID] = true
		return nil
	}
	if err := walk(child); err != nil {
		return err
	}
	for _, v := range []struct {
		role, raw string
		bound     *string
	}{{"father", child.FatherSampleInternalID, &child.FatherSampleUUID}, {"mother", child.MotherSampleInternalID, &child.MotherSampleUUID}} {
		_, parent := resolveParent(child, v.raw, *v.bound, v.role, rows, s.sameParentScope)
		if parent != nil {
			*v.bound = parent.UUID
		}
	}
	if child.FatherSampleUUID != "" && child.FatherSampleUUID == child.MotherSampleUUID {
		return fmt.Errorf("father and mother must be different samples")
	}
	return nil
}

func (s *SampleService) ResolveParents(childID, internalID, father, mother string, actor model.OverlayActor) (*model.SampleParents, error) {
	child := &model.Sample{InternalID: internalID, ExternalOrgID: actor.OrgID, CreatedBy: actor.UserID}
	if childID != "" {
		var err error
		child, err = s.repo.FindScopedByUUID(childID, actor)
		if err != nil {
			return nil, err
		}
	}
	var rows []model.Sample
	if err := s.parentScope(database.GetDB(), child).Where("internal_id IN ? OR uuid IN ?", []string{father, mother}, []string{child.FatherSampleUUID, child.MotherSampleUUID}).Find(&rows).Error; err != nil {
		return nil, err
	}
	bound := func(requested, raw, id string) string {
		if requested == "" {
			return ""
		}
		if requested == raw {
			return id
		}
		for _, row := range rows {
			if row.UUID == id && row.InternalID == requested {
				return id
			}
		}
		return ""
	}
	f, _ := resolveParent(child, father, bound(father, child.FatherSampleInternalID, child.FatherSampleUUID), "father", rows, s.sameParentScope)
	m, _ := resolveParent(child, mother, bound(mother, child.MotherSampleInternalID, child.MotherSampleUUID), "mother", rows, s.sameParentScope)
	return &model.SampleParents{Father: f, Mother: m}, nil
}
