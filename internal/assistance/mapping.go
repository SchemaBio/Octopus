package assistance

import (
	"errors"
	"fmt"
	"math"
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

// BuildPayload exposes only SepiLens AI execution. Local evaluation is never
// submitted, and an AI failure must never be replaced with its local baseline.
func BuildPayload(in Input, profile string) (string, map[string]any, error) {
	if in.Feature == "hpo" {
		if strings.TrimSpace(in.Diagnosis) == "" || profile == "" {
			return "", nil, errors.New("clinical text and provider profile required")
		}
		return "/v2/hpo/extract", map[string]any{"text": in.Diagnosis, "options": map[string]any{"provider_profile_id": profile, "allow_local_fallback": false}}, nil
	}
	eng, ok := Engines[in.Feature]
	if !ok {
		return "", nil, errors.New("unknown AI feature")
	}
	if profile == "" {
		return "", nil, errors.New("AI provider profile required")
	}
	c, v := in.ClinicalContext, in.Variant
	isCNV := in.Feature == "cnv_gain" || in.Feature == "cnv_loss"
	autoContext := in.Feature == "acmg" || in.Feature == "svcv4" || isCNV
	if autoContext {
		c = Context{Inheritance: "unknown", SampleType: "unknown", VariantOrigin: "unknown"}
	}
	if !autoContext && (c.DiseaseID == "" || c.Inheritance == "") {
		return "", nil, errors.New("disease and inheritance required")
	}
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
	ctx := map[string]any{"disease_id": c.DiseaseID, "inheritance": c.Inheritance, "sample_type": c.SampleType, "variant_origin": c.VariantOrigin}
	// HPO IDs are context supplied by the user, never verified PP4 evidence.
	if len(c.HPOTerms) > 0 {
		ctx["hpo_terms"] = c.HPOTerms
	}
	opts := map[string]any{"provider_profile_id": profile}
	pkg := map[string]any{"sources": []any{}}
	if autoContext {
		pkg["ai_context"] = map[string]any{"variant_annotations": acmgVariantAnnotations(v), "sample": in.SampleContext}
	}
	body := map[string]any{"schema_version": "sepilens-evaluation-2.0", "mode": "ai_assisted", "engine_id": eng, "options": opts, "clinical_context": ctx, "evidence_package": pkg}
	if !isCNV {
		variant := map[string]any{"assembly": asm, "chrom": chrom, "pos": number(v["position"]), "ref": strings.ToUpper(text(v["ref"])), "alt": strings.ToUpper(text(v["alt"])), "gene": text(v["gene"])}
		if autoContext && !canonicalAIGene.MatchString(text(v["gene"])) {
			variant["gene"] = ""
		}
		for from, to := range map[string]string{"transcript": "transcript", "hgvsc": "hgvs_c", "hgvsp": "hgvs_p", "consequence": "consequence"} {
			if value := text(v[from]); value != "" {
				if autoContext && (len(value) > 512 || strings.ContainsAny(value, " \t\r\n\"<>{}")) {
					continue
				}
				variant[to] = value
			}
		}
		body["variant"] = variant
		// Overall AF must not be relabelled as population maximum AF or FAF95.
		if af, ok := v["gnomadAF"].(float64); ok {
			variant["allele_frequency"] = af
		}
		if in.Feature == "svcv4" {
			if in.Mode != "experimental" {
				return "", nil, errors.New("SVC draft requires experimental mode")
			}
			// Imported clinical context is unverified. The browser draft is not a
			// verified method package and cannot establish MOI or germline origin.
			// AI organizes evidence; the manual draft calculator remains separate.
			opts["allow_experimental"] = true
		}
	} else {
		if typ := text(v["type"]); typ != "" && ((in.Feature == "cnv_loss" && typ != "Deletion") || (in.Feature == "cnv_gain" && typ != "Amplification")) {
			return "", nil, errors.New("CNV event and engine mismatch")
		}
		typ := "gain"
		if in.Feature == "cnv_loss" {
			typ = "loss"
		}
		start, end := number(v["startPosition"]), number(v["endPosition"])
		if math.IsNaN(start) || math.IsInf(start, 0) || math.IsNaN(end) || math.IsInf(end, 0) || start != math.Trunc(start) || end != math.Trunc(end) || start < 0 || end < start || end > 9007199254740991 {
			return "", nil, errors.New("invalid CNV interval")
		}
		switch text(v["coordinateSystem"]) {
		case "0-based-half-open", "cnr-bed-zero-based-half-open":
			if end <= start {
				return "", nil, errors.New("empty CNV interval")
			}
			start++
		case "1-based-inclusive":
			if start < 1 {
				return "", nil, errors.New("invalid CNV start")
			}
		default:
			return "", nil, errors.New("explicit CNV coordinate system required")
		}
		cnv := map[string]any{"assembly": asm, "chrom": chrom, "start": start, "end": end, "coordinate_system": "1-based-inclusive", "type": typ}
		if n, ok := v["copyNumber"].(float64); ok {
			if n < 0 || n > 9007199254740991 || math.IsNaN(n) || math.IsInf(n, 0) {
				return "", nil, errors.New("invalid CNV copy number")
			}
			if n == math.Trunc(n) {
				cnv["copy_number"] = int(n)
			} else {
				// Depth-based reports contain continuous CN estimates. Never
				// round them into a discrete or clinically confirmed copy count.
				imported := pkg["ai_context"].(map[string]any)
				imported["variant_annotations"].(map[string]any)["copyNumberEstimate"] = n
			}
		}
		body["cnv"] = cnv
	}
	// Browser annotation values have no verified dataset version or record identity.
	// Preserve that distinction rather than inventing trusted sources or criteria.
	annotations := map[string]any{"source": "caller_supplied"}
	record := func() map[string]any {
		return map[string]any{"source": "caller_supplied", "status": "caller_supplied", "version": "unverified", "assembly": asm}
	}
	if value := text(v["clinvarSignificance"]); value != "" {
		r := record()
		r["classification"] = value
		r["review_status"] = text(v["clinvarReviewStatus"])
		r["variation_id"] = text(v["clinvarId"])
		annotations["clinvar"] = r
	}
	if value, ok := v["gnomadPopmaxAF"].(float64); ok {
		r := record()
		r["popmax_af"] = value
		annotations["gnomad"] = r
	}
	if value, ok := v["alphaMissenseScore"].(float64); ok {
		r := record()
		r["score"] = value
		annotations["alphamissense"] = r
	}
	if len(annotations) > 1 {
		pkg["annotations"] = annotations
	}
	if in.IncludeFactPack {
		attachImportedFactPack(body, pkg)
	}
	return "/v2/evaluations", body, nil
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
	return normalizeResult(feature, model, raw, false)
}

func normalizeResult(feature, model string, raw map[string]any, paidDelivery bool) (Result, bool, error) {
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
		if _, model := proposal["ai_assessment"].(map[string]any); !model {
			return r, false, errors.New("missing AI proposal")
		}
		r.ExperimentalResult = experimental
		r.ResultKind = "experimental"
		r.Warnings = []string{"输入未经核验；禁止临床用途和人工审核采纳。"}
	}
	if ai, ok := proposal["ai_assessment"].(map[string]any); ok {
		if feature == "svcv4" {
			r.SVCContext = ai["svc_context"]
		}
		if text(raw["schema_version"]) != "sepilens-evaluation-2.0" || text(raw["failure_code"]) != "" {
			return r, false, errors.New("invalid v2 AI result")
		}
		r.Summary = "AI 注释证据整理完成，请逐条复核。"
		if summary := text(ai["clinical_summary"]); summary != "" {
			r.Summary = summary
		}
		r.SuggestedClassification = text(ai["classification"])
		if warnings, ok := proposal["normalization_warnings"].([]any); ok {
			for _, warning := range warnings {
				if value := text(warning); value != "" {
					r.Warnings = append(r.Warnings, value)
				}
			}
		}
		if text(proposal["retrieval_status"]) == "unavailable" {
			r.Warnings = append(r.Warnings, "外部文献检索不可用，以下建议未完成文献核验；不可将检索失败理解为没有相关证据。")
		}
		if criteria, ok := ai["criteria"].([]any); ok {
			for _, item := range criteria {
				if c, ok := item.(map[string]any); ok {
					code := text(c["code"])
					reason := text(c["reason"])
					if reason == "" {
						reason = text(c["override_reason"])
					}
					if code != "" {
						r.Evidence = append(r.Evidence, map[string]string{"code": code, "reason": reason, "status": text(c["status"]), "strength": text(c["strength"])})
					}
				}
			}
		}
		if result, ok := r.RuleEngineResult.(map[string]any); ok {
			r.Classification = text(result["classification"])
			if missing, ok := result["missing_information"].([]any); ok {
				for _, item := range missing {
					if value := text(item); value != "" {
						r.Warnings = append(r.Warnings, value)
					}
				}
			}
		}
		if r.ResultKind == "experimental" {
			r.RuleEngineResult = nil
			r.Classification = ""
			eligible, _ := raw["settlement_eligible"].(bool)
			return r, eligible && paidDelivery && feature == "svcv4", nil
		}
		r.Warnings = append(r.Warnings, "模型建议、规则结果与人工判定分别保存；本结果尚未由人工采纳。")
		eligible, _ := raw["settlement_eligible"].(bool)
		return r, eligible && suggestionBillingPolicy(r.ResultKind, paidDelivery && (feature == "acmg" || feature == "cnv_gain" || feature == "cnv_loss")), nil
	}
	if text(raw["schema_version"]) == "sepilens-evaluation-2.0" {
		return r, false, errors.New("missing v2 AI proposal")
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
	// Commercial AI evidence billing is explicitly snapshotted per ACMG request.
	eligible, _ := raw["settlement_eligible"].(bool)
	return r, valid && eligible && suggestionBillingPolicy(r.ResultKind, paidDelivery && (feature == "acmg" || feature == "cnv_gain" || feature == "cnv_loss")), nil
}

func suggestionBillingPolicy(kind string, evidenceOnlyEnabled bool) bool {
	return kind == "production" || (evidenceOnlyEnabled && kind == "evidence_only")
}
