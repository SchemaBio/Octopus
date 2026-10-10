package svcv4

import (
	"fmt"
	"math"
)

func Population(e object, moi string) *ScoreResult {
	r := score("POP")
	r.Provenance = []string{`POP: "POP" is a grouping label (POP_FRQ/POP_HMZ are independent case-level codes), not an SVCv4 parent code; parent_total is a convenience subtotal (no SM 3 combined cap).`}
	faf, daft := value(e["faf"]), value(e["daft"])
	if faf == nil || daft == nil || *daft <= 0 {
		r.Provenance = append(r.Provenance, "POP_FRQ: _ND (no FAF and/or DAFT; absent-in-db is faf=0.0 -> 0.0, not None)")
	} else {
		fold := *faf / *daft
		n := -6.0
		switch {
		case fold < 1.5:
			n = 0
		case fold < 5:
			n = -1
		case fold < 15:
			n = -3
		}
		r.Sub.Set("POP_FRQ", n)
		r.Provenance = append(r.Provenance, fmt.Sprintf("POP_FRQ: FAF %s / DAFT %s = %.3gx -> %s (bands <1.5x/5x/15x, lower edge inclusive -- SM 3 boundary assumption)", py(faf), py(daft), fold, pyFloat(n)))
	}
	homo, hemi := value(e["homozygote_count"]), value(e["hemizygote_count"])
	if moi != "XLD" && moi != "XLR" {
		hemi = nil
	}
	weight := -.5
	if moi == "AD" {
		weight = -1
	}
	if e["hmz_eligible"] != "TRUE" || (homo == nil && hemi == nil) {
		r.Provenance = append(r.Provenance, "POP_HMZ: _ND (not hmz_eligible, or no homozygote/hemizygote count)")
	} else {
		count := 0.0
		if homo != nil {
			count += *homo
		}
		if hemi != nil {
			count += *hemi
		}
		n := weight * math.Max(count-1, 0)
		r.Sub.Set("POP_HMZ", n)
		r.Provenance = append(r.Provenance, fmt.Sprintf("POP_HMZ: %s (weight %s/obs from the 2nd -- SM 3 Table 7; AD -1.0 vs prose -0.5 conflict, encoded to Table 7)", pyFloat(n), pyFloat(weight)))
	}
	r.Subtotal()
	if r.Total != nil {
		r.Provenance = append(r.Provenance, "POP total: "+py(r.Total)+" (POP_FRQ + POP_HMZ; no SM 3 combined cap)")
	}
	return r
}
