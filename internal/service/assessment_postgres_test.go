package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAssessmentWorkflowPostgresSaveAdoptConflictAndReadOnly(t *testing.T) {
	db := organizationResetDB(t)
	task := &model.Task{UUID: uuid.NewString(), Status: model.TaskStatusCompleted, InputJSON: "{}"}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, task.UUID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(dir, "sample.snv_indel.txt")
	if err := os.WriteFile(report, []byte("Chromosome\tPosition\tRef\tAlt\tType\tGene\tConsequence\tTranscript\n17\t10544634\tC\tT\tSNP\tMYH3\tmissense variant\tNM_002470.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(map[string]interface{}{"summary": map[string]string{"snv": report}})
	if err := os.WriteFile(filepath.Join(dir, "outputs.resolved.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Edition: "opensource", Storage: config.StorageConfig{Provider: "local"}, Task: config.TaskConfig{ArchiveDir: root}}
	cfg.ResultQuery.CacheDir = t.TempDir()
	s := &ResultService{cfg: cfg}
	ctx := context.Background()
	d, err := s.ensureParquetDataset(ctx, task, model.TenantIDForTask(task), executionAttempt(task), "snv-indel")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(d).Update("rows", 1).Error; err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(d.ID+"/"+d.ObjectSHA256+"/0")))
	ordinal := int64(0)
	save := func(version uint64, change map[string]interface{}) (*model.ResultRowAdjustment, error) {
		r, _, e := s.SaveParquetRowAdjustment(ctx, task, "snv-indel", id, "synthetic-reviewer", model.ResultRowAdjustmentRequest{ExpectedVersion: version, Adjustments: change, RowOrdinal: &ordinal, AttemptID: executionAttempt(task), DatasetVersion: d.DataVersion})
		return r, e
	}
	evidence := []interface{}{map[string]interface{}{"code": "PP3", "strength": "supporting", "source": "manual"}}
	first, err := save(0, map[string]interface{}{"acmgEvidence": evidence, "activeAcmgVersion": "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := save(first.Version, map[string]interface{}{"acmgEvidence": []interface{}{map[string]interface{}{"code": "BA1", "strength": "standalone"}}})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	_ = json.Unmarshal([]byte(next.PayloadJSON), &payload)
	row := map[string]interface{}{}
	applyActiveACMG(row, payload)
	if row["acmgClassification"] != "VUS" || payload["acmgClassification"] != "Benign" {
		t.Fatal("save-only changed adopted classification", row)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := save(next.Version, map[string]interface{}{"activeAcmgVersion": "legacy"})
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for e := range results {
		if e == nil {
			successes++
		} else if errors.Is(e, ErrAdjustmentConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("optimistic transaction conflict failed", successes, conflicts)
	}
	now := time.Now()
	if err := db.Model(task).Update("interpretation_completed_at", now).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := save(next.Version+1, map[string]interface{}{"activeAcmgVersion": "legacy"}); !errors.Is(err, ErrInterpretationCompleted) {
		t.Fatal("read-only save accepted", err)
	}
}
