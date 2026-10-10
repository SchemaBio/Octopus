// Development-only benchmark driver; not part of the Octopus runtime image.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/resultengine"
)

func main() {
	root := flag.String("root", "/tmp/resultengine-benchmark", "fixture directory")
	rows := flag.Int("rows", 100000, "fixture rows")
	op := flag.String("operation", "query", "query/sort/filter/export/prepare")
	name := flag.String("file", "", "fixture basename within root")
	flag.Parse()
	file := filepath.Join(*root, fmt.Sprint(*rows)+".parquet")
	if *name != "" {
		if filepath.Base(*name) != *name {
			panic("fixture must be a basename")
		}
		file = filepath.Join(*root, *name)
	}
	f, err := os.Open(file)
	if err != nil {
		panic(err)
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		panic(err)
	}
	f.Close()
	temp := filepath.Join(*root, "go-temp")
	os.MkdirAll(temp, 0750)
	e := resultengine.New(*root, filepath.Join(*root, "go-assessments"), temp)
	q := resultengine.Request{Table: "snv-indel", FilePath: file, DatasetID: strings.Repeat("a", 64), ObjectHash: hex.EncodeToString(hash.Sum(nil)), Limit: 20}
	if *op == "sort" || *op == "export" {
		q.Sort, q.Direction = "GnomAD_AF", "asc"
	}
	if *op == "filter" {
		q.Filters = []resultengine.Filter{{Column: "GnomAD_AF", Operator: "lt", Value: .000002}}
	}
	start := time.Now()
	total := int64(0)
	switch *op {
	case "prepare":
		var r *resultengine.Prepared
		r, err = e.Prepare(context.Background(), q)
		if err == nil {
			total = r.Rows
		}
	case "export":
		var path string
		path, err = e.Export(context.Background(), q)
		if err == nil {
			info, _ := os.Stat(path)
			total = info.Size()
			os.Remove(path)
		}
	default:
		var r *resultengine.Response
		r, err = e.Query(context.Background(), q)
		if err == nil {
			total = r.Total
		}
	}
	output := map[string]interface{}{"operation": *op, "rows": *rows, "elapsedSeconds": time.Since(start).Seconds(), "result": total, "implementation": "Go"}
	if err != nil {
		output["error"] = err.Error()
	}
	json.NewEncoder(os.Stdout).Encode(output)
	if err != nil {
		os.Exit(1)
	}
}
