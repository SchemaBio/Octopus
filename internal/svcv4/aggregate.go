package svcv4

import (
	"fmt"
	"math"
)

func Aggregate(results []*ScoreResult, family string, cap *float64) (*ScoreResult, error) {
	r := score(family)
	for _, v := range results {
		if v.Total != nil && math.Abs(*v.Total-v.Sub.Sum()) > 1e-9 {
			return nil, fmt.Errorf("%s: input parent_total %s != sum(sub_code_points) %s -- this family aggregator assumes they match.", family, py(v.Total), pyFloat(v.Sub.Sum()))
		}
		for _, k := range v.Sub.Keys {
			if _, ok := r.Sub.Values[k]; ok {
				return nil, fmt.Errorf("%s: duplicate sub-code %s across inputs.", family, repr(k))
			}
			r.Sub.Set(k, v.Sub.Values[k])
		}
	}
	r.Provenance = append(r.Provenance, fmt.Sprintf(`%s: "%s" is the HOD grouping label; family subtotal (reference).`, family, family))
	if len(r.Sub.Keys) == 0 {
		r.Provenance = append(r.Provenance, family+": _ND (no scored sub-codes)")
		return r, nil
	}
	raw := r.Sub.Sum()
	total := raw
	if cap != nil {
		total = math.Min(raw, *cap)
	}
	r.Total = num(total)
	if cap != nil && raw > *cap {
		r.Held.Set("raw_sum", raw)
		r.Provenance = append(r.Provenance, fmt.Sprintf("%s: subtotal capped %s -> %s (+%s cap): %s", family, pyFloat(raw), pyFloat(total), py(cap), r.Sub.Detail()))
	} else {
		r.Provenance = append(r.Provenance, fmt.Sprintf("%s: subtotal %s (%s; cap %s)", family, pyFloat(total), r.Sub.Detail(), py(cap)))
	}
	return r, nil
}
func AggregateClinical(results []*ScoreResult) *ScoreResult {
	r := score("CLN")
	for _, v := range results {
		for _, k := range v.Sub.Keys {
			r.Sub.Set(k, r.Sub.Values[k]+v.Sub.Values[k])
		}
	}
	r.Provenance = []string{fmt.Sprintf("CLN: cross-proband subtotal over %d proband(s) (reference); assumes unrelated index probands -- one per family (SM 4 L27); related individuals are LOC segregation, not CLN.", len(results))}
	if len(r.Sub.Keys) == 0 {
		r.Provenance = append(r.Provenance, "CLN: _ND (no CLN sub-code scored across probands)")
		return r
	}
	r.Subtotal()
	r.Provenance = append(r.Provenance, fmt.Sprintf("CLN: summed %s -> %s (no cross-proband cap, SM 4).", r.Sub.Detail(), py(r.Total)))
	return r
}
func FinalizeClinical(subtotal, ccs *ScoreResult, popFRQ *float64) *ScoreResult {
	r := score("CLN")
	r.Provenance = []string{"CLN: finalize (reference) -- CLN_CCS exclusivity + POP_FRQ gate (SM 4)."}
	merged := points()
	for _, k := range subtotal.Sub.Keys {
		merged.Set(k, subtotal.Sub.Values[k])
	}
	if ccs != nil {
		for _, k := range ccs.Sub.Keys {
			merged.Set(k, ccs.Sub.Values[k])
		}
	}
	na := map[string]bool{}
	if _, ok := merged.Values["CLN_CCS"]; ok {
		for _, k := range []string{"CLN_AFF", "CLN_ALT", "CLN_UAF"} {
			na[k] = true
		}
		r.Provenance = append(r.Provenance, "CLN: CLN_CCS applied -> NA CLN_AFF/CLN_ALT/CLN_UAF (keep CLN_CCS + CLN_DNV).")
	}
	if popFRQ == nil || (*popFRQ != 0 && *popFRQ != -1) {
		na["CLN_AFF"], na["CLN_DNV"] = true, true
		r.Provenance = append(r.Provenance, fmt.Sprintf("CLN: POP_FRQ gate -- pop_frq_points=%s not in {0.0, -1.0} -> NA CLN_AFF + CLN_DNV (SM 4 L27; DNV-gating is the faithful default, Fig 1 image-gap).", py(popFRQ)))
	}
	for _, k := range merged.Keys {
		if !na[k] {
			r.Sub.Set(k, merged.Values[k])
		}
	}
	if len(r.Sub.Keys) == 0 {
		r.Provenance = append(r.Provenance, "CLN: _ND (all CLN codes NA / none scored after finalize).")
		return r
	}
	r.Subtotal()
	r.Provenance = append(r.Provenance, fmt.Sprintf("CLN: final %s -> %s.", r.Sub.Detail(), py(r.Total)))
	return r
}

type familyTotal struct {
	Code  *string
	Total *float64
}

func Combine(families []familyTotal) (*ScoreResult, error) {
	r := score("")
	r.Provenance = []string{"CASE: cross-code combine (reference) -- PFD parent + POP + CLN + LOC summed; UNCLAMPED (SM 1 Pathogenic is open-ended >=+10; the GA4GH scale 10/-8 is a display concern -- see known-gaps)."}
	for _, v := range families {
		if v.Total == nil {
			continue
		}
		key := "?"
		if v.Code != nil {
			key = *v.Code
		}
		if _, ok := r.Sub.Values[key]; ok {
			return nil, fmt.Errorf("CASE: duplicate family code %s across subtotals.", repr(key))
		}
		r.Sub.Set(key, *v.Total)
	}
	if len(r.Sub.Keys) == 0 {
		r.Provenance = append(r.Provenance, "CASE: _ND (no family scored).")
		return r, nil
	}
	r.Subtotal()
	r.Provenance = append(r.Provenance, fmt.Sprintf("CASE: final total %s (%s).", py(r.Total), r.Sub.Detail()))
	return r, nil
}
