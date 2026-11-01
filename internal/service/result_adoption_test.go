package service

import "testing"

func TestSaveOnlyPreservesAdoptedResultWhileCalculatorChanges(t *testing.T) {
	p := map[string]interface{}{"acmgEvidence": []interface{}{}, "acmgClassification": "VUS", "acmgScore": 2, "acmgSavedAt": "old"}
	preserveAdoptedACMG(p, map[string]interface{}{"acmgEvidence": []interface{}{}})
	p["acmgClassification"] = "Pathogenic"
	p["acmgSavedAt"] = "new"
	row := map[string]interface{}{}
	applyActiveACMG(row, p)
	if row["acmgClassification"] != "VUS" {
		t.Fatal("save-only replaced adopted result", row)
	}
	p["adoptedACMG"] = snapshotAdoptedACMG(p)
	applyActiveACMG(row, p)
	if row["acmgClassification"] != "Pathogenic" {
		t.Fatal("explicit adoption failed", row)
	}
}
func TestSaveOnlyPreservesAutomaticReferenceAndSVCAdoption(t *testing.T) {
	p := map[string]interface{}{}
	preserveAdoptedACMG(p, map[string]interface{}{"acmgEvidence": []interface{}{}})
	p["acmgClassification"] = "VUS"
	row := map[string]interface{}{"automaticAcmg": map[string]interface{}{"classification": "Pathogenic"}}
	applyActiveACMG(row, p)
	if row["acmgClassification"] != "Pathogenic" {
		t.Fatal(row)
	}
	p = map[string]interface{}{"activeAcmgVersion": "svcv4", "svcv4Assessment": map[string]interface{}{"confirmed": true, "revision": SVCv4Revision, "result": map[string]interface{}{"state": "classified", "classification": "VUS", "score": 3}}}
	preserveAdoptedACMG(p, map[string]interface{}{"svcv4Assessment": true})
	p["svcv4Assessment"] = map[string]interface{}{"confirmed": false}
	if err := validateActiveACMG(p); err != nil {
		t.Fatal("saved partial draft invalidated adopted snapshot", err)
	}
	applyActiveACMG(row, p)
	if row["acmgClassification"] != "VUS" {
		t.Fatal(row)
	}
}
