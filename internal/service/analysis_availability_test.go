package service

import (
	"github.com/SchemaBio/Octopus/internal/model"
	"testing"
)

func TestFamilyAnalysisAvailableForBothGenomes(t *testing.T) {
	for _, tc := range []struct{ id, genome string }{{model.BuiltinPipelineWESFamilyID, "hg19"}, {model.BuiltinPipelineWESFamilyHG38ID, "hg38"}} {
		req := &model.TaskCreateRequest{PipelineID: tc.id}
		if _, err := (&TaskService{}).resolveAnalysisPipeline(req, model.OverlayActor{}); err != nil {
			t.Fatal(err)
		}
		if req.Template != "trio" || req.Inputs["reference_genome"] != tc.genome {
			t.Fatalf("incorrect resolution: %+v", req)
		}
	}
	for _, row := range builtinPipelineResponses() {
		if row.Template == "trio" && (row.Status != model.PipelineStatusActive || !row.ResourceAvailable) {
			t.Fatal("Trio unavailable")
		}
	}
}
