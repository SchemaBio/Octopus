package resultengine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPythonOracle(t *testing.T) {
	data, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Operation string          `json:"operation"`
		Input     Request         `json:"input"`
		Output    json.RawMessage `json:"output"`
		Error     interface{}     `json:"error"`
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("%02d_%s", i, c.Operation), func(t *testing.T) {
			root, _ := filepath.Abs("testdata")
			temp := t.TempDir()
			engine := New(root, temp, temp)
			q := c.Input
			q.FilePath = filepath.Join(root, q.FilePath)
			var got interface{}
			var err error
			switch c.Operation {
			case "query":
				got, err = engine.Query(context.Background(), q)
			case "export":
				var path string
				path, err = engine.Export(context.Background(), q)
				if err == nil {
					var bytes []byte
					bytes, err = os.ReadFile(path)
					got = string(bytes)
					os.Remove(path)
				}
			case "prepare":
				var p *Prepared
				p, err = engine.Prepare(context.Background(), q)
				if err == nil {
					data, _ := os.ReadFile(p.AssessmentFile)
					records := []interface{}{}
					for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
						if line == "" {
							continue
						}
						var v interface{}
						if e := json.Unmarshal([]byte(line), &v); e != nil {
							t.Fatal(e)
						}
						records = append(records, v)
					}
					got = map[string]interface{}{"profile": p.Profile, "rows": p.Rows, "records": records}
				}
			}
			if c.Error != nil {
				if err == nil {
					t.Fatal("expected rejection")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var actual, expected interface{}
			json.Unmarshal(encoded, &actual)
			json.Unmarshal(c.Output, &expected)
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("oracle mismatch\nactual %s\nexpected %s", encoded, c.Output)
			}
		})
	}
}
