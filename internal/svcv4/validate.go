package svcv4

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// normalize implements the Pydantic input behavior present in the pinned model:
// extra=forbid, nullable unions, defaults, enum literals and non-strict numeric
// and boolean coercions. The public JSON schema remains byte-for-byte sourced
// from that model, rather than generated from the Go implementation.
func normalize(v interface{}, s, root object, path string) (interface{}, error) {
	if ref := str(s["$ref"]); ref != "" {
		return normalize(v, obj(get(root, "$defs", strings.TrimPrefix(ref, "#/$defs/"))), root, path)
	}
	if alternatives, ok := s["anyOf"].([]interface{}); ok {
		for _, a := range alternatives {
			n, err := normalize(v, obj(a), root, path)
			if err == nil {
				return n, nil
			}
		}
		return nil, fmt.Errorf("%s: invalid nullable value", path)
	}
	failure := func() (interface{}, error) { return nil, fmt.Errorf("%s: invalid %s", path, str(s["type"])) }
	switch str(s["type"]) {
	case "null":
		if v != nil {
			return failure()
		}
		return nil, nil
	case "object":
		m, ok := v.(map[string]interface{})
		if !ok {
			return failure()
		}
		properties := obj(s["properties"])
		out := object{}
		if s["additionalProperties"] == false {
			for key := range m {
				if _, ok := properties[key]; !ok {
					return nil, fmt.Errorf("%s.%s: extra field", path, key)
				}
			}
		}
		required := map[string]bool{}
		for _, key := range list(s["required"]) {
			required[str(key)] = true
		}
		for key, property := range properties {
			rule := obj(property)
			value, present := m[key]
			if !present {
				if required[key] {
					return nil, fmt.Errorf("%s.%s: required field", path, key)
				}
				if d, ok := rule["default"]; ok {
					value = d
				} else if rule["type"] == "array" {
					value = []interface{}{}
				} else {
					value = nil
				}
			}
			n, err := normalize(value, rule, root, path+"."+key)
			if err != nil {
				return nil, err
			}
			out[key] = n
		}
		return out, nil
	case "array":
		a, ok := v.([]interface{})
		if !ok {
			return failure()
		}
		if min := value(s["minItems"]); min != nil && float64(len(a)) < *min {
			return failure()
		}
		if max := value(s["maxItems"]); max != nil && float64(len(a)) > *max {
			return failure()
		}
		out := []interface{}{}
		for i, item := range a {
			n, err := normalize(item, obj(s["items"]), root, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
		return out, nil
	case "string":
		text, ok := v.(string)
		if !ok {
			return failure()
		}
		if min := value(s["minLength"]); min != nil && float64(utf8.RuneCountInString(text)) < *min {
			return failure()
		}
		if max := value(s["maxLength"]); max != nil && float64(utf8.RuneCountInString(text)) > *max {
			return failure()
		}
		if pattern := str(s["pattern"]); pattern != "" {
			r, err := regexp.Compile(pattern)
			if err != nil || !r.MatchString(text) {
				return failure()
			}
		}
	case "number", "integer":
		n := value(v)
		if boolean, ok := v.(bool); ok {
			n = num(0)
			if boolean {
				n = num(1)
			}
		}
		if n == nil || math.IsNaN(*n) || math.IsInf(*n, 0) {
			return failure()
		}
		if s["type"] == "integer" && math.Trunc(*n) != *n {
			return failure()
		}
		for _, bound := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"} {
			limit := value(s[bound])
			if limit == nil {
				continue
			}
			bad := false
			switch bound {
			case "minimum":
				bad = *n < *limit
			case "maximum":
				bad = *n > *limit
			case "exclusiveMinimum":
				bad = *n <= *limit
			case "exclusiveMaximum":
				bad = *n >= *limit
			}
			if bad {
				return failure()
			}
		}
		v = *n
	case "boolean":
		if _, ok := v.(bool); !ok {
			if n := value(v); n != nil && (*n == 0 || *n == 1) {
				v = *n == 1
			} else if text, ok := v.(string); ok {
				switch strings.ToLower(text) {
				case "true", "t", "yes", "y", "on", "1":
					v = true
				case "false", "f", "no", "n", "off", "0":
					v = false
				default:
					return failure()
				}
			} else {
				return failure()
			}
		}
	default:
		if v == nil {
			return nil, nil
		}
	}
	if allowed, ok := s["enum"].([]interface{}); ok {
		found := false
		for _, a := range allowed {
			if reflect.DeepEqual(v, a) {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("%s: invalid enum value", path)
		}
	}
	if c, ok := s["const"]; ok && !reflect.DeepEqual(c, v) {
		return nil, fmt.Errorf("%s: invalid constant", path)
	}
	return v, nil
}
func validateModel(name string, input interface{}) (object, error) {
	schema := schemaDocument
	var spec object
	switch name {
	case "population", "case", "caseControl":
		spec = obj(schema[name])
	default:
		for _, w := range list(schema["workflows"]) {
			m := obj(w)
			if m["id"] == name {
				spec = obj(m["schema"])
				break
			}
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("此工作流暂不支持")
	}
	n, err := normalize(input, spec, spec, name)
	return obj(n), err
}
func finite(v interface{}) error {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Errorf("证据数值必须为有限数")
		}
	case json.Number:
		n, e := strconv.ParseFloat(string(x), 64)
		if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("证据数值必须为有限数")
		}
	case map[string]interface{}:
		for _, item := range x {
			if err := finite(item); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, item := range x {
			if err := finite(item); err != nil {
				return err
			}
		}
	}
	return nil
}
func truthy(v interface{}) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case []interface{}:
		return len(x) > 0
	case map[string]interface{}:
		return len(x) > 0
	}
	return true
}
