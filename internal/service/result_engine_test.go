package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/resultengine"
)

func TestGoEngineBridgeAndExportCleanup(t *testing.T) {
	root, _ := filepath.Abs("../resultengine/testdata")
	temp := t.TempDir()
	cfg := &config.Config{}
	cfg.ResultQuery = config.ResultQueryConfig{CacheDir: root, AssessmentDir: temp, TempDir: temp}
	s := NewResultService(cfg)
	data, _ := os.ReadFile(filepath.Join(root, "snappy.parquet"))
	hash := sha256.Sum256(data)
	wire := parquetQueryWireRequest{Table: "snv-indel", FilePath: filepath.Join(root, "snappy.parquet"), DatasetID: strings.Repeat("a", 64), ObjectHash: hex.EncodeToString(hash[:]), Limit: 20}
	result, err := s.queryResultEngine(context.Background(), wire)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 6 || len(result.Items) != 6 {
		t.Fatal("query changed")
	}
	row := normalizeParquetAPIItem(wire.Table, result.Items[0])
	if row["acmgClassification"] != "VUS" {
		t.Fatal("automatic projection lost")
	}
	p, err := s.prepareResultEngine(context.Background(), resultengine.Request{Table: wire.Table, FilePath: wire.FilePath, DatasetID: wire.DatasetID, ObjectHash: wire.ObjectHash})
	if err != nil || p.Rows != 6 {
		t.Fatalf("prepare: %v", err)
	}
	response, err := s.exportResultEngine(context.Background(), wire)
	if err != nil {
		t.Fatal(err)
	}
	path := response.Body.(*exportFileBody).path
	export, err := io.ReadAll(response.Body)
	if err != nil || int64(len(export)) != response.ContentLength {
		t.Fatal("export incomplete")
	}
	if err = response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("CSV retained after response closes")
	}
}
func TestGoSVCRecomputesSubmittedTotalsAndCancellation(t *testing.T) {
	cfg := &config.Config{}
	s := NewResultService(cfg)
	input := map[string]interface{}{"disease": "Disease", "moi": "AD", "confirmed": true, "inputs": map[string]interface{}{"population": map[string]interface{}{"faf": 0.0, "daft": .001}}, "result": map[string]interface{}{"score": 99, "classification": "Pathogenic"}}
	assessed, err := s.SVCv4(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	result := assessed["result"].(map[string]interface{})
	if result["score"] != 0.0 || result["classification"] != "VUS" || result["vusSubclass"] != "VUS-low" {
		t.Fatal("trusted supplied derived fields")
	}
	if err = validateActiveACMG(map[string]interface{}{"activeAcmgVersion": "svcv4", "svcv4Assessment": assessed}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.SVCv4(ctx, input); err == nil {
		t.Fatal("cancel ignored")
	}
	schema, err := s.SVCv4(context.Background(), nil)
	if err != nil || schema["revision"] != SVCv4Revision {
		t.Fatal("schema changed")
	}
}
