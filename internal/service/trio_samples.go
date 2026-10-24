package service

import (
	"fmt"
	"github.com/SchemaBio/Octopus/internal/model"
	"strings"
	"time"
)

func (s *TaskService) prepareSampleTrio(sampleID, pedigreeID, workflowUUID string, actor model.OverlayActor, inputs map[string]interface{}) (*model.Sample, []model.TaskDataAsset, error) {
	if strings.TrimSpace(sampleID) == "" {
		if pedigreeID == "" {
			return nil, nil, fmt.Errorf("sampleId is required for trio analysis")
		}
		return s.prepareTrioInputs(pedigreeID, workflowUUID, actor, inputs)
	}
	child, err := s.sampleRepo.FindScopedByUUID(sampleID, actor)
	if err != nil {
		return nil, nil, fmt.Errorf("proband sample not found")
	}
	rows := []model.Sample{*child}
	ss := NewSampleService(s.cfg)
	if err := ss.enrichParents(rows); err != nil {
		return nil, nil, err
	}
	child = &rows[0]
	members := []model.Sample{*child}
	for _, v := range []struct {
		role string
		info *model.ParentSampleInfo
	}{{"father", child.Father}, {"mother", child.Mother}} {
		if v.info == nil || v.info.State != "found" {
			state := "missing"
			if v.info != nil && v.info.State != "empty" {
				state = v.info.State
			}
			return nil, nil, fmt.Errorf("%s sample %s", v.role, state)
		}
		parent, err := s.sampleRepo.FindScopedByUUID(v.info.ID, actor)
		if err != nil {
			return nil, nil, fmt.Errorf("%s sample is unavailable", v.role)
		}
		members = append(members, *parent)
	}
	return s.prepareTrioMembers(members, []string{"affected", child.FatherAffectedStatus, child.MotherAffectedStatus}, workflowUUID, actor, inputs)
}

func (s *TaskService) prepareTrioMembers(members []model.Sample, statuses []string, workflowUUID string, actor model.OverlayActor, inputs map[string]interface{}) (*model.Sample, []model.TaskDataAsset, error) {
	if len(members) != 3 || len(statuses) != 3 {
		return nil, nil, fmt.Errorf("trio requires three different samples")
	}
	ss := NewSampleService(s.cfg)
	for i := 1; i < len(members); i++ {
		if !ss.sameParentScope(&members[0], &members[i]) {
			return nil, nil, fmt.Errorf("trio samples must belong to the same sample scope")
		}
	}
	roles := []string{"proband", "father", "mother"}
	seen := map[string]bool{}
	seenAssets := map[uint]bool{}
	read1, read2 := make([]string, 3), make([]string, 3)
	var assets []model.TaskDataAsset
	var snapshot []model.TrioMemberSnapshot
	var ped []model.PedigreeMember
	var total int64
	for i, m := range members {
		if seen[m.UUID] {
			return nil, nil, fmt.Errorf("trio requires three different samples")
		}
		seen[m.UUID] = true
		if m.ExpiresAt != nil && !m.ExpiresAt.After(time.Now()) {
			return nil, nil, fmt.Errorf("%s sample has expired", roles[i])
		}
		pair := m.GetMatchedPair()
		if !matchedPairComplete(pair) {
			return nil, nil, fmt.Errorf("%s requires complete R1/R2 data", roles[i])
		}
		read1[i], read2[i] = pair.R1Path, pair.R2Path
		member := model.TrioMemberSnapshot{Role: roles[i], SampleID: m.UUID, InternalID: m.InternalID, Gender: m.Gender, AffectedStatus: statuses[i]}
		for j, key := range []string{pair.R1Path, pair.R2Path} {
			asset, err := s.assetRepo.FindCompletedByStorageKey(key, actor)
			if err != nil || asset == nil {
				return nil, nil, fmt.Errorf("%s FASTQ asset is unavailable", roles[i])
			}
			expected := model.ReadTypeRead1
			role := model.TaskAssetRoleTrioRead1
			if j == 1 {
				expected = model.ReadTypeRead2
				role = model.TaskAssetRoleTrioRead2
			}
			if asset.ExternalOrgID != m.ExternalOrgID || asset.ReadType != expected || asset.FileSize <= 0 || (asset.ExpiresAt != nil && !asset.ExpiresAt.After(time.Now())) {
				return nil, nil, fmt.Errorf("%s FASTQ asset has invalid read type, size or expiry", roles[i])
			}
			if seenAssets[asset.ID] {
				return nil, nil, fmt.Errorf("trio members must use six different FASTQ assets")
			}
			seenAssets[asset.ID] = true
			total += asset.FileSize
			assets = append(assets, model.TaskDataAsset{AssetID: asset.ID, InputRole: role, InputIndex: i})
			if j == 0 {
				member.Read1AssetID = asset.UUID
			} else {
				member.Read2AssetID = asset.UUID
			}
		}
		snapshot = append(snapshot, member)
		ped = append(ped, model.PedigreeMember{Gender: model.Gender(m.Gender), AffectedStatus: model.AffectedStatus(statuses[i])})
	}
	inputs["TrioWES.meta_info"] = map[string]interface{}{"members": roles, "read_1": read1, "read_2": read2}
	inputs["TrioWES.ped_content"] = trioPED(taskOutputPrefix(workflowUUID), ped)
	inputs["trio_members"] = snapshot
	inputs["trio_input_bytes"] = total
	return &members[0], assets, nil
}

func trioEstimatedMinutes(inputs map[string]interface{}) int {
	var total int64
	switch v := inputs["trio_input_bytes"].(type) {
	case int64:
		total = v
	case float64:
		total = int64(v)
	}
	if total > 0 {
		return estimateTaskMinutesFromBytes(total)
	}
	return defaultTaskEstimatedMinutes
}

func (s *TaskService) TrioContext(sampleID string, actor model.OverlayActor) (*model.TrioContext, error) {
	if _, err := s.sampleRepo.FindScopedByUUID(sampleID, actor); err != nil {
		return nil, fmt.Errorf("sample not found")
	}
	result := &model.TrioContext{Members: []model.TrioMemberSnapshot{}, Errors: []string{}, Warnings: []string{}}
	inputs := map[string]interface{}{}
	_, _, err := s.prepareSampleTrio(sampleID, "", "preflight", actor, inputs)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	result.Members = inputs["trio_members"].([]model.TrioMemberSnapshot)
	result.Ready = true
	result.EstimatedMinutes = trioEstimatedMinutes(inputs)
	for _, m := range result.Members {
		if (m.Role == "father" && m.Gender == model.SampleGenderFemale) || (m.Role == "mother" && m.Gender == model.SampleGenderMale) {
			result.Warnings = append(result.Warnings, m.Role+": parent_gender_mismatch")
		}
	}
	return result, nil
}
