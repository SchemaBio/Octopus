package resultengine

import "testing"

func TestAdoptedSnapshotWinsOverNewSavedCalculator(t *testing.T) {
	p := projected{overlay: map[string]interface{}{"acmgClassification": "Pathogenic", "adoptedACMG": map[string]interface{}{"version": "legacy", "classification": "VUS", "score": 2}}}
	if p.field("acmgClassification") != "VUS" || p.field("acmgScore") != "2" {
		t.Fatal(p.field("acmgClassification"), p.field("acmgScore"))
	}
	p.overlay["adoptedACMG"] = map[string]interface{}{"automatic": true, "version": "legacy"}
	p.automatic = map[string]interface{}{"classification": "Pathogenic"}
	if p.field("acmgClassification") != "Pathogenic" {
		t.Fatal("automatic reference lost")
	}
}
