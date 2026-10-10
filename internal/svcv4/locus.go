package svcv4

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var percentToken = regexp.MustCompile(`[0-9]*\.?[0-9]+`)

func parsePercent(raw interface{}) *float64 {
	if raw == nil {
		return nil
	}
	s := str(raw)
	token := percentToken.FindString(s)
	if token == "" {
		return nil
	}
	n, _ := strconv.ParseFloat(token, 64)
	if !strings.Contains(s, "%") && n < 1 {
		n *= 100
	}
	if strings.HasPrefix(strings.TrimLeft(s, " \t\r\n"), "<") {
		n -= 1e-9
	}
	return num(n)
}
func pheBand(n float64) float64 {
	switch {
	case n < 33:
		return 0
	case n <= 50:
		return 1
	case n < 68:
		return 2
	case n < 82:
		return 3
	default:
		return 4
	}
}
func nonSeg(c object, moi string) []string {
	reasons := []string{}
	near := c["age_matched_penetrance"] == "NEAR_100"
	for i, v := range list(c["relatives"]) {
		r := obj(v)
		if r["affected_w_mde"] == "TRUE" && r["vbc_exists"] == "FALSE" {
			reasons = append(reasons, fmt.Sprintf("relative[%d] affected but VBC-absent (rule a)", i))
		} else if moi != "" && moi != "AR" && near && r["affected_w_mde"] == "FALSE" && r["vbc_exists"] == "TRUE" {
			reasons = append(reasons, fmt.Sprintf("relative[%d] unaffected VBC-carrier at ~100%% penetrance (rule b)", i))
		}
	}
	return reasons
}
func LocusPhenotype(c object, moi string) *ScoreResult {
	r := score("LOC")
	r.Provenance = []string{`LOC: "LOC" is the HOD grouping label. LOC_SEG (co-segregation) and the combined LOC +4.0 cap are computed in LOC-2 / case aggregation.`}
	raw := get(c, "testing", "diagnostic_yield_for_phenotypes")
	pct := parsePercent(raw)
	if pct == nil {
		r.Provenance = append(r.Provenance, fmt.Sprintf("LOC_PHE: _ND (no parseable diagnostic yield; raw=%s)", repr(raw)))
		return r
	}
	n := pheBand(*pct)
	r.Provenance = append(r.Provenance, fmt.Sprintf("LOC_PHE: +%s from diagnostic yield (raw=%s); robustness caveats (sample size, 95%% CI, methodology match) and most-specific-proband selection not verifiable from captured inputs -- reference-only.", pyFloat(n), repr(raw)))
	if n > 0 {
		reasons := nonSeg(c, moi)
		if len(reasons) > 0 {
			r.Provenance = append(r.Provenance, "LOC_PHE: zeroed to 0.0 -- non-segregation observed: "+strings.Join(reasons, "; "))
			if moi == "AR" {
				r.Provenance = append(r.Provenance, "LOC_PHE: AR caveat -- an AR non-segregation may reflect another causative locus, not benignity; the LOC_SEG -4.0 flip is not applied (LOC_SEG is deferred to LOC-2).")
			}
			n = 0
		}
	}
	r.Sub.Set("LOC_PHE", n)
	r.Total = num(n)
	return r
}
func segregant(r object, moi string, near bool) *float64 {
	aff, carrier, zyg := r["affected_w_mde"], r["vbc_exists"], r["vbc_zygosity"]
	comp, severe := r["cmp_het_variant_exists"] == "TRUE", r["severe_phenotype"] == "TRUE"
	if aff == "TRUE" && carrier == "TRUE" {
		switch moi {
		case "AD":
			if zyg == "HET" {
				return num(1)
			}
		case "AR":
			if zyg == "HOM" || comp {
				return num(2)
			}
		case "SD":
			if severe && (zyg == "HOM" || comp) {
				return num(2)
			}
			if zyg == "HET" {
				return num(1)
			}
		case "XLD", "XLR":
			if zyg == "HEMI" || zyg == "HOM" || zyg == "HET" || comp {
				return num(1)
			}
		}
		return nil
	}
	if aff == "FALSE" {
		if moi == "AR" {
			if carrier == "FALSE" || (carrier == "TRUE" && zyg == "HET") {
				return num(.4)
			}
			return nil
		}
		if near && carrier == "FALSE" {
			return num(1)
		}
	}
	return nil
}
func LocusSegregation(c object, moi string) *ScoreResult {
	r := score("LOC")
	r.Provenance = []string{`LOC: "LOC" is the HOD grouping label; the combined LOC +4.0 cap (with LOC_PHE) is applied in case aggregation.`}
	if moi == "" {
		r.Provenance = append(r.Provenance, "LOC_SEG: _ND (MOI is required to tier co-segregations, SM 5 Figure 2)")
		return r
	}
	r.Provenance = append(r.Provenance, "LOC_SEG: the SM 5 Figure 2 entry gate (>1 locus AND phenocopy rate very low/zero) is not captured in the model -- assumed satisfied; reference-only (see known-gaps).")
	reasons := nonSeg(c, moi)
	if len(reasons) > 0 {
		n := 0.0
		if moi == "AD" || moi == "XLD" || moi == "XLR" || (moi == "AR" && c["vbc_zygosity"] == "HOM") {
			n = -4
			r.Provenance = append(r.Provenance, fmt.Sprintf("LOC_SEG: -4.0 -- non-segregation observed (%s); AD / AR-homozygous / X-linked benign flip (SM 5 Figure 2). This also zeroes LOC_PHE.", strings.Join(reasons, "; ")))
		} else {
			r.Provenance = append(r.Provenance, fmt.Sprintf("LOC_SEG: 0.0 -- non-segregation observed (%s); the -4.0 flip is NOT applied for moi=%s (a plain-AR / semidominant non-segregation may reflect another causative locus, not benignity). This also zeroes LOC_PHE.", strings.Join(reasons, "; "), moi))
		}
		r.Sub.Set("LOC_SEG", n)
		r.Total = num(n)
		return r
	}
	total, count := 0.0, 0
	near := c["age_matched_penetrance"] == "NEAR_100"
	for i, v := range list(c["relatives"]) {
		n := segregant(obj(v), moi, near)
		if n != nil {
			total += *n
			count++
			r.Provenance = append(r.Provenance, fmt.Sprintf("LOC_SEG: relative[%d] +%s co-segregation (%s)", i, py(n), moi))
		}
	}
	if count == 0 {
		r.Provenance = append(r.Provenance, "LOC_SEG: _ND (no informative co-segregations among captured relatives)")
		return r
	}
	capped := Cap(num(total), 0, 4)
	if *capped != total {
		r.Provenance = append(r.Provenance, fmt.Sprintf("LOC_SEG: raw sum %s capped to %s (0.0..+4.0, SM 5 L38)", pyFloat(total), py(capped)))
	}
	r.Sub.Set("LOC_SEG", *capped)
	r.Total = capped
	return r
}
