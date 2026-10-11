package assistance

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func text(v any) string    { s, _ := v.(string); return strings.TrimSpace(s) }
func number(v any) float64 { n, _ := v.(float64); return n }
func assembly(s string) string {
	switch strings.ToLower(s) {
	case "hg19", "grch37":
		return "GRCh37"
	case "hg38", "grch38":
		return "GRCh38"
	}
	return ""
}
func BuildPayload(in Input, profile string) (string, map[string]any, error) {
	opts := map[string]any{"provider_profile_id": profile, "allow_local_fallback": false, "retrieval_mode": "targeted"}
	if in.Feature == "hpo" {
		if strings.TrimSpace(in.Diagnosis) == "" {
			return "", nil, errors.New("clinical diagnosis required")
		}
		return "/v1/hpo/extract", map[string]any{"text": in.Diagnosis, "options": opts}, nil
	}
	eng, ok := Engines[in.Feature]
	if !ok {
		return "", nil, errors.New("unknown AI feature")
	}
	v := in.Variant
	c := in.ClinicalContext
	if c.SampleType == "" {
		return "", nil, errors.New("sample type required")
	}
	if c.VariantOrigin == "" {
		c.VariantOrigin = "unknown"
	}
	if c.VariantOrigin != "unknown" && c.VariantOrigin != "germline_confirmed" {
		return "", nil, errors.New("unsupported variant origin")
	}
	asm := assembly(in.Reference)
	if asm == "" {
		return "", nil, errors.New("reference assembly required")
	}
	chrom := strings.TrimPrefix(text(v["chromosome"]), "chr")
	if chrom == "" {
		return "", nil, errors.New("chromosome required")
	}
	ctx := map[string]any{"disease_id": c.DiseaseID, "sample_type": c.SampleType, "variant_origin": c.VariantOrigin, "hpo_terms": c.HPOTerms}
	body := map[string]any{"engine_id": eng, "options": opts, "clinical_context": ctx}
	if in.Feature == "acmg" || in.Feature == "svcv4" {
		if c.DiseaseID == "" || c.Inheritance == "" {
			return "", nil, errors.New("disease and inheritance required")
		}
		ctx["inheritance"] = c.Inheritance
		gene := text(v["gene"])
		if gene == "" {
			return "", nil, errors.New("gene required")
		}
		variant := map[string]any{"assembly": asm, "chrom": chrom, "pos": number(v["position"]), "ref": strings.ToUpper(text(v["ref"])), "alt": strings.ToUpper(text(v["alt"])), "gene": gene}
		for from, to := range map[string]string{"transcript": "transcript", "hgvsc": "hgvs_c", "hgvsp": "hgvs_p", "consequence": "consequence"} {
			if s := text(v[from]); s != "" {
				variant[to] = s
			}
		}
		if af, ok := v["gnomadAF"].(float64); ok {
			variant["allele_frequency"] = af
		}
		body["variant"] = variant
		if in.Feature == "svcv4" {
			if in.Mode != "experimental" || c.VariantOrigin != "germline_confirmed" {
				return "", nil, errors.New("SVC draft requires experimental mode and explicitly confirmed germline origin")
			}
			var method map[string]any
			if json.Unmarshal(in.DraftAssessment, &method) != nil || method["upstream_commit"] == nil || method["moi"] != c.Inheritance {
				return "", nil, errors.New("SVC structured draft and matching inheritance required")
			}
			body["draft_assessment"] = method
			opts["allow_experimental"] = true
			opts["retrieval_mode"] = "off"
		}
	} else {
		typ := "gain"
		if in.Feature == "cnv_loss" {
			typ = "loss"
		}
		cnv := map[string]any{"assembly": asm, "chrom": chrom, "start": number(v["startPosition"]), "end": number(v["endPosition"]), "coordinate_system": "1-based-inclusive", "type": typ}
		if n := number(v["copyNumber"]); n > 0 {
			cnv["copy_number"] = int(n)
		}
		body["cnv"] = cnv
	}
	// Annotation and calculator material is explicitly unverified. It cannot
	// assert facts, provenance, applied scores or reviewer approval.
	allowed := map[string]bool{}
	for _, k := range strings.Fields("gene genes clinvarSignificance clinvarReviewStatus clinvarStars clinvarDisease gnomadAF gnomadEasAF alphaMissenseScore pangolinGain pangolinLoss evoScore diseaseAssociation inheritanceMode copyNumber copyRatio confidence exon") {
		allowed[k] = true
	}
	annotations := map[string]any{}
	for k, v := range v {
		if allowed[k] {
			annotations[k] = v
		}
	}
	var draft any
	if len(in.Calculator) > 0 {
		_ = json.Unmarshal(in.Calculator, &draft)
	}
	snippet, _ := json.Marshal(map[string]any{"annotations": annotations, "calculator_draft": sanitizeDraft(draft)})
	body["sources"] = []any{map[string]any{"id": "platform-draft", "type": "curated_snapshot", "title": "Platform annotation and unreviewed calculator draft", "snippet": string(snippet), "retrieved_at": time.Now().UTC().Format(time.RFC3339), "validation_status": "unverified", "identifier_valid": false, "record_exists": false, "object_matched": false, "material_verified": false, "full_text_available": false, "case_count": 0}}
	path := "/v1/variants/snv/evaluations"
	if strings.HasPrefix(in.Feature, "cnv_") {
		path = "/v1/variants/cnv/evaluations"
	}
	return path, body, nil
}
func sanitizeDraft(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, value := range x {
			switch k {
			case "evidence", "criteria", "code", "criterion", "ruleCode", "reason", "note", "score", "strength", "status", "selected", "diseaseId", "inheritance", "moi", "draft", "sections", "items", "totalScore", "classification", "override":
				out[k] = sanitizeDraft(value)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, a := range x {
			out = append(out, sanitizeDraft(a))
		}
		return out
	case string:
		if strings.Contains(x, "://") {
			return "[URL omitted]"
		}
		return x
	default:
		return x
	}
}
func NormalizeResult(feature, model string, raw map[string]any) (Result, bool, error) {
	r := Result{Feature: feature, Model: model, GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	if feature == "hpo" {
		degraded, _ := raw["degraded"].(bool)
		if degraded || text(raw["execution_mode"]) != "model" {
			return r, false, errors.New("degraded HPO result")
		}
		terms, _ := raw["terms"].([]any)
		for _, t := range terms {
			v, ok := t.(map[string]any)
			if !ok {
				continue
			}
			id := text(v["hpo"])
			if !strings.HasPrefix(id, "HP:") {
				continue
			}
			pol := text(v["polarity"])
			if pol == "present" {
				term := HPOTerm{ID: id, Name: text(v["name"]), Polarity: pol}
				start, a := v["span_start"].(float64)
				end, b := v["span_end"].(float64)
				if a && b && start >= 0 && end >= start && start == float64(int(start)) && end == float64(int(end)) {
					s, e := int(start), int(end)
					term.SpanStart = &s
					term.SpanEnd = &e
					term.SpanUnit = "unicode_codepoint"
				}
				r.HPOTerms = append(r.HPOTerms, term)
			} else {
				r.Warnings = append(r.Warnings, fmt.Sprintf("%s %s · %s，需人工复核", id, text(v["name"]), pol))
			}
		}
		r.Summary = "HPO 候选提取完成，请人工复核。"
		return r, len(r.HPOTerms) > 0 || len(r.Warnings) > 0, nil
	}
	if text(raw["delivery_status"]) != "delivered" {
		return r, false, errors.New("AI result not delivered")
	}
	r.ResultKind = text(raw["result_kind"])
	r.RulesetHash = text(raw["ruleset_hash"])
	r.DatasetHash = text(raw["dataset_hash"])
	r.AgentProposal = raw["agent_proposal"]
	r.RuleEngineResult = raw["rule_engine_result"]
	r.ReviewerDecision = raw["reviewer_decision"]
	proposal, _ := r.AgentProposal.(map[string]any)
	if experimental, ok := proposal["experimental_result"].(map[string]any); ok {
		r.ExperimentalResult = experimental
		r.ResultKind = "experimental"
		r.Summary = "SVC 锁定草案实验评分，仅供研究与工程对照。"
		r.Warnings = []string{"输入未经核验；禁止临床用途、人工审核采纳和积分结算。"}
		return r, false, nil
	}
	p := proposal
	hasModel := false
	if nested, ok := proposal["model"].(map[string]any); ok {
		p = nested
		hasModel = true
	}
	r.Summary = text(p["summary"])
	for _, container := range []map[string]any{p, proposal} {
		if items, ok := container["evidence"].([]any); ok {
			for _, item := range items {
				if v, ok := item.(map[string]any); ok {
					r.Evidence = append(r.Evidence, map[string]string{"code": text(v["rule_code"]), "reason": text(v["reason"])})
				}
			}
			break
		}
	}
	if missing, ok := p["missing_information"].([]any); ok {
		for _, v := range missing {
			if s := text(v); s != "" {
				r.Warnings = append(r.Warnings, s)
			}
		}
	}
	if engine, ok := r.RuleEngineResult.(map[string]any); ok {
		r.Classification = text(engine["classification"])
	}
	valid := hasModel && r.Summary != ""
	if candidates, ok := p["evidence"].([]any); ok && hasModel {
		for _, candidate := range candidates {
			if evidence, ok := candidate.(map[string]any); ok && text(evidence["rule_code"]) != "" && text(evidence["reason"]) != "" {
				valid = true
			}
		}
	}
	if strings.EqualFold(r.Classification, "insufficient_data") {
		valid = false
	}
	if r.Summary == "" {
		r.Summary = "AI 材料整理完成，请人工复核。"
	}
	r.Warnings = append(r.Warnings, "模型建议、规则结果与人工判定分别保存；本结果尚未由人工采纳。")
	// Evidence-only billing is reserved for a future settlement agreement.
	eligible, _ := raw["settlement_eligible"].(bool)
	return r, valid && eligible && suggestionBillingPolicy(r.ResultKind, false), nil
}

func suggestionBillingPolicy(kind string, evidenceOnlyEnabled bool) bool {
	return kind == "production" || (evidenceOnlyEnabled && kind == "evidence_only")
}
