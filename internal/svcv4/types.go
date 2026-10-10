// Package svcv4 ports the NON-AUTHORITATIVE scoring implementation pinned in
// parquet-query/vendor/svcv4/UPSTREAM.md. ClinGen CSpec remains authoritative.
package svcv4

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const Revision = "ef66faff51a265fef7b5c4e6439905f3aa540c46"
const Source = "https://github.com/clingen-data-model/svcv4-model/tree/" + Revision

//go:embed schema.json
var schemaBytes []byte

//go:embed branches.json
var branchBytes []byte

var schemaDocument = func() object {
	var m object
	if err := json.Unmarshal(schemaBytes, &m); err != nil {
		panic(err)
	}
	return m
}()

type object = map[string]interface{}

func Schema() object { var m object; json.Unmarshal(schemaBytes, &m); return m }

type Points struct {
	Values map[string]float64
	Keys   []string
}

func (p Points) MarshalJSON() ([]byte, error) { return json.Marshal(p.Values) }
func (p *Points) UnmarshalJSON(data []byte) error {
	p.Values = map[string]float64{}
	p.Keys = []string{}
	d := json.NewDecoder(bytes.NewReader(data))
	if _, err := d.Token(); err != nil {
		return err
	}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return err
		}
		var value float64
		if err = d.Decode(&value); err != nil {
			return err
		}
		p.Set(token.(string), value)
	}
	_, err := d.Token()
	return err
}
func points() Points { return Points{Values: map[string]float64{}, Keys: []string{}} }
func (p *Points) Set(k string, v float64) {
	if p.Values == nil {
		p.Values = map[string]float64{}
	}
	if _, ok := p.Values[k]; !ok {
		p.Keys = append(p.Keys, k)
	}
	p.Values[k] = v
}
func (p Points) Sum() float64 {
	sum := 0.0
	for _, k := range p.Keys {
		sum += p.Values[k]
	}
	return sum
}
func (p Points) Detail() string {
	parts := []string{}
	for _, k := range p.Keys {
		parts = append(parts, k+"="+pyFloat(p.Values[k]))
	}
	return strings.Join(parts, ", ")
}

type ScoreResult struct {
	ParentCode    *string  `json:"parent_code"`
	Sub           Points   `json:"sub_code_points"`
	Held          Points   `json:"held_combined"`
	Total         *float64 `json:"parent_total"`
	Provenance    []string `json:"provenance"`
	Authoritative bool     `json:"authoritative"`
}

func score(code string) *ScoreResult {
	r := &ScoreResult{Sub: points(), Held: points(), Provenance: []string{}}
	if code != "" {
		r.ParentCode = &code
	}
	return r
}
func (r *ScoreResult) Add(code string, v *float64) {
	if v != nil {
		r.Sub.Set(code, *v)
	}
}
func (r *ScoreResult) Subtotal() {
	if len(r.Sub.Keys) > 0 {
		r.Total = num(r.Sub.Sum())
	}
}

type MissenseResult struct {
	Amino         *ScoreResult `json:"amino_acid"`
	Splice        *ScoreResult `json:"splice"`
	Selected      string       `json:"selected_path"`
	Code          string       `json:"applied_parent_code"`
	Total         *float64     `json:"applied_total"`
	Provenance    []string     `json:"provenance"`
	Authoritative bool         `json:"authoritative"`
}

func num(v float64) *float64 { return &v }
func value(v interface{}) *float64 {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case float64:
		return num(x)
	case int:
		return num(float64(x))
	case json.Number:
		n, e := x.Float64()
		if e == nil {
			return num(n)
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "nan", "+nan", "-nan":
			return num(math.NaN())
		case "infinity", "+infinity", "inf", "+inf":
			return num(math.Inf(1))
		case "-infinity", "-inf":
			return num(math.Inf(-1))
		}
		n, e := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if e == nil {
			return num(n)
		}
	}
	return nil
}
func str(v interface{}) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}
func obj(v interface{}) object {
	m, _ := v.(map[string]interface{})
	if m == nil {
		return object{}
	}
	return m
}
func list(v interface{}) []interface{} {
	a, _ := v.([]interface{})
	if a == nil {
		return []interface{}{}
	}
	return a
}
func get(m object, keys ...string) interface{} {
	var v interface{} = m
	for _, k := range keys {
		v = obj(v)[k]
	}
	return v
}
func pyFloat(n float64) string {
	if math.IsNaN(n) {
		return "nan"
	}
	if math.IsInf(n, 1) {
		return "inf"
	}
	if math.IsInf(n, -1) {
		return "-inf"
	}
	format := byte('f')
	if n != 0 && (math.Abs(n) < 1e-4 || math.Abs(n) >= 1e16) {
		format = 'e'
	}
	s := strconv.FormatFloat(n, format, -1, 64)
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	return s
}
func py(v interface{}) string {
	if v == nil {
		return "None"
	}
	switch x := v.(type) {
	case *float64:
		if x == nil {
			return "None"
		}
		return pyFloat(*x)
	case float64:
		return pyFloat(x)
	case bool:
		if x {
			return "True"
		}
		return "False"
	default:
		return fmt.Sprint(v)
	}
}
func repr(v interface{}) string {
	if v == nil {
		return "None"
	}
	s := str(v)
	quote := "'"
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		quote = `"`
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, quote, `\`+quote)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return quote + s + quote
}
