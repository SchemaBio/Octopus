package handler

import (
	"encoding/json"
	"github.com/SchemaBio/Octopus/internal/model"
	"strings"
)

// Explicit clinical-field allowlist: no sample IDs, names, file links, project
// metadata or credentials. The current user must already have sample access.
func acmgSampleContext(sample *model.Sample) map[string]any {
	diagnosis := model.ClinicalDiagnosisInfo{}
	if json.Unmarshal([]byte(sample.ClinicalDiagnosis), &diagnosis) != nil {
		diagnosis.MainDiagnosis = sample.GetClinicalDiagnosis()
	}
	terms := sample.GetHPOTerms()
	terms = append(terms, diagnosis.HPOTerms...)
	context := map[string]any{
		"gender": sample.Gender, "age": sample.Age, "sample_type": sample.SampleType,
		"clinical_diagnosis": diagnosis, "hpo_terms": terms, "family_history": sample.GetFamilyHistory(),
		"father_affected_status": sample.FatherAffectedStatus, "mother_affected_status": sample.MotherAffectedStatus,
	}
	// Also remove known identifiers if they were copied into narrative fields.
	raw, _ := json.Marshal(context)
	for _, id := range []string{sample.UUID, sample.InternalID, sample.FatherSampleInternalID, sample.MotherSampleInternalID} {
		if len(id) >= 3 {
			encoded, _ := json.Marshal(id)
			raw = []byte(strings.ReplaceAll(string(raw), string(encoded[1:len(encoded)-1]), "[identifier omitted]"))
		}
	}
	_ = json.Unmarshal(raw, &context)
	return context
}

// Use the executed task design, never the current sample's parent links.
func acmgTaskSampleContext(sample *model.Sample, task *model.Task) map[string]any {
	out := acmgSampleContext(sample)
	mode := "unknown"
	if task != nil {
		switch task.Template {
		case "single", "trio":
			mode = task.Template
		}
	}
	out["analysis_mode"] = mode
	if mode == "trio" {
		for _, member := range task.ToResponse().TrioMembers {
			if member.Role == "father" || member.Role == "mother" {
				out[member.Role+"_affected_status"] = member.AffectedStatus
			}
		}
	}
	return out
}
