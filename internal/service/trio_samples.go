package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

const trioWaitingForFASTQ = "trio_waiting_for_fastq"

var errTrioFASTQPending = errors.New("trio FASTQ data is not ready")

func (s *TaskService) supportsWaitingTrio() bool {
	return s.cfg != nil && s.cfg.Edition == "opensource" && s.cfg.Storage.Provider == "local" &&
		s.cfg.Task.DefaultExecutor != string(model.ExecutorCVM)
}

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
	var snapshot []model.TrioMemberSnapshot
	var ped []model.PedigreeMember
	for i, m := range members {
		if seen[m.UUID] {
			return nil, nil, fmt.Errorf("trio requires three different samples")
		}
		seen[m.UUID] = true
		if m.ExpiresAt != nil && !m.ExpiresAt.After(time.Now()) {
			return nil, nil, fmt.Errorf("%s sample has expired", roles[i])
		}
		snapshot = append(snapshot, model.TrioMemberSnapshot{Role: roles[i], SampleID: m.UUID, InternalID: m.InternalID, Gender: m.Gender, AffectedStatus: statuses[i]})
		ped = append(ped, model.PedigreeMember{Gender: model.Gender(m.Gender), AffectedStatus: model.AffectedStatus(statuses[i])})
	}
	inputs["TrioWES.ped_content"] = trioPED(taskOutputPrefix(workflowUUID), ped)
	inputs["trio_members"] = snapshot
	delete(inputs, trioWaitingForFASTQ)
	waitForLocalFiles := s.supportsWaitingTrio() && actor.SharedInstance
	assets, err := s.resolveTrioFASTQs(members, snapshot, actor, inputs, waitForLocalFiles)
	if err != nil {
		if !waitForLocalFiles || !errors.Is(err, errTrioFASTQPending) {
			return nil, nil, err
		}
		inputs[trioWaitingForFASTQ] = true
		delete(inputs, "TrioWES.meta_info")
		delete(inputs, "trio_input_bytes")
	}
	return &members[0], assets, nil
}

func (s *TaskService) resolveTrioFASTQs(members []model.Sample, snapshot []model.TrioMemberSnapshot, actor model.OverlayActor, inputs map[string]interface{}, waitForLocalFiles bool) ([]model.TaskDataAsset, error) {
	snapshot = append([]model.TrioMemberSnapshot(nil), snapshot...)
	roles := make([]string, len(members))
	read1, read2 := make([]string, len(members)), make([]string, len(members))
	for i, member := range members {
		roles[i] = snapshot[i].Role
		pair := member.GetMatchedPair()
		if !matchedPairComplete(pair) {
			if waitForLocalFiles {
				return nil, errTrioFASTQPending
			}
			return nil, fmt.Errorf("%s requires complete R1/R2 data", roles[i])
		}
		read1[i], read2[i] = pair.R1Path, pair.R2Path
	}
	seenAssets := make(map[uint]bool)
	var assets []model.TaskDataAsset
	var total int64
	for i, member := range members {
		for j, key := range []string{read1[i], read2[i]} {
			asset, err := s.assetRepo.FindCompletedByStorageKey(key, actor)
			if waitForLocalFiles && errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errTrioFASTQPending
			}
			if err != nil || asset == nil {
				return nil, fmt.Errorf("%s FASTQ asset is unavailable", roles[i])
			}
			expected := model.ReadTypeRead1
			role := model.TaskAssetRoleTrioRead1
			if j == 1 {
				expected = model.ReadTypeRead2
				role = model.TaskAssetRoleTrioRead2
			}
			if asset.ExternalOrgID != member.ExternalOrgID || asset.ReadType != expected || asset.FileSize <= 0 || (asset.ExpiresAt != nil && !asset.ExpiresAt.After(time.Now())) {
				return nil, fmt.Errorf("%s FASTQ asset has invalid read type, size or expiry", roles[i])
			}
			if seenAssets[asset.ID] {
				return nil, fmt.Errorf("trio members must use six different FASTQ assets")
			}
			if waitForLocalFiles && asset.Provider == model.UploadProviderLocal {
				path, err := safeLocalUploadPath(s.cfg.Storage.LocalDir, key)
				if err != nil {
					return nil, errTrioFASTQPending
				}
				file, err := os.Open(path)
				if err != nil {
					return nil, errTrioFASTQPending
				}
				info, statErr := file.Stat()
				file.Close()
				if statErr != nil || info.Size() != asset.FileSize {
					return nil, errTrioFASTQPending
				}
				if j == 0 {
					read1[i] = path
				} else {
					read2[i] = path
				}
			}
			seenAssets[asset.ID] = true
			total += asset.FileSize
			assets = append(assets, model.TaskDataAsset{AssetID: asset.ID, InputRole: role, InputIndex: i})
			if j == 0 {
				snapshot[i].Read1AssetID = asset.UUID
			} else {
				snapshot[i].Read2AssetID = asset.UUID
			}
		}
	}
	inputs["TrioWES.meta_info"] = map[string]interface{}{"members": roles, "read_1": read1, "read_2": read2}
	inputs["trio_members"] = snapshot
	inputs["trio_input_bytes"] = total
	return assets, nil
}

// Bind under the task start lock. The stored member identities and PED remain
// fixed even if the proband's family links change while the task is waiting.
func (s *TaskService) bindWaitingTrio(tx *gorm.DB, task *model.Task) error {
	var inputs map[string]interface{}
	if err := json.Unmarshal([]byte(task.InputJSON), &inputs); err != nil {
		return err
	}
	if inputs[trioWaitingForFASTQ] != true {
		return nil
	}
	var saved struct {
		Members []model.TrioMemberSnapshot `json:"trio_members"`
	}
	if err := json.Unmarshal([]byte(task.InputJSON), &saved); err != nil {
		return err
	}
	if len(saved.Members) != 3 || task.ExternalOrgID != "" {
		return fmt.Errorf("invalid waiting trio snapshot")
	}
	actor := model.OverlayActor{UserID: task.CreatedBy, SharedInstance: true}
	members := make([]model.Sample, 0, len(saved.Members))
	for i, role := range []string{"proband", "father", "mother"} {
		member := saved.Members[i]
		if member.Role != role || member.SampleID == "" || (i == 0 && member.SampleID != task.SampleID) {
			return fmt.Errorf("invalid waiting trio member: %s", role)
		}
		sample, err := s.sampleRepo.FindScopedByUUID(member.SampleID, actor)
		if err != nil || (sample.ExpiresAt != nil && !sample.ExpiresAt.After(time.Now())) {
			return fmt.Errorf("saved trio member %s is unavailable", role)
		}
		members = append(members, *sample)
	}
	assets, err := s.resolveTrioFASTQs(members, saved.Members, actor, inputs, true)
	if err != nil {
		return err
	}
	for i := range assets {
		assets[i].TaskUUID = task.UUID
	}
	if err := lockTaskAssets(tx, assets); err != nil {
		return err
	}
	if err := tx.Create(&assets).Error; err != nil {
		return err
	}
	delete(inputs, trioWaitingForFASTQ)
	data, err := json.Marshal(inputs)
	if err != nil {
		return err
	}
	task.InputJSON = string(data)
	task.EstimatedMinutes = trioEstimatedMinutes(inputs)
	return nil
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
	result.WaitingForData = inputs[trioWaitingForFASTQ] == true
	result.Ready = !result.WaitingForData
	result.EstimatedMinutes = trioEstimatedMinutes(inputs)
	for _, m := range result.Members {
		if (m.Role == "father" && m.Gender == model.SampleGenderFemale) || (m.Role == "mother" && m.Gender == model.SampleGenderMale) {
			result.Warnings = append(result.Warnings, m.Role+": parent_gender_mismatch")
		}
	}
	return result, nil
}
