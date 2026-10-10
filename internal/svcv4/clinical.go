package svcv4

import (
	"fmt"
	"strings"
)

func classification(v interface{}) string {
	c := strings.ToUpper(strings.TrimSpace(str(v)))
	switch c {
	case "P", "PATHOGENIC":
		return "P"
	case "LP", "LIKELY_PATHOGENIC":
		return "LP"
	case "B", "BENIGN":
		return "B"
	case "LB", "LIKELY_BENIGN":
		return "LB"
	case "VUS":
		return "VUS"
	}
	return ""
}
func categories(c object) map[string]bool {
	m := map[string]bool{}
	for _, v := range list(c["additional_variants"]) {
		m[classification(obj(v)["classification"])] = true
	}
	return m
}
func ClinicalUnaffected(c object, moi string) *ScoreResult {
	r := score("CLN")
	r.Provenance = []string{`CLN: "CLN" is the HOD grouping label; scored per Case (cross-proband sum + CLN_CCS exclusivity deferred to case aggregation).`}
	col := ""
	pen := str(c["age_matched_penetrance"])
	if moi == "AD" || moi == "SD" {
		col = "dom"
	} else if moi == "AR" || moi == "XLD" || moi == "XLR" {
		switch c["vbc_zygosity"] {
		case "HOM", "HEMI":
			col = "rec_homo_hemi"
		case "HET":
			switch classification(get(c, "compound_het_variant", "classification")) {
			case "P":
				col = "rec_trans_p"
			case "LP":
				col = "rec_trans_lp"
			default:
				col = "no_trans_plp"
			}
		}
	}
	if col == "" {
		r.Provenance = append(r.Provenance, "CLN_UAF: _ND (moi unknown, or recessive/XL VBC zygosity unknown)")
		return r
	}
	n := 0.0
	if col == "no_trans_plp" {
		r.Provenance = append(r.Provenance, "CLN_UAF: 0.0 (recessive/XL HET, no confirmed-trans P/LP -- SM 4 L203)")
	} else {
		if pen == "NEAR_100" {
			n = -4
		} else if pen == "PCT_80_100" {
			n = -2
		}
		if col == "rec_trans_lp" {
			n /= 2
		}
		r.Provenance = append(r.Provenance, fmt.Sprintf("CLN_UAF: %s (col=%s, penetrance=%s)", pyFloat(n), col, optional(pen)))
	}
	r.Sub.Set("CLN_UAF", n)
	r.Total = num(n)
	return r
}
func ClinicalAlternate(c object, moi string) *ScoreResult {
	r := score("CLN")
	r.Provenance = []string{`CLN: "CLN" is the HOD grouping label; scored per Case (cross-proband sum deferred).`}
	alts := []object{}
	for _, v := range list(c["additional_variants"]) {
		m := obj(v)
		cls := classification(m["classification"])
		if cls == "P" || cls == "LP" {
			alts = append(alts, m)
		}
	}
	if len(alts) == 0 {
		r.Provenance = append(r.Provenance, "CLN_ALT: _ND (no P/LP alternate-cause variant -- Table 4 gate)")
		return r
	}
	sev := str(c["pheno_severity"])
	if sev == "" {
		r.Provenance = append(r.Provenance, "CLN_ALT: _ND (no pheno_severity)")
		return r
	}
	n := 0.0
	switch sev {
	case "MONO_EQ_EXPECTED":
		n = -.5
	case "BIALLELIC_LT_EXPECTED":
		same := false
		for _, m := range alts {
			if m["phase_in_ref_to_vbc"] != nil {
				same = true
			}
		}
		pen := c["age_matched_penetrance"] == "PCT_80_100" || c["age_matched_penetrance"] == "NEAR_100"
		if same && pen {
			n = -1
		}
		r.Provenance = append(r.Provenance, fmt.Sprintf("CLN_ALT BIALLELIC_LT_EXPECTED: same_gene=%s, penetrance>80%%=%s (>80%% = PCT_80_100/NEAR_100; SM 4 L198-200)", py(same), py(pen)))
	}
	r.Provenance = append(r.Provenance, fmt.Sprintf("CLN_ALT: %s (pheno_severity=%s; 'in expected zygosity' trusted to input)", pyFloat(n), sev))
	r.Sub.Set("CLN_ALT", n)
	r.Total = num(n)
	return r
}
func ClinicalMono(c object, moi string) *ScoreResult {
	r := score("CLN")
	r.Provenance = []string{`CLN: "CLN" is the HOD grouping label; scored per Case (table selection + cross-proband sum + the AD +1.0/proband ceiling-on-sum deferred to case aggregation).`}
	pheno := str(c["pheno_specificity_for_mde"])
	if pheno == "" {
		r.Provenance = append(r.Provenance, "CLN_AFF: _ND (no pheno_specificity_for_mde)")
		return r
	}
	n := 0.0
	if pheno == "INCONSISTENT" {
		r.Provenance = append(r.Provenance, "CLN_AFF: 0.0 (phenotype INCONSISTENT -- SM 4 Table 1; -> CLN_UAF)")
	} else {
		cats := categories(c)
		thorough := get(c, "testing", "covers_all_genes_relevant_to_mde") == "TRUE" && get(c, "testing", "non_genetic_etiology_excluded") == "TRUE"
		tier := "middle"
		n = .5
		if cats["P"] || cats["LP"] {
			tier, n = "plp_alt", 0
		} else if thorough && !cats["VUS"] {
			tier, n = "best", 1
		}
		if pheno == "CONSISTENT" {
			n /= 2
		}
		r.Provenance = append(r.Provenance, fmt.Sprintf("CLN_AFF: %s (phenotype=%s, tier=%s)", pyFloat(n), pheno, tier))
	}
	r.Sub.Set("CLN_AFF", n)
	r.Total = num(n)
	return r
}
func biallelicColumn(c object) string {
	if c["vbc_zygosity"] == "HOM" {
		return "hom"
	}
	if c["vbc_zygosity"] != "HET" {
		return ""
	}
	ch := obj(c["compound_het_variant"])
	cls := classification(ch["classification"])
	confirmed := ch["phase_confidence"] == "HIGH"
	if cls == "P" || cls == "LP" {
		if confirmed {
			return "conf_plp"
		}
		return "assumed_plp"
	}
	if cls == "VUS" && confirmed {
		return "conf_vus"
	}
	return "none"
}

var table2 = map[string]map[string]float64{"A1": {"conf_plp": 3, "assumed_plp": 1.5, "conf_vus": 1.5, "hom": 1, "none": 0}, "A2": {"conf_plp": 2, "assumed_plp": 1, "conf_vus": 1, "hom": 1, "none": 0}, "B": {"conf_plp": 1, "assumed_plp": .75, "conf_vus": .5, "hom": .5, "none": 0}, "zero": {"conf_plp": 0, "assumed_plp": 0, "conf_vus": 0, "hom": 0, "none": 0}}

func ClinicalBiallelic(c object, moi string) *ScoreResult {
	r := score("CLN")
	r.Provenance = []string{`CLN: "CLN" is the HOD grouping label; scored per Case (table selection + cross-proband sum + the AD ceiling-on-sum deferred to case aggregation).`}
	pheno := str(c["pheno_specificity_for_mde"])
	if pheno == "" {
		r.Provenance = append(r.Provenance, "CLN_AFF: _ND (no pheno_specificity_for_mde)")
		return r
	}
	col := biallelicColumn(c)
	if col == "" {
		r.Provenance = append(r.Provenance, "CLN_AFF: _ND (VBC zygosity absent or hemizygous -- not a Table 2 column)")
		return r
	}
	cats := categories(c)
	row := "B"
	if pheno == "INCONSISTENT" || cats["P"] || cats["LP"] {
		row = "zero"
	} else {
		thorough := get(c, "testing", "covers_all_genes_relevant_to_mde") == "TRUE" && get(c, "testing", "non_genetic_etiology_excluded") == "TRUE" && !cats["VUS"]
		if thorough {
			if col == "hom" {
				row = "A1"
			} else {
				switch get(c, "compound_het_variant", "co_occurrence_likelihood") {
				case "LT_0_0001":
					row = "A1"
				case "BETWEEN_0_0001_0_01":
					row = "A2"
				}
			}
		}
	}
	n := table2[row][col]
	r.Provenance = append(r.Provenance, fmt.Sprintf("CLN_AFF: %s (biallelic Table 2, column=%s, row=%s)", pyFloat(n), col, row))
	r.Sub.Set("CLN_AFF", n)
	r.Total = num(n)
	return r
}
func ClinicalDeNovo(c object, moi string, biallelic *bool) *ScoreResult {
	r := score("CLN")
	r.Provenance = []string{`CLN: "CLN" is the HOD grouping label; scored per Case (cross-proband sum + summing CLN_AFF + CLN_DNV per proband deferred to case aggregation).`}
	pheno := str(c["pheno_specificity_for_mde"])
	if pheno == "" {
		r.Provenance = append(r.Provenance, "CLN_DNV: _ND (no pheno_specificity_for_mde)")
		return r
	}
	bi := moi == "AR" || moi == "XLR"
	if biallelic != nil {
		bi = *biallelic
	}
	if bi && pheno == "SPECIFIC" {
		pheno = "CONSISTENT"
		r.Provenance = append(r.Provenance, "CLN_DNV: biallelic disorder -> SPECIFIC folds to CONSISTENT.")
	}
	confirmed := c["confirmed_parental_relationship"] == "TRUE"
	n := 0.0
	switch pheno {
	case "SPECIFIC":
		n = 2
		if confirmed {
			n = 7
			r.Provenance = append(r.Provenance, "CLN_DNV: +7.0 ** -- SM 4 recommends reducing this if the VBC is outside coding/adjacent-intronic regions; not applied (no VBC-region annotation).")
		}
	case "CONSISTENT":
		n = 1
		if confirmed {
			n = 4
		}
	}
	r.Provenance = append(r.Provenance, fmt.Sprintf("CLN_DNV: %s (row=%s, confirmed_parental=%s)", pyFloat(n), pheno, py(confirmed)))
	r.Sub.Set("CLN_DNV", n)
	r.Total = num(n)
	return r
}
func CaseControl(e object) *ScoreResult {
	r := score("CLN")
	r.Provenance = []string{`CLN: "CLN" is the HOD grouping label. When CLN_CCS is applied, SM 4 marks all other CLN codes NA except CLN_DNV -- exclusivity deferred to case aggregation.`}
	odds := value(e["odds_ratio"])
	if odds == nil {
		r.Provenance = append(r.Provenance, "CLN_CCS: _ND (no odds_ratio)")
		return r
	}
	count, cohort := value(e["case_variant_count"]), value(e["case_cohort_size"])
	if count == nil || *count < 5 || cohort == nil || *cohort < 100 || e["controls_matched"] != true {
		r.Provenance = append(r.Provenance, "CLN_CCS: _ND (study not robust -- SM 4 requires >=5 case-variant observations, >=100 unrelated cases, and matched controls).")
		return r
	}
	if e["ascertainment_bias_considered"] != true {
		r.Provenance = append(r.Provenance, "CLN_CCS: note -- ascertainment_bias_considered is not TRUE (SM 4 caution).")
	}
	lo, hi := value(e["ci_lower"]), value(e["ci_upper"])
	includes := lo != nil && hi != nil && *lo <= 1 && *hi >= 1
	n := 0.0
	if *odds > 5 && !includes {
		n = 4
		r.Provenance = append(r.Provenance, fmt.Sprintf("CLN_CCS: +4.0 (OR %s > 5.0, CI excludes 1.0)", py(odds)))
	} else if *odds <= 1 {
		r.Provenance = append(r.Provenance, fmt.Sprintf("CLN_CCS: 0.0 (OR %s <= 1.0 -- benignity indicated, but SM 4 assigns no CLN_CCS benign point value; see known-gaps).", py(odds)))
	} else if includes {
		r.Provenance = append(r.Provenance, fmt.Sprintf("CLN_CCS: 0.0 (OR %s, CI includes 1.0 -- association not significant)", py(odds)))
	} else {
		r.Provenance = append(r.Provenance, fmt.Sprintf("CLN_CCS: 0.0 (OR %s <= 5.0 -- insufficient enrichment)", py(odds)))
	}
	r.Sub.Set("CLN_CCS", n)
	r.Total = num(n)
	return r
}
func ClinicalProband(c object, moi string) *ScoreResult {
	r := score("CLN")
	r.Provenance = []string{"CLN: per-proband combine (reference); cross-proband sum + CLN_CCS exclusivity + POP_FRQ gating deferred to Inc 3b/3c."}
	merge := func(v *ScoreResult) {
		for _, k := range v.Sub.Keys {
			r.Sub.Set(k, v.Sub.Values[k])
		}
		lines := v.Provenance
		if len(lines) > 0 && strings.HasPrefix(lines[0], "CLN:") {
			lines = lines[1:]
		}
		r.Provenance = append(r.Provenance, lines...)
	}
	pheno := c["pheno_specificity_for_mde"]
	if pheno == "SPECIFIC" || pheno == "CONSISTENT" {
		known, bi := true, false
		switch moi {
		case "AD", "XLD":
		case "AR":
			bi = true
		case "XLR":
			switch c["sex"] {
			case "M":
			case "F":
				bi = true
			default:
				known = false
			}
		case "SD":
			bi = c["vbc_zygosity"] == "HOM" || (c["vbc_zygosity"] == "HET" && c["compound_het_variant"] != nil)
		default:
			known = false
		}
		if !known {
			r.Provenance = append(r.Provenance, "CLN_AFF: _ND (unroutable -- moi None, or XLR without a known sex)")
		} else {
			if bi {
				merge(ClinicalBiallelic(c, moi))
			} else {
				merge(ClinicalMono(c, moi))
			}
			if moi != "AR" {
				merge(ClinicalAlternate(c, moi))
			}
			if aff, exists := r.Sub.Values["CLN_AFF"]; exists {
				if aff > 0 {
					parents, absent := 0, true
					for _, v := range list(c["relatives"]) {
						m := obj(v)
						if m["parent_of_proband"] == "TRUE" {
							parents++
							if m["vbc_exists"] != "FALSE" {
								absent = false
							}
						}
					}
					if parents >= 2 && absent && c["confirmed_parental_relationship"] == "TRUE" {
						merge(ClinicalDeNovo(c, moi, &bi))
					} else {
						r.Provenance = append(r.Provenance, "CLN_DNV: not scored (proband not inferred de-novo)")
					}
				} else if aff == 0 {
					r.Provenance = append(r.Provenance, "CLN_DNV: not scored (CLN_AFF 0.0 -- phenotype explained by alternate cause; proband not counted under CLN_AFF).")
				}
			}
		}
	} else {
		r.Provenance = append(r.Provenance, "CLN: unaffected/inconsistent path (pheno not SPECIFIC/CONSISTENT) -> CLN_UAF")
		merge(ClinicalUnaffected(c, moi))
	}
	r.Subtotal()
	if r.Total == nil {
		r.Provenance = append(r.Provenance, "CLN: _ND (no CLN sub-code scored for this proband)")
	}
	return r
}
