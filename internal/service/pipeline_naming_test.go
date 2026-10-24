package service

import (
	"context"
	"github.com/SchemaBio/Octopus/internal/model"
	"strings"
	"testing"
)

func TestCanonicalBuiltinNamingAndLegacyHG19Aliases(t *testing.T) {
	for _, tc := range []struct{ old, id, template string }{{"builtin-wes-single", "builtin-wes-single-hg19", "single"}, {"builtin-wes-family", "builtin-wes-family-hg19", "trio"}} {
		for _, id := range []string{tc.old, tc.id} {
			req := &model.TaskCreateRequest{PipelineID: id}
			if _, err := (&TaskService{}).resolveAnalysisPipeline(req, model.OverlayActor{}); err != nil {
				t.Fatal(err)
			}
			if req.PipelineID != tc.id || req.Template != tc.template || req.Inputs["reference_genome"] != "hg19" {
				t.Fatalf("bad alias resolution: %+v", req)
			}
			row, err := (&PipelineService{}).GetPipeline(context.Background(), id, model.OverlayActor{})
			if err != nil || row.ID != tc.id {
				t.Fatalf("alias catalog lookup: %+v %v", row, err)
			}
			if !model.IsBuiltinPipelineID(id) {
				t.Fatal("legacy ID no longer read-only")
			}
		}
	}
	for _, row := range builtinPipelineResponses() {
		if !strings.HasSuffix(row.Name, "_"+row.ReferenceGenome) {
			t.Fatalf("name lacks genome suffix: %+v", row)
		}
		if row.BEDAssetID != "" && !strings.HasSuffix(row.BEDFile, "_"+row.ReferenceGenome) {
			t.Fatal(row.BEDFile)
		}
	}
}
