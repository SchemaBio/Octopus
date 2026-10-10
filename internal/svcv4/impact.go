package svcv4

import (
	"encoding/json"
	"fmt"
)

type branch struct {
	Code          string  `json:"parent_code"`
	PRDLo         float64 `json:"prd_lo"`
	PRDHi         float64 `json:"prd_hi"`
	HeldHi        float64 `json:"held_hi"`
	ParentLo      float64 `json:"parent_lo"`
	ParentHi      float64 `json:"parent_hi"`
	INFLo         float64 `json:"inf_lo"`
	INFHi         float64 `json:"inf_hi"`
	MechanismOnly bool    `json:"sm18_mechanism_only"`
	FXNNA         bool    `json:"fxn_na"`
	PRDSPALo      float64 `json:"prd_spa_lo"`
	PRDSPAHi      float64 `json:"prd_spa_hi"`
	PRDSPAFXNHi   float64 `json:"prd_spa_fxn_hi"`
}

var branchTables = func() map[string]map[string]branch {
	var v map[string]map[string]branch
	if err := json.Unmarshal(branchBytes, &v); err != nil {
		panic(err)
	}
	return v
}()

const oddityNote = "SM 6 blue/violet SPL_ parent caps are inverted vs SM 11/12 (blue -8..0, violet -8..+8); encoded as documented -- suspected SM 6 inconsistency, flagged for WG review."

func Impact(workflow string, a object, gdv string) (*ScoreResult, *MissenseResult) {
	if workflow == "missense" {
		amino, splice := aminoAcid(obj(a["amino_acid"])), spliceImpact("missense_splice", obj(a["splice"]), gdv)
		selected, code, total := "AMINO_ACID", "MIS", amino.Total
		if splice.Total != nil && *splice.Total > 0 && (amino.Total == nil || *splice.Total > *amino.Total) {
			selected, code, total = "SPLICE", "SPL", splice.Total
		}
		return nil, &MissenseResult{Amino: amino, Splice: splice, Selected: selected, Code: code, Total: total, Provenance: []string{fmt.Sprintf("compared MIS_ %s vs SPL_ %s -> %s (SM 6 take-higher: negative/absent splice or a positive tie -> amino-acid)", py(amino.Total), py(splice.Total), selected)}}
	}
	if workflow == "missense_amino_acid" {
		return aminoAcid(a), nil
	}
	if workflow == "canonical_splice" || workflow == "intronic_synonymous" || workflow == "missense_splice" {
		return spliceImpact(workflow, a, gdv), nil
	}
	if workflow == "exon_duplication" && a["prediction_outcome"] == "WHOLE_GENE_NA" {
		r := score("CDS")
		r.Provenance = []string{"WHOLE_GENE_NA: CDS_NA (evaluated, determined not applicable)"}
		return r, nil
	}
	b, known := branchTables[workflow][str(a["prediction_outcome"])]
	if !known {
		b = branch{PRDLo: 0, PRDHi: 0, HeldHi: 9, ParentLo: -8, ParentHi: 10, INFLo: -8, INFHi: 8}
	}
	r := score(b.Code)
	initial := value(get(a, "predictive", "initial_points"))
	mer := obj(a["mechanism_exon_relevance"])
	mech, exon := str(mer["gencc_mechanism"]), str(mer["exon_relevance"])
	var prd *float64
	if initial == nil || !known {
		r.Provenance = append(r.Provenance, "PRD: _ND (no initial points and/or unknown branch)")
	} else {
		adj := SM18(initial, mech, exon, gdv, b.MechanismOnly)
		prd = Cap(adj, b.PRDLo, b.PRDHi)
		r.Add("PRD", prd)
		r.Provenance = append(r.Provenance, fmt.Sprintf("PRD: initial %s x SM18(mech=%s, exon=%s, gdv=%s) = %s, capped [%s, %s] -> %s", py(initial), optional(mech), optional(exon), optional(gdv), py(adj), pyFloat(b.PRDLo), pyFloat(b.PRDHi), py(prd)))
	}
	held := prd
	if b.FXNNA {
		r.Provenance = append(r.Provenance, "FXN: NA (functional not considered on this gain path)")
	} else {
		fxn := value(a["fxn_points"])
		if fxn == nil {
			r.Provenance = append(r.Provenance, "FXN: _ND (no coded fxn_points captured; OddsPath not recomputed)")
		} else {
			r.Add("FXN", fxn)
			r.Provenance = append(r.Provenance, "FXN: consumed coded value "+py(fxn))
		}
		held = Hold(-8, b.HeldHi, prd, fxn)
		if held != nil {
			r.Held.Set("PRD+FXN", *held)
			r.Provenance = append(r.Provenance, fmt.Sprintf("held PRD+FXN: %s (cap [-8.0, %s])", py(held), pyFloat(b.HeldHi)))
		}
	}
	inf := Cap(informative(list(get(a, "informative", "variants")), false), b.INFLo, b.INFHi)
	if inf == nil {
		r.Provenance = append(r.Provenance, "INF: _ND (no classified informative variants)")
	} else {
		r.Add("INF", inf)
		r.Provenance = append(r.Provenance, fmt.Sprintf("INF: %s (cap [%s, %s])", py(inf), pyFloat(b.INFLo), pyFloat(b.INFHi)))
	}
	r.Total = Hold(b.ParentLo, b.ParentHi, held, inf)
	if r.Total != nil {
		r.Provenance = append(r.Provenance, fmt.Sprintf("parent_total: %s (cap [%s, %s])", py(r.Total), pyFloat(b.ParentLo), pyFloat(b.ParentHi)))
	}
	captured := str(a["parent_code"])
	if captured != "" && b.Code != "" && captured != b.Code {
		r.Provenance = append(r.Provenance, fmt.Sprintf("NOTE: captured parent_code %s != branch-derived %s", captured, b.Code))
	}
	return r, nil
}
func optional(s string) string {
	if s == "" {
		return "None"
	}
	return s
}
func spliceImpact(workflow string, a object, gdv string) *ScoreResult {
	b, known := branchTables[workflow][str(a["prediction_outcome"])]
	if !known {
		b = branch{PRDSPALo: -8, PRDSPAHi: 10, PRDSPAFXNHi: 9, INFLo: -8, INFHi: 8, ParentLo: -8, ParentHi: 10}
	}
	r := score("SPL")
	initial := value(get(a, "predictive", "initial_points"))
	var prd *float64
	if initial == nil || !known {
		r.Provenance = append(r.Provenance, "SPL_PRD: _ND (no initial points and/or unknown path)")
	} else {
		adj := SM18(initial, str(get(a, "mechanism_exon_relevance", "gencc_mechanism")), str(get(a, "mechanism_exon_relevance", "exon_relevance")), gdv, false)
		prd = Cap(adj, b.PRDLo, b.PRDHi)
		r.Add("PRD", prd)
		r.Provenance = append(r.Provenance, fmt.Sprintf("SPL_PRD: %s x SM18 = %s, capped [%s, %s] -> %s", py(initial), py(adj), pyFloat(b.PRDLo), pyFloat(b.PRDHi), py(prd)))
	}
	spa := value(a["spa_points"])
	if spa == nil {
		r.Provenance = append(r.Provenance, "SPL_SPA: _ND (no coded spa_points)")
	} else {
		r.Add("SPA", spa)
		r.Provenance = append(r.Provenance, "SPL_SPA: consumed coded value "+py(spa))
	}
	heldSPA := Hold(b.PRDSPALo, b.PRDSPAHi, prd, spa)
	if heldSPA != nil {
		r.Held.Set("PRD+SPA", *heldSPA)
		r.Provenance = append(r.Provenance, fmt.Sprintf("held PRD+SPA: %s (cap [%s, %s])", py(heldSPA), pyFloat(b.PRDSPALo), pyFloat(b.PRDSPAHi)))
	}
	fxn := value(a["fxn_points"])
	if fxn == nil {
		r.Provenance = append(r.Provenance, "SPL_FXN: _ND (no coded fxn_points; OddsPath not recomputed)")
	} else {
		r.Add("FXN", fxn)
		r.Provenance = append(r.Provenance, "SPL_FXN: consumed coded value "+py(fxn))
	}
	held := Hold(-8, b.PRDSPAFXNHi, heldSPA, fxn)
	if held != nil {
		r.Held.Set("PRD+SPA+FXN", *held)
		r.Provenance = append(r.Provenance, fmt.Sprintf("held PRD+SPA+FXN: %s (cap [-8.0, %s])", py(held), pyFloat(b.PRDSPAFXNHi)))
	}
	inf := Cap(informative(list(get(a, "informative", "variants")), false), b.INFLo, b.INFHi)
	if inf == nil {
		r.Provenance = append(r.Provenance, "SPL_INF: _ND (no classified informative variants)")
	} else {
		r.Add("INF", inf)
		r.Provenance = append(r.Provenance, fmt.Sprintf("SPL_INF: %s (cap [%s, %s])", py(inf), pyFloat(b.INFLo), pyFloat(b.INFHi)))
	}
	r.Total = Hold(b.ParentLo, b.ParentHi, held, inf)
	if r.Total != nil {
		r.Provenance = append(r.Provenance, fmt.Sprintf("spl_total: %s (cap [%s, %s])", py(r.Total), pyFloat(b.ParentLo), pyFloat(b.ParentHi)))
	}
	if workflow == "missense_splice" {
		r.Provenance = append(r.Provenance, oddityNote)
	}
	return r
}
func aminoAcid(a object) *ScoreResult {
	r := score("MIS")
	initial := value(get(a, "predictive", "initial_points"))
	var prd *float64
	if initial == nil {
		r.Provenance = append(r.Provenance, "MIS_PRD: _ND (no initial points)")
	} else {
		adj := Transcript(initial, str(get(a, "predictive", "transcript_relevance")))
		prd = Cap(adj, -4, 4)
		r.Add("PRD", prd)
		r.Provenance = append(r.Provenance, fmt.Sprintf("MIS_PRD: %s x transcript-relevance = %s, capped [-4.0, 4.0] -> %s", py(initial), py(adj), py(prd)))
	}
	fxn := value(a["fxn_points"])
	if fxn == nil {
		r.Provenance = append(r.Provenance, "MIS_FXN: _ND (no coded fxn_points; OddsPath not recomputed)")
	} else {
		r.Add("FXN", fxn)
		r.Provenance = append(r.Provenance, "MIS_FXN: consumed coded value "+py(fxn))
	}
	held := Hold(-8, 6, prd, fxn)
	if held != nil {
		r.Held.Set("PRD+FXN", *held)
		r.Provenance = append(r.Provenance, "held PRD+FXN: "+py(held)+" (cap [-8.0, 6.0])")
	}
	inf := Cap(informative(list(get(a, "informative", "variants")), true), -8, 8)
	if inf == nil {
		r.Provenance = append(r.Provenance, "MIS_INF: _ND (no categorized informative variants; SM7 motif deferred)")
	} else {
		r.Add("INF", inf)
		r.Provenance = append(r.Provenance, "MIS_INF: "+py(inf)+" (cap [-8.0, 8.0]); SM7 motif special-case deferred")
	}
	r.Total = Hold(-8, 9, held, inf)
	if r.Total != nil {
		r.Provenance = append(r.Provenance, "mis_total: "+py(r.Total)+" (cap [-8.0, 9.0])")
	}
	return r
}
