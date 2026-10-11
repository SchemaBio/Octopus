package model

import "testing"

func TestTaskResponsesPreserveWorkflowTemplate(t *testing.T) {
	for _, template := range []string{"baseline_fix", "single", "trio"} {
		task := &Task{Template: template}
		if task.ToResponse().Template != template || task.ToDetailResponse().Template != template {
			t.Fatalf("task response lost workflow type %q", template)
		}
	}
}
