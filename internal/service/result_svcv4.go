package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/svcv4"
	"strings"
)

const SVCv4Revision = "ef66faff51a265fef7b5c4e6439905f3aa540c46"

// SVCv4 computes with the pinned pure-Go reference port; saved inputs remain
// independent of the legacy assessment and submitted totals are never trusted.
func (s *ResultService) SVCv4(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil {
		return svcv4.Schema(), nil
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > 250000 {
		return nil, fmt.Errorf("SVCv4 evidence is invalid or too large")
	}
	result, err := svcv4.EvaluatePartial(input)
	if err != nil {
		return nil, fmt.Errorf("SVCv4: %w", err)
	}
	// Existing save/projection code consumes JSON maps, not internal DTOs.
	encoded, err = json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var output map[string]interface{}
	if err = json.Unmarshal(encoded, &output); err != nil {
		return nil, err
	}
	return output, nil
}

func svcAssessment(payload map[string]interface{}) map[string]interface{} {
	value, _ := payload["svcv4Assessment"].(map[string]interface{})
	return value
}
func svcResult(payload map[string]interface{}) map[string]interface{} {
	value, _ := svcAssessment(payload)["result"].(map[string]interface{})
	return value
}
func validateActiveACMG(payload map[string]interface{}) error {
	version := stringAdjustment(payload, "activeAcmgVersion")
	if snap, ok := payload["adoptedACMG"].(map[string]interface{}); ok {
		if snap["version"] != "svcv4" || (snap["state"] == "classified" && snap["classification"] != "" && snap["source"] == "svcv4_reference") {
			return nil
		}
		return fmt.Errorf("invalid adopted SVC snapshot")
	}
	if version != "" && version != "legacy" && version != "svcv4" {
		return fmt.Errorf("invalid ACMG version")
	}
	if version != "svcv4" {
		return nil
	}
	assessment, result := svcAssessment(payload), svcResult(payload)
	if assessment["confirmed"] != true || stringAdjustment(assessment, "revision") != SVCv4Revision || stringAdjustment(result, "state") != "classified" || stringAdjustment(result, "classification") == "" {
		return fmt.Errorf("新版须完成有效评定，并确认草案假设后才能采用")
	}
	return nil
}

// Only server-calculated saved evidence can become an adopted snapshot.
func snapshotAdoptedACMG(payload map[string]interface{}) map[string]interface{} {
	version := stringAdjustment(payload, "activeAcmgVersion")
	if version == "" {
		version = "legacy"
	}
	snap := map[string]interface{}{"version": version}
	if version == "svcv4" {
		snap["assessment"] = svcAssessment(payload)
		if ai, ok := payload["svcv4AIResult"].(map[string]interface{}); ok {
			snap["aiRequestId"] = ai["requestId"]
		}
		for _, k := range []string{"classification", "score", "state", "vusSubclass"} {
			snap[k] = svcResult(payload)[k]
		}
		snap["profile"], snap["source"], snap["savedAt"], snap["savedBy"] = "svcv4-draft-reference", "svcv4_reference", payload["svcv4SavedAt"], payload["svcv4SavedBy"]
	} else {
		snap["evidence"], snap["overrideReason"] = payload["acmgEvidence"], payload["acmgOverrideReason"]
		if ai, ok := payload["acmgAIResult"].(map[string]interface{}); ok {
			snap["aiRequestId"] = ai["requestId"]
		}
		_, manual := payload["acmgEvidence"]
		manual = manual || stringAdjustment(payload, "acmgOverride") != ""
		snap["automatic"] = !manual
		for _, k := range []string{"classification", "score", "profile", "state"} {
			snap[k] = payload["acmg"+strings.ToUpper(k[:1])+k[1:]]
		}
		snap["source"] = "automatic"
		if manual {
			snap["source"] = "manual_evidence"
		}
		if stringAdjustment(payload, "acmgOverride") != "" {
			snap["source"] = "manual_override"
			snap["classification"] = payload["acmgOverride"]
		}
		snap["savedAt"], snap["savedBy"] = payload["acmgSavedAt"], payload["acmgSavedBy"]
	}
	return snap
}
func preserveAdoptedACMG(payload, changes map[string]interface{}) {
	if _, exists := payload["adoptedACMG"]; exists {
		return
	}
	for _, key := range []string{"acmgEvidence", "acmgOverride", "resetAcmg", "svcv4Assessment", "activeAcmgVersion"} {
		if _, changed := changes[key]; changed {
			payload["adoptedACMG"] = snapshotAdoptedACMG(payload)
			return
		}
	}
}
func applyActiveACMG(row, payload map[string]interface{}) {
	if snap, ok := payload["adoptedACMG"].(map[string]interface{}); ok {
		row["activeAcmgVersion"] = snap["version"]
		if snap["automatic"] == true {
			if auto, ok := row["automaticAcmg"].(map[string]interface{}); ok {
				for _, k := range []string{"classification", "score", "profile", "state"} {
					row["acmg"+strings.ToUpper(k[:1])+k[1:]] = auto[k]
				}
			}
		} else {
			for _, k := range []string{"classification", "score", "profile", "state", "vusSubclass"} {
				row["acmg"+strings.ToUpper(k[:1])+k[1:]] = snap[k]
			}
		}
		row["acmgAssessmentSource"] = snap["source"]
		return
	}

	version := stringAdjustment(payload, "activeAcmgVersion")
	if version == "" {
		version = "legacy"
	}
	row["activeAcmgVersion"] = version
	if version == "svcv4" {
		result := svcResult(payload)
		row["acmgClassification"], row["acmgScore"] = result["classification"], result["score"]
		row["acmgVusSubclass"] = result["vusSubclass"]
		row["acmgProfile"], row["acmgAssessmentSource"] = "svcv4-draft-reference", "svcv4_reference"
	}
}
