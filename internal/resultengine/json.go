package resultengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func pythonFloat(n float64) string {
	if math.IsNaN(n) {
		return "NaN"
	}
	if math.IsInf(n, 1) {
		return "Infinity"
	}
	if math.IsInf(n, -1) {
		return "-Infinity"
	}
	format := byte('f')
	if n != 0 && (math.Abs(n) < 1e-6 || math.Abs(n) >= 1e21) {
		format = 'e'
	}
	s := strconv.FormatFloat(n, format, -1, 64)
	if format == 'e' {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "e-0", "e-"), "e+0", "e+")
		s = strings.ReplaceAll(s, "e+", "e")
	}
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	return s
}
func asciiQuote(s string) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}

// Preserve object insertion order and DuckDB's compact JSON extraction encoding
// for opaque adjustment objects exported as JSON inside a CSV cell.
func pythonJSON(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return pythonValue(decoder)
}
func pythonValue(d *json.Decoder) (string, error) {
	token, err := d.Token()
	if err != nil {
		return "", err
	}
	switch v := token.(type) {
	case json.Delim:
		var out strings.Builder
		out.WriteRune(rune(v))
		first := true
		for d.More() {
			if !first {
				out.WriteByte(',')
			}
			first = false
			if v == '{' {
				key, err := d.Token()
				if err != nil {
					return "", err
				}
				out.WriteString(asciiQuote(key.(string)))
				out.WriteByte(':')
			}
			value, err := pythonValue(d)
			if err != nil {
				return "", err
			}
			out.WriteString(value)
		}
		end, err := d.Token()
		if err != nil {
			return "", err
		}
		out.WriteRune(rune(end.(json.Delim)))
		return out.String(), nil
	case string:
		return asciiQuote(v), nil
	case json.Number:
		s := v.String()
		if strings.ContainsAny(s, ".eE") {
			n, err := v.Float64()
			if err != nil {
				return "", err
			}
			return pythonFloat(n), nil
		}
		if s == "-0" {
			s = "0"
		}
		return s, nil
	case nil:
		return "null", nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	}
	return "", fmt.Errorf("invalid JSON token")
}
