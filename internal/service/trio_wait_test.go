package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestLocalTrioWaitsAndBindsSavedFamilyAtStart(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	old := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = old })
	root := t.TempDir()
	cfg := &config.Config{Edition: "opensource", Storage: config.StorageConfig{Provider: "local", LocalDir: root}}
	svc := NewTaskService(cfg)
	actor := model.OverlayActor{SharedInstance: true, UserID: 1}
	samples := []model.Sample{
		{UUID: "child", InternalID: "CHILD"},
		{UUID: "father", InternalID: "FATHER", Gender: model.SampleGenderMale},
		{UUID: "mother", InternalID: "MOTHER", Gender: model.SampleGenderFemale},
	}
	inputs := map[string]interface{}{}
	_, assets, err := svc.prepareTrioMembers(samples, []string{"affected", "carrier", "unaffected"}, "trio-task", actor, inputs)
	if err != nil || len(assets) != 0 || inputs[trioWaitingForFASTQ] != true {
		t.Fatalf("unmatched local family must be creatable: %v, %v", assets, err)
	}
	converted, err := buildCVMWDLInputs("trio", "hg19", inputs)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := converted["TrioWES.meta_info"]; ok || converted[trioWaitingForFASTQ] != true {
		t.Fatal("waiting task must persist its state without catalog example FASTQs")
	}
	saved, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}

	for i := range samples {
		r1 := filepath.Join(root, samples[i].UUID+"_R1.fq")
		r2 := filepath.Join(root, samples[i].UUID+"_R2.fq")
		for _, path := range []string{r1, r2} {
			if err := os.WriteFile(path, []byte("@read\nACGT\n+\nIIII\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		samples[i].SetAutoMatchedPair(&model.MatchedPair{R1Path: r1, R2Path: r2})
	}
	for _, ready := range []bool{false, true} {
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT .*FROM "tasks".*FOR UPDATE`).WillReturnRows(
			sqlmock.NewRows([]string{"id", "uuid", "status", "executor", "template", "sample_id", "created_by", "input_json"}).
				AddRow("trio-id", "trio-task", model.TaskStatusWaitingData, model.ExecutorLocal, "trio", "child", 1, string(saved)))
		for range 2 {
			mock.ExpectQuery(`SELECT .*FROM "task_data_assets"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		}
		for i, sample := range samples {
			pair := sample.AutoMatchedPair
			if !ready && i == 2 {
				pair = "null"
			}
			// The original UUIDs must be queried, and mutable names are ignored
			// for the snapshot and pedigree written with the analysis.
			mock.ExpectQuery(`SELECT .*FROM "samples"`).WithArgs(sample.UUID, 1).WillReturnRows(
				sqlmock.NewRows([]string{"id", "uuid", "internal_id", "auto_matched_pair"}).
					AddRow(i+1, sample.UUID, "renamed-"+sample.UUID, pair))
		}
		if ready {
			assetRows := func(id int, readType model.ReadType) *sqlmock.Rows {
				return sqlmock.NewRows([]string{"id", "uuid", "status", "provider", "read_type", "file_size"}).
					AddRow(id, fmt.Sprintf("asset-%d", id), model.FileStatusCompleted, model.UploadProviderLocal, readType, 18)
			}
			for i, sample := range samples {
				pair := sample.GetMatchedPair()
				mock.ExpectQuery(`SELECT .*FROM "data_assets"`).WithArgs(pair.R1Path, model.FileStatusCompleted, 1).
					WillReturnRows(assetRows(i*2+1, model.ReadTypeRead1))
				mock.ExpectQuery(`SELECT .*FROM "data_assets"`).WithArgs(pair.R2Path, model.FileStatusCompleted, 1).
					WillReturnRows(assetRows(i*2+2, model.ReadTypeRead2))
			}
			for i := range 6 {
				mock.ExpectQuery(`SELECT .*FROM "data_assets".*FOR UPDATE`).WillReturnRows(assetRows(i+1, model.ReadTypeRead1))
			}
			mock.ExpectQuery(`INSERT INTO "task_data_assets"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2).AddRow(3).AddRow(4).AddRow(5).AddRow(6))
			mock.ExpectExec(`UPDATE "tasks"`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
		} else {
			mock.ExpectRollback()
		}
		task, _, err := svc.claimLocalStart(context.Background(), "trio-task")
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		if !ready {
			if !errors.Is(err, errTrioFASTQPending) || task != nil {
				t.Fatalf("missing mother FASTQs must block start: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var bound struct {
			Members []model.TrioMemberSnapshot `json:"trio_members"`
			Pending bool                       `json:"trio_waiting_for_fastq"`
			PED     string                     `json:"TrioWES.ped_content"`
		}
		if err := json.Unmarshal([]byte(task.InputJSON), &bound); err != nil {
			t.Fatal(err)
		}
		if task.ExecutionPhase != "starting" || bound.Pending || len(bound.Members) != 3 ||
			bound.Members[1].InternalID != "FATHER" || bound.Members[2].Read2AssetID != "asset-6" ||
			bound.PED != inputs["TrioWES.ped_content"] {
			t.Fatalf("unexpected bound trio snapshot: %+v", bound)
		}
	}

	cfg.Edition = "saas"
	samples[2].SetAutoMatchedPair(nil)
	if _, _, err := svc.prepareTrioMembers(samples, []string{"affected", "", ""}, "saas-task", actor, map[string]interface{}{}); err == nil {
		t.Fatal("SaaS must still require all matched pairs at submission")
	}
}
