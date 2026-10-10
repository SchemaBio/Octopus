package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const SVCv4Revision = "ef66faff51a265fef7b5c4e6439905f3aa540c46"

// Uses the existing internal Python service and its pinned, offline reference scorer.
func (s *ResultService) SVCv4(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	method, endpoint := http.MethodGet, "/v1/svcv4/schema"
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil || len(encoded) > 250000 {
			return nil, fmt.Errorf("SVCv4 evidence is invalid or too large")
		}
		method, endpoint, body = http.MethodPost, "/v1/svcv4/evaluate", bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.cfg.ResultQuery.ServiceURL, "/")+endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("SVCv4 reference service unavailable")
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid SVCv4 response")
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadRequest {
			return nil, fmt.Errorf("SVCv4: %s", stringAdjustment(result, "message"))
		}
		return nil, fmt.Errorf("SVCv4 reference service unavailable (status %d)", resp.StatusCode)
	}
	if stringAdjustment(result, "revision") != SVCv4Revision || result["authoritative"] != false {
		return nil, fmt.Errorf("SVCv4 reference version mismatch")
	}
	return result, nil
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
