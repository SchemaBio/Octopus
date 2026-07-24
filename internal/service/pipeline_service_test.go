package service

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestBuiltinPipelineCatalogContainsOnlySupportedWESFlows(t *testing.T) {
	items := builtinPipelineResponses()
	if len(items) != 4 {
		t.Fatalf("expected exactly four built-in pipelines, got %d", len(items))
	}
	if items[0].ID != model.BuiltinPipelineWESSingleID || items[0].Template != "single" || !items[0].IsBuiltin {
		t.Fatalf("unexpected single-sample built-in: %+v", items[0])
	}
	if items[1].ID != model.BuiltinPipelineWESFamilyID || items[1].Template != "trio" || !items[1].IsBuiltin {
		t.Fatalf("unexpected family built-in: %+v", items[1])
	}
	if items[0].BEDAssetID != model.BuiltinBEDHG19ID || items[1].CNVBaselineID != model.BuiltinCNVBaselineHG19ID {
		t.Fatalf("hg19 built-ins must use hg19 placeholder resources: %+v %+v", items[0], items[1])
	}
	if items[2].ID != model.BuiltinPipelineWESSingleHG38ID || items[2].ReferenceGenome != "hg38" || items[2].BEDAssetID != model.BuiltinBEDHG38ID {
		t.Fatalf("unexpected hg38 single-sample built-in: %+v", items[2])
	}
	if items[3].ID != model.BuiltinPipelineWESFamilyHG38ID || items[3].ReferenceGenome != "hg38" || items[3].CNVBaselineID != model.BuiltinCNVBaselineHG38ID {
		t.Fatalf("unexpected hg38 family built-in: %+v", items[3])
	}
	if isAllowedPipelineBase(model.PipelineBaseType("panel")) {
		t.Fatal("panel must not be accepted as a pipeline base")
	}
}

func TestResolveHG38BuiltinPipelineForcesGenomeAndTemplate(t *testing.T) {
	request := &model.TaskCreateRequest{PipelineID: model.BuiltinPipelineWESSingleHG38ID, Inputs: map[string]interface{}{"reference_genome": "hg19"}}
	service := &TaskService{}
	if _, err := service.resolveAnalysisPipeline(request, model.OverlayActor{}); err != nil {
		t.Fatalf("resolve hg38 built-in: %v", err)
	}
	if request.Template != "single" || request.Inputs["reference_genome"] != "hg38" {
		t.Fatalf("hg38 built-in did not force server-side template/genome: %+v", request)
	}
}

func TestResolvePipelineBaseInheritsReferenceGenome(t *testing.T) {
	tests := []struct {
		id       string
		baseType model.PipelineBaseType
		genome   string
	}{
		{model.BuiltinPipelineWESSingleID, model.PipelineBaseWESSingle, "hg19"},
		{model.BuiltinPipelineWESFamilyID, model.PipelineBaseWESFamily, "hg19"},
		{model.BuiltinPipelineWESSingleHG38ID, model.PipelineBaseWESSingle, "hg38"},
		{model.BuiltinPipelineWESFamilyHG38ID, model.PipelineBaseWESFamily, "hg38"},
	}
	for _, test := range tests {
		baseType, genome, err := resolvePipelineBase(test.id, "", "")
		if err != nil {
			t.Fatalf("resolve %s: %v", test.id, err)
		}
		if baseType != test.baseType || genome != test.genome {
			t.Fatalf("resolve %s = %s/%s, want %s/%s", test.id, baseType, genome, test.baseType, test.genome)
		}
	}
	if _, _, err := resolvePipelineBase("custom-pipeline", "", ""); err == nil {
		t.Fatal("custom pipeline must not be accepted as a base pipeline")
	}
}
