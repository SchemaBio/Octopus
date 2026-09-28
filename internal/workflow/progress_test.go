package workflow

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestProgressProfilesCoverKnownWDLCallsExactlyOnce(t *testing.T) {
	expected := map[string][]string{
		"germline_single":       {"FixBed", "TargetBed", "Fastp", "BwaAlign", "Markdup", "SamtoolsSexCheck", "Xamdst", "CreateMitoBed", "MtXamdst", "CollectQCMetrics", "FingerPrint", "QCReport", "DeepVariant", "SplitVcfHap", "Whatshap", "UniversalMergeVcfsHap", "LeftAlignAndTrimVariants", "SplitVcf", "VEP_Parallel", "UniversalMergeVcfs", "SNPInDelReport", "MitochondrialMutect2", "MtVEP", "MTReport", "CNVKitAntitarget", "CNVKitCoverage", "CNVKitFix", "CNVGene", "CNVRegion", "CNVAnnoGene", "CNVAnnoRegion", "AutoMap", "ROHReport", "TIEA_WES", "MeiVEP", "MEIReport", "ExpansionHunter", "Stranger", "STRReport"},
		"germline_trio":         {"FixBed", "TargetBed", "Fastp", "BwaAlign", "Markdup", "SamtoolsSexCheck", "Xamdst", "CreateMitoBed", "MtXamdst", "CollectQCMetrics", "FingerPrint", "QCReport", "DeepVariant", "GLNexus", "Peddy", "SplitVcfHap", "Whatshap", "UniversalMergeVcfsHap", "LeftAlignAndTrimVariants", "SplitVcf", "VEP_Parallel", "UniversalMergeVcfs", "SNPInDelReport", "MitochondrialMutect2", "MtVEP", "MTReport", "CNVKitAntitarget", "CNVKitCoverage", "CNVKitFix", "CNVGene", "CNVRegion", "CNVAnnoGene", "CNVAnnoRegion", "AutoMap", "ROHReport", "AutoMapParent1", "ROHReportParent1", "AutoMapParent2", "ROHReportParent2", "UPD", "TIEA_WES", "MeiVEP", "MEIReport", "ExpansionHunter", "Stranger", "STRReport"},
		"germline_baseline":     {"FixBed", "TargetBed", "CNVKitAntitarget", "Fastp", "BwaAlign", "Markdup", "CNVKitCoverage", "CNVKitReference"},
		"germline_baseline_fix": {"ValidateInputs", "FixBed", "TargetBed", "CNVKitAntitarget", "Fastp", "BwaAlign", "Markdup", "CNVKitCoverage", "NewBatchReference", "UpdateReference"},
	}
	if problems := ValidateProgressProfiles(expected); len(problems) > 0 {
		t.Fatalf("invalid WDL progress profile: %v", problems)
	}
}

func TestCalculateAnalysisProgressUsesFixedWeightsAndScatterAverage(t *testing.T) {
	progress := CalculateAnalysisProgress("germline_single", []model.SepiidaTask{
		{JobName: "SingleWES.Fastp_0", Status: model.SepiidaStatusSuccess},
		{JobName: "SingleWES.Fastp_1", Status: model.SepiidaStatusRunning},
		{JobName: "SingleWES.DeepVariant", Status: model.SepiidaStatusRunning},
	}, false)
	if progress.ProfileVersion != ProgressProfileVersion {
		t.Fatalf("profile version = %q", progress.ProfileVersion)
	}
	if progress.Percent < 1 || progress.Percent > 98 {
		t.Fatalf("running progress outside 1-98%%: %d", progress.Percent)
	}
	if len(progress.ActiveStages) != 2 || progress.ActiveStages[0] != "alignment" || progress.ActiveStages[1] != "small_variants" {
		t.Fatalf("unexpected active stages: %#v", progress.ActiveStages)
	}
	if got := progress.Stages[1].Percent; got != 19 {
		t.Fatalf("scatter call progress = %d%%, want 19%%", got)
	}
	if got := progress.Stages[0].Weight + progress.Stages[1].Weight + progress.Stages[2].Weight + progress.Stages[3].Weight + progress.Stages[4].Weight; got != 97 {
		t.Fatalf("weights total %d, want 97", got)
	}
}

func TestCalculateAnalysisProgressCompletionAndUnknownTemplate(t *testing.T) {
	complete := CalculateAnalysisProgress("germline_baseline_fix", nil, true)
	if complete.Percent != 98 {
		t.Fatalf("computed workflow completion = %d, want 98", complete.Percent)
	}
	unknown := CalculateAnalysisProgress("future-workflow", []model.SepiidaTask{{JobName: "NewCall", Status: model.SepiidaStatusSuccess}}, false)
	if unknown.Percent != 1 || unknown.ProfileVersion != ProgressProfileVersion {
		t.Fatalf("unknown profile must stay at 1%%: %+v", unknown)
	}
}

func TestNormalizeCallAlias(t *testing.T) {
	for input, want := range map[string]string{
		"call-Fastp_2":      "Fastp",
		"SingleWES.Fastp_0": "Fastp",
		"workflow/Whatshap": "Whatshap",
	} {
		if got := NormalizeCallAlias(input); got != want {
			t.Errorf("NormalizeCallAlias(%q) = %q, want %q", input, got, want)
		}
	}
}
