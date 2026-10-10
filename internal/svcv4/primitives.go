package svcv4

func Cap(v *float64, lo, hi float64) *float64 {
	if v == nil {
		return nil
	}
	// Python max(lo, min(hi, value)) compares in that order; in particular a
	// permissively parsed NaN string takes hi. Preserve the pinned behavior.
	inner := hi
	if *v < hi {
		inner = *v
	}
	if inner > lo {
		return num(inner)
	}
	return num(lo)
}
func Hold(lo, hi float64, parts ...*float64) *float64 {
	sum := 0.0
	found := false
	for _, p := range parts {
		if p != nil {
			sum += *p
			found = true
		}
	}
	if !found {
		return nil
	}
	return Cap(num(sum), lo, hi)
}
func informative(variants []interface{}, missense bool) *float64 {
	counts := map[string]map[string]int{}
	found := false
	for _, v := range variants {
		m := obj(v)
		c := str(m["classification"])
		if c == "" {
			continue
		}
		category := "all"
		if missense {
			category = str(m["category"])
			if category == "" {
				continue
			}
		}
		if counts[category] == nil {
			counts[category] = map[string]int{}
		}
		counts[category][c]++
		if !missense {
			found = true
		}
	}
	standard := func(c map[string]int, strong, weak string) float64 {
		s, w := c[strong], c[weak]
		n := 0.0
		if s > 0 {
			n += 2
		}
		if w > 0 {
			n++
		}
		return n + float64(max(s-1, 0)+max(w-1, 0))
	}
	doubled := func(c map[string]int, strong, weak string) float64 {
		s, w := c[strong], c[weak]
		if s+w == 0 {
			return 0
		}
		found = true
		first := 2.0
		if s > 0 {
			first = 4
		}
		return first + 2*float64(s+w-1)
	}
	if !missense {
		if !found {
			return nil
		}
		return num(standard(counts["all"], "PATHOGENIC", "LIKELY_PATHOGENIC") - standard(counts["all"], "BENIGN", "LIKELY_BENIGN"))
	}
	// Categories are the pinned enum values, not heuristic variant similarities.
	c1, c2, c3, c4 := counts["SAME_AA_PATHOGENIC"], counts["DISTINCT_AA_PATHOGENIC"], counts["DISTINCT_AA_BENIGN"], counts["SAME_AA_BENIGN"]
	n := doubled(c1, "PATHOGENIC", "LIKELY_PATHOGENIC") + standard(c2, "PATHOGENIC", "LIKELY_PATHOGENIC") - standard(c3, "BENIGN", "LIKELY_BENIGN") - doubled(c4, "BENIGN", "LIKELY_BENIGN")
	if c2["PATHOGENIC"]+c2["LIKELY_PATHOGENIC"]+c3["BENIGN"]+c3["LIKELY_BENIGN"] > 0 {
		found = true
	}
	if !found {
		return nil
	}
	return num(n)
}
func SM18(v *float64, mech, exon, gdv string, mechanismOnly bool) *float64 {
	if v == nil || *v <= 0 {
		return v
	}
	if gdv != "MODERATE" && gdv != "STRONG" && gdv != "DEFINITIVE" {
		return num(0)
	}
	m := map[string]float64{"ESTABLISHED": 1, "LIKELY": .5, "SUSPECTED": .25, "UNCERTAIN": 0}[mech]
	if mechanismOnly {
		return num(*v * m)
	}
	e := 1.0
	switch exon {
	case "MOST":
		e = .5
	case "FEW":
		e = 0
	}
	if mech == "SUSPECTED" && exon == "MOST" {
		return num(0)
	}
	return num(*v * m * e)
}
func Transcript(v *float64, exon string) *float64 {
	if v == nil || *v <= 0 {
		return v
	}
	f := 1.0
	switch exon {
	case "MOST":
		f = .5
	case "FEW":
		f = 0
	}
	return num(*v * f)
}
func Classify(n float64) (string, *string) {
	category := "BENIGN"
	sub := ""
	switch {
	case n >= 10:
		category = "PATHOGENIC"
	case n >= 6:
		category = "LIKELY_PATHOGENIC"
	case n >= 4:
		category, sub = "VUS", "VUS-high"
	case n >= 2:
		category, sub = "VUS", "VUS-mid"
	case n > -1:
		category, sub = "VUS", "VUS-low"
	case n > -4:
		category = "LIKELY_BENIGN"
	}
	if sub == "" {
		return category, nil
	}
	return category, &sub
}
