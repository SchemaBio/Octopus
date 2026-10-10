package resultengine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const ACMGProfile = "acmg-snv-points-v2"
const FieldProfile = "parquet-fields-v2"

var overlayFields = strings.Fields("activeAcmgVersion svcv4Assessment acmgVusSubclass acmgTrial reviewed reported interpretation acmgClassification acmgEvidence cnvAssessment cnvClassification cnvScore acmgScore acmgProfile acmgState acmgOverride acmgOverrideReason")
var numericFields = strings.Fields("acmgScore cnvScore Position Start End Quality Depth VAF GnomAD_AF GnomAD_AF_EAS GnomAD_nhomalt_XX GnomAD_nhomalt_XY Pangolin_Gain Pangolin_Loss EVOScore AlphaMissense_AM copy_number score size Repeat_Count RepeatCount Heteroplasmy Heteroplasmy_Level NbVariants Percentage_Homozygosity Average_Depth Log2_Ratio Copy_Ratio Start_Position End_Position")
var numericName = regexp.MustCompile(`(?i)(^|_)(af|vaf|depth|score|count|size|start|end|position|ratio|length|fraction|percent|heteroplasmy)(_|$)`)
var transcriptName = regexp.MustCompile(`^ENST[0-9]+(?:\.[0-9]+)?$`)

func RowID(dataset, hash string, ordinal int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", dataset, hash, ordinal)))
	return hex.EncodeToString(h[:])
}
func text(v interface{}) string {
	if v == nil {
		return "None"
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}
func jsonText(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch v.(type) {
	case map[string]interface{}, []interface{}, []map[string]interface{}:
		b, _ := json.Marshal(v)
		return string(b)
	}
	return text(v)
}
func numeric(v interface{}) (float64, bool) {
	if v == nil {
		return 0, false
	}
	n, e := strconv.ParseFloat(strings.TrimSpace(text(v)), 64)
	return n, e == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
func isNumeric(name string) bool {
	for _, v := range numericFields {
		if v == name {
			return true
		}
	}
	return numericName.MatchString(name)
}
func FieldType(name string) string {
	if name == "reviewed" || name == "reported" {
		return "boolean"
	}
	if name == "acmgClassification" {
		return "enum"
	}
	if isNumeric(name) {
		return "number"
	}
	return "text"
}

func AutomaticACMG(row map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{"profile": ACMGProfile, "state": "insufficient_evidence", "score": 0, "criteria": []interface{}{}}
	pending := func(s string) map[string]interface{} { result["pending"] = []string{s}; return result }
	kind := strings.ToLower(strings.TrimSpace(text(row["Type"])))
	if (kind != "snp" && kind != "snv") || !strings.Contains(strings.ToLower(text(row["Consequence"])), "missense_variant") {
		return pending("自动 PP3/BP4 仅适用于有明确错义后果的 SNP")
	}
	if !transcriptName.MatchString(strings.TrimSpace(text(row["Transcript"]))) {
		return pending("当前转录本缺失、多值或无法对应 AlphaMissense")
	}
	raw := row["AlphaMissense_AM"]
	if raw == nil || strings.Contains(text(raw), "&") {
		return pending("AlphaMissense 分值缺失或多值无法对应当前转录本")
	}
	score, err := strconv.ParseFloat(strings.TrimSpace(text(raw)), 64)
	if err != nil {
		return pending("AlphaMissense 分值不可解析")
	}
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
		return pending("AlphaMissense 分值超出有效范围")
	}
	points, code, strength := 0, "", ""
	switch {
	case score >= .990:
		points, code, strength = 4, "PP3", "strong"
	case score >= .906:
		points, code, strength = 2, "PP3", "moderate"
	case score >= .792:
		points, code, strength = 1, "PP3", "supporting"
	case score < .100:
		points, code, strength = -2, "BP4", "moderate"
	case score < .170:
		points, code, strength = -1, "BP4", "supporting"
	}
	classification := ""
	if points > 0 {
		classification = "VUS"
	}
	if points < 0 {
		classification = "Likely_Benign"
	}
	result["score"], result["classification"] = points, classification
	if code != "" {
		result["criteria"] = []interface{}{map[string]interface{}{"code": code, "strength": strength, "source": "AlphaMissense", "value": score}}
		result["state"] = "classified"
	}
	return pending("疾病机制、病例/家系及实验室证据未由本自动初评评估")
}

type projected struct {
	raw, overlay, automatic map[string]interface{}
	fields                  map[string]bool
	table                   string
}

func (p projected) field(name string) interface{} {
	active := p.overlay["activeAcmgVersion"]
	switch name {
	case "activeAcmgVersion":
		if active == nil {
			return "legacy"
		}
		return jsonText(active)
	case "acmgTrial":
		if active == "svcv4" {
			return "true"
		}
		return "false"
	case "acmgVusSubclass":
		if active == "svcv4" {
			return jsonText(nested(p.overlay, "svcv4Assessment", "result", "vusSubclass"))
		}
		return nil
	case "cnvClassification":
		return jsonText(nested(p.overlay, "cnvAssessment", "classification"))
	case "cnvScore":
		return jsonText(nested(p.overlay, "cnvAssessment", "totalScore"))
	case "reviewed", "reported":
		if p.overlay[name] == nil {
			return "false"
		}
		return jsonText(p.overlay[name])
	}
	acmg := map[string]string{"acmgClassification": "classification", "acmgScore": "score", "acmgProfile": "profile", "acmgState": "state", "acmgEvidence": "criteria"}
	if key, ok := acmg[name]; ok {
		if active == "svcv4" && name != "acmgEvidence" {
			if name == "acmgProfile" {
				return "svcv4-draft-reference"
			}
			return jsonText(nested(p.overlay, "svcv4Assessment", "result", key))
		}
		if name == "acmgClassification" {
			if override := p.overlay["acmgOverride"]; override != nil && override != "" {
				return jsonText(override)
			}
		}
		if _, ok := p.overlay["acmgEvidence"]; ok {
			v := p.overlay[name]
			if name == "acmgClassification" && v == "" {
				return nil
			}
			return jsonText(v)
		}
		// SQL's simplified automatic projection omits classification for unsupported
		// tables/missing columns; row payloads retain the complete pending reasons.
		if p.table != "snv-indel" || !p.fields["Type"] || !p.fields["Consequence"] || !p.fields["Transcript"] || !p.fields["AlphaMissense_AM"] {
			switch key {
			case "profile":
				return ACMGProfile
			case "state":
				return "insufficient_evidence"
			case "score":
				return "0"
			case "criteria":
				return "[]"
			}
			return nil
		}
		if v, ok := p.automatic[key]; ok {
			return jsonText(v)
		}
		if key == "classification" {
			return ""
		}
		return nil
	}
	for _, field := range overlayFields {
		if name == field {
			return jsonText(p.overlay[name])
		}
	}
	return p.raw[name]
}
func nested(m map[string]interface{}, keys ...string) interface{} {
	var v interface{} = m
	for _, k := range keys {
		next, ok := v.(map[string]interface{})
		if !ok {
			return nil
		}
		v = next[k]
	}
	return v
}
