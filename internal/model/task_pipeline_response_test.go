package model

import "testing"

func TestTaskPipelineIdentityComesOnlyFromSavedSnapshot(t *testing.T) {
	for _, tc := range []struct{ snapshot, expected string }{
		{`{"pipeline_id":"builtin-wes-single-hg19","reference":"hg19"}`, "builtin-wes-single-hg19"},
		{`{"pipeline_id":"custom-uuid"}`, "custom-uuid"},
		{`{}`, ""}, {`not-json`, ""},
	} {
		task := &Task{Pipeline: "WES单样本分析_hg19", PipelineSnapshotJSON: tc.snapshot}
		if task.ToResponse().PipelineID != tc.expected || task.ToDetailResponse().PipelineID != tc.expected {
			t.Fatalf("unexpected pipeline identity from snapshot %q", tc.snapshot)
		}
		if task.ToResponse().Pipeline != task.Pipeline || task.ToDetailResponse().Pipeline != task.Pipeline {
			t.Fatal("stored pipeline name changed")
		}
	}
}
