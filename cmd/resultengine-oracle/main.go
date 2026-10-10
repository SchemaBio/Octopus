// Development-only exact query/export parity driver. Never included in runtime.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"github.com/SchemaBio/Octopus/internal/resultengine"
	"io"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", "", "fixture root")
	op := flag.String("operation", "query", "query/export")
	flag.Parse()
	var q resultengine.Request
	d := json.NewDecoder(os.Stdin)
	d.UseNumber()
	if err := d.Decode(&q); err != nil {
		panic(err)
	}
	q.FilePath = filepath.Join(*root, filepath.Base(q.FilePath))
	temp := filepath.Join(*root, "oracle-temp")
	if err := os.MkdirAll(temp, 0750); err != nil {
		panic(err)
	}
	e := resultengine.New(*root, temp, temp)
	if *op == "export" {
		path, err := e.Export(context.Background(), q)
		if err != nil {
			panic(err)
		}
		defer os.Remove(path)
		f, err := os.Open(path)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		h := sha256.New()
		n, err := io.Copy(h, f)
		if err != nil {
			panic(err)
		}
		json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"bytes": n, "sha256": hex.EncodeToString(h.Sum(nil))})
		return
	}
	r, err := e.Query(context.Background(), q)
	if err != nil {
		panic(err)
	}
	json.NewEncoder(os.Stdout).Encode(r)
}
