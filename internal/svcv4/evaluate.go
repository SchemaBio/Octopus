package svcv4

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Evaluate ignores submitted derived fields and calculates only from raw input.
// No implementation-version tag is added to the public response: rule revision
// and existing stored assessments retain their established compatibility shape.
func Evaluate(request object) (object, error) {
	if err := finite(request); err != nil {
		return nil, err
	}
	if v := request["revision"]; v != nil && v != Revision {
		return nil, fmt.Errorf("草案规则版本不匹配，请保留旧记录并新建评定")
	}
	rawInputs, present := request["inputs"]
	if !present {
		rawInputs = object{}
	}
	inputs, ok := rawInputs.(map[string]interface{})
	encoded, _ := json.Marshal(inputs)
	if !ok || len(encoded) > 200000 {
		return nil, fmt.Errorf("新版证据输入无效或过大")
	}
	allowed := map[string]bool{"workflow": true, "impact": true, "population": true, "cases": true, "caseControl": true, "family": true}
	for key := range inputs {
		if !allowed[key] {
			return nil, fmt.Errorf("未知新版证据字段")
		}
	}
	disease, ok := request["disease"].(string)
	if !ok {
		if request["disease"] != nil {
			return nil, fmt.Errorf("疾病必须为文本")
		}
	}
	disease = strings.TrimSpace(disease)
	moi, gdv := str(request["moi"]), str(request["geneDiseaseValidity"])
	schema := schemaDocument
	validEnum := func(name, v string) bool {
		for _, item := range list(schema[name]) {
			if item == v {
				return true
			}
		}
		return false
	}
	if moi != "" && !validEnum("moi", moi) {
		return nil, fmt.Errorf("invalid mode of inheritance")
	}
	if gdv != "" && !validEnum("geneDiseaseValidity", gdv) {
		return nil, fmt.Errorf("invalid gene disease validity")
	}
	if disease == "" || utf8.RuneCountInString(disease) > 500 || moi == "" {
		return nil, fmt.Errorf("请确认疾病及遗传模式")
	}
	warnings := []string{"草案／非权威参考实现；ClinGen CSpec 为权威计分来源。", "上游尚未完整强制执行病例适用性及基因疾病有效性规则，请人工核验。"}
	details := object{}
	families := []familyTotal{}
	add := func(r *ScoreResult) { families = append(families, familyTotal{r.ParentCode, r.Total}) }
	provenance := func(r *ScoreResult) { warnings = append(warnings, r.Provenance...) }
	workflow := str(inputs["workflow"])
	if truthy(inputs["workflow"]) {
		impact := inputs["impact"]
		if !truthy(impact) {
			impact = object{}
		}
		a, err := validateModel(workflow, impact)
		if err != nil {
			return nil, err
		}
		r, missense := Impact(workflow, a, gdv)
		if missense != nil {
			families = append(families, familyTotal{&missense.Code, missense.Total})
			details["impact"] = missense
			provenance(missense.Amino)
			provenance(missense.Splice)
			warnings = append(warnings, missense.Provenance...)
		} else {
			add(r)
			details["impact"] = r
			provenance(r)
		}
	}
	population := inputs["population"]
	if !truthy(population) {
		population = object{}
	}
	p, err := validateModel("population", population)
	if err != nil {
		return nil, err
	}
	if faf := value(p["faf"]); faf != nil && (*faf < 0 || *faf > 1) {
		return nil, fmt.Errorf("FAF 必须在0到1之间")
	}
	if daft := value(p["daft"]); daft != nil && (*daft <= 0 || *daft > 1) {
		return nil, fmt.Errorf("DAFT 必须大于0且不超过1")
	}
	for _, key := range []string{"homozygote_count", "hemizygote_count"} {
		if count := value(p[key]); count != nil && *count < 0 {
			return nil, fmt.Errorf("出现次数不能为负数")
		}
	}
	pop := Population(p, moi)
	popTotal, err := Aggregate([]*ScoreResult{pop}, "POP", nil)
	if err != nil {
		return nil, err
	}
	add(popTotal)
	details["population"] = pop
	provenance(pop)
	cases := inputs["cases"]
	if !truthy(cases) {
		cases = []interface{}{}
	}
	caseList, ok := cases.([]interface{})
	if !ok || len(caseList) > 100 {
		return nil, fmt.Errorf("最多录入100个独立病例")
	}
	ids, familyIDs := map[string]bool{}, map[string]bool{}
	clinical := []*ScoreResult{}
	for _, item := range caseList {
		c, err := validateModel("case", item)
		if err != nil {
			return nil, err
		}
		id, family := str(c["id"]), str(c["family_id"])
		if id == "" || family == "" || ids[id] || familyIDs[family] {
			return nil, fmt.Errorf("临床病例须填写唯一病例和家系编号，每个无关家系仅一个先证者")
		}
		ids[id], familyIDs[family] = true, true
		clinical = append(clinical, ClinicalProband(c, moi))
	}
	var ccs *ScoreResult
	if truthy(inputs["caseControl"]) {
		c, err := validateModel("caseControl", inputs["caseControl"])
		if err != nil {
			return nil, err
		}
		ccs = CaseControl(c)
	}
	var frq *float64
	if n, ok := pop.Sub.Values["POP_FRQ"]; ok {
		frq = num(n)
	}
	cln := FinalizeClinical(AggregateClinical(clinical), ccs, frq)
	add(cln)
	details["cases"], details["clinical"] = clinical, cln
	for _, c := range clinical {
		provenance(c)
	}
	provenance(cln)
	if ccs != nil {
		details["caseControl"] = ccs
		provenance(ccs)
	}
	if truthy(inputs["family"]) {
		c, err := validateModel("case", inputs["family"])
		if err != nil {
			return nil, err
		}
		phe, seg := LocusPhenotype(c, moi), LocusSegregation(c, moi)
		loc, err := Aggregate([]*ScoreResult{phe, seg}, "LOC", num(4))
		if err != nil {
			return nil, err
		}
		add(loc)
		details["family"] = object{"phenotype": phe, "segregation": seg, "subtotal": loc}
		provenance(phe)
		provenance(seg)
		provenance(loc)
	}
	combined, err := Combine(families)
	if err != nil {
		return nil, err
	}
	provenance(combined)
	var category interface{}
	var vus *string
	state := "insufficient_evidence"
	if combined.Total != nil {
		c, s := Classify(*combined.Total)
		vus = s
		category = map[string]string{"PATHOGENIC": "Pathogenic", "LIKELY_PATHOGENIC": "Likely_Pathogenic", "VUS": "VUS", "LIKELY_BENIGN": "Likely_Benign", "BENIGN": "Benign"}[c]
		state = "classified"
	}
	unique := []string{}
	seen := map[string]bool{}
	for _, w := range warnings {
		if !seen[w] {
			unique = append(unique, w)
			seen[w] = true
		}
	}
	var gdvValue interface{}
	if gdv != "" {
		gdvValue = gdv
	}
	return object{"disease": disease, "moi": moi, "geneDiseaseValidity": gdvValue, "inputs": inputs, "revision": Revision, "source": Source, "authoritative": false, "confirmed": request["confirmed"] == true, "result": object{"score": combined.Total, "classification": category, "vusSubclass": vus, "state": state, "breakdown": combined.Sub, "details": details, "warnings": unique}}, nil
}
