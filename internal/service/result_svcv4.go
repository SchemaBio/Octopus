package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/svcv4"
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
	result, err := svcv4.Evaluate(input)
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

func applyActiveACMG(row, payload map[string]interface{}) {
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
