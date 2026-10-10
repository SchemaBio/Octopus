package resultengine

import (
	"fmt"
	"strconv"
	"strings"
)

type Filter struct {
	Column   string      `json:"column"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
}

func validateField(name string, fields map[string]bool) error {
	if name == "" || len(name) > 256 || strings.ContainsRune(name, 0) {
		return fmt.Errorf("invalid field")
	}
	if fields[name] {
		return nil
	}
	for _, f := range overlayFields {
		if f == name {
			return nil
		}
	}
	return fmt.Errorf("unknown filter field")
}
func validateFilter(f Filter, fields map[string]bool) error {
	if err := validateField(f.Column, fields); err != nil {
		return err
	}
	switch f.Operator {
	case "is_missing", "is_not_missing", "gt", "gte", "lt", "lte":
		return nil
	case "contains", "equals", "in":
		if a, ok := f.Value.([]interface{}); ok && (len(a) == 0 || len(a) > 1000) {
			return fmt.Errorf("invalid filter values")
		}
		return nil
	case "between":
		a, ok := f.Value.([]interface{})
		if !ok || len(a) != 2 {
			return fmt.Errorf("between requires two numeric bounds")
		}
		lo, ok1 := numeric(a[0])
		hi, ok2 := numeric(a[1])
		if !ok1 || !ok2 {
			return fmt.Errorf("between requires numeric bounds")
		}
		if lo > hi {
			return fmt.Errorf("between lower bound exceeds upper bound")
		}
		return nil
	default:
		return fmt.Errorf("unsupported filter")
	}
}
func matches(v interface{}, f Filter) bool {
	missing := v == nil || v == "" || v == "."
	if f.Operator == "is_missing" {
		return missing
	}
	if f.Operator == "is_not_missing" {
		return !missing
	}
	if v == nil {
		return false
	}
	s := text(v)
	values, ok := f.Value.([]interface{})
	if !ok {
		values = []interface{}{f.Value}
	}
	switch f.Operator {
	case "contains":
		return strings.Contains(strings.ToLower(s), strings.ToLower(text(values[0])))
	case "equals", "in":
		for _, part := range strings.Split(s, "&") {
			for _, needle := range values {
				compared := text(needle)
				if f.Operator == "in" {
					if b, ok := needle.(bool); ok {
						compared = "False"
						if b {
							compared = "True"
						}
					}
				}
				if part == compared {
					return true
				}
			}
		}
		return false
	default:
		for _, part := range strings.Split(s, "&") {
			n, ok := numeric(part)
			if !ok {
				continue
			}
			bound, ok := numeric(values[0])
			if !ok {
				continue
			}
			switch f.Operator {
			case "gt":
				if compareNumber(n, bound) > 0 {
					return true
				}
			case "gte":
				if compareNumber(n, bound) >= 0 {
					return true
				}
			case "lt":
				if compareNumber(n, bound) < 0 {
					return true
				}
			case "lte":
				if compareNumber(n, bound) <= 0 {
					return true
				}
			case "between":
				upper, _ := numeric(values[1])
				if compareNumber(n, bound) >= 0 && compareNumber(n, upper) <= 0 {
					return true
				}
			}
		}
		return false
	}
}

type sortKey struct {
	Missing bool     `json:"missing"`
	Number  *float64 `json:"number,omitempty"`
	Text    string   `json:"text,omitempty"`
}

func makeKey(name string, value interface{}) sortKey {
	lower := strings.ToLower(name)
	if lower == "chr" || lower == "chrom" || lower == "chromosome" {
		s := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(text(value))), "CHR")
		n, e := strconv.ParseInt(s, 10, 32)
		if e != nil {
			switch s {
			case "X":
				n = 23
			case "Y":
				n = 24
			case "M", "MT":
				n = 25
			default:
				n = 1000
			}
		}
		f := float64(n)
		return sortKey{Number: &f}
	}
	if isNumeric(name) {
		var min *float64
		for _, part := range strings.Split(text(value), "&") {
			if n, ok := numeric(part); ok && (min == nil || compareNumber(n, *min) < 0) {
				copy := n
				min = &copy
			}
		}
		return sortKey{Missing: min == nil, Number: min}
	}
	return sortKey{Missing: value == nil || value == "" || value == ".", Text: text(value)}
}
