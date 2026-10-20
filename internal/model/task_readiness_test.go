package model

import "testing"

func TestTaskResponsesExposeResultReadiness(t *testing.T) {
	for _, state := range []ResultImportStatus{ResultImportStatusPending, ResultImportStatusRunning, ResultImportStatusSuccess, ResultImportStatusFailed} {
		task := Task{ResultImportStatus: state}
		if task.ToResponse().ResultImportStatus != state || task.ToDetailResponse().ResultImportStatus != state {
			t.Fatalf("lost result preparation state %s", state)
		}
	}
}
