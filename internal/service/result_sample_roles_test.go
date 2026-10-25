package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSampleRoleMappingBoundToObjectAndEngine(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input.txt")
	text := "Chromosome\tPosition\tGenotype\tAD\tDepth\tGenotype_father\tAD_father\tDepth_father\tGenotype_mother\tAD_mother\tDepth_mother\n1\t65099775\t0/0\t53,0\t53\t0/1\t58,59\t117\t1|0\t63,68\t131\n"
	if err := os.WriteFile(input, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	parquet, _, _, err := convertArchivedTextParquet(input)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(parquet)
	data, _ := os.ReadFile(parquet)
	name := filepath.Join(root, "source.parquet")
	if err = os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	roles := map[string]interface{}{"proband": "mother", "father": "", "mother": "father"}
	manifest := map[string]interface{}{"sample_column_roles": map[string]interface{}{"snv-indel": map[string]interface{}{"source_sha256": digest, "roles": roles}}}
	dataset := model.ResultDataset{Table: "snv-indel", ObjectSHA256: digest}
	if err = attachSampleColumnRoles(&dataset, manifest); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.ResultQuery = config.ResultQueryConfig{CacheDir: root, AssessmentDir: root, TempDir: root}
	svc := NewResultService(cfg)
	wire := parquetQueryWireRequest{Table: "snv-indel", FilePath: name, DatasetID: strings.Repeat("a", 64), ObjectHash: digest, Limit: 20, SampleColumnRoles: dataset.SampleColumnRoles}
	result, err := svc.queryResultEngine(context.Background(), wire)
	if err != nil {
		t.Fatal(err)
	}
	row := result.Items[0]
	if row["Genotype"] != "1|0" || row["AD"] != "63,68" || row["Genotype_father"] != "0/0" || row["AD_mother"] != "58,59" {
		t.Fatalf("incorrect sample identity: %#v", row)
	}
	id := row["__row_id"]
	wire.SampleColumnRoles = nil
	original, err := svc.queryResultEngine(context.Background(), wire)
	if err != nil {
		t.Fatal(err)
	}
	if original.Items[0]["__row_id"] != id || original.Items[0]["Genotype"] != "0/0" {
		t.Fatal("source or row identity changed")
	}
	dataset.ObjectSHA256 = strings.Repeat("0", 64)
	if attachSampleColumnRoles(&dataset, manifest) == nil {
		t.Fatal("accepted another object")
	}
}
