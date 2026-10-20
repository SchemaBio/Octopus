package service

import (
	"context"
	"errors"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestFamilyAnalysisUnavailableBeforeTaskOrPipelineAdmission(t *testing.T) {
	for _, id := range []string{model.BuiltinPipelineWESFamilyID, model.BuiltinPipelineWESFamilyHG38ID} {
		s := &TaskService{}
		_, err := s.resolveAnalysisPipeline(&model.TaskCreateRequest{PipelineID: id}, model.OverlayActor{})
		if !errors.Is(err, ErrFamilyAnalysisUnavailable) {
			t.Fatalf("family task accepted: %v", err)
		}
		_, err = s.EstimateTask(&model.TaskEstimateRequest{PipelineID: id}, model.OverlayActor{})
		if !errors.Is(err, ErrFamilyAnalysisUnavailable) {
			t.Fatalf("family estimate accepted: %v", err)
		}
		_, err = (&PipelineService{}).CreatePipeline(context.Background(), &model.PipelineCreateRequest{Name: "family", BasePipelineID: id}, model.OverlayActor{})
		if !errors.Is(err, ErrFamilyAnalysisUnavailable) {
			t.Fatalf("family organization pipeline accepted: %v", err)
		}
	}
	for _, template := range []string{"trio", "germline_trio"} {
		_, err := (&TaskService{}).buildCVMDispatchRequestForContract(context.Background(), model.OverlayActor{}, &model.Task{Template: template}, "germline-v2")
		if !errors.Is(err, ErrFamilyAnalysisUnavailable) {
			t.Fatalf("family cloud execution admitted: %v", err)
		}
	}
	for _, row := range builtinPipelineResponses() {
		if row.Template == "trio" && (row.Status != model.PipelineStatusInactive || row.ResourceAvailable || row.ResourceError == "") {
			t.Fatalf("family entry is not disabled: %+v", row)
		}
		if row.Template != "trio" && (row.Status != model.PipelineStatusActive || !row.ResourceAvailable) {
			t.Fatalf("released entry was disabled: %+v", row)
		}
	}
	custom := (&PipelineService{}).toResponse(&model.Pipeline{ID: "org-family", BaseType: model.PipelineBaseWESFamily, Status: model.PipelineStatusActive}, nil, nil)
	if custom.Status != model.PipelineStatusInactive || custom.ResourceAvailable {
		t.Fatal("existing family organization flow still offered")
	}
}
