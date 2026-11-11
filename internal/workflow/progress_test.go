package workflow

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestProgressProfilesCoverKnownWDLCallsExactlyOnce(t *testing.T) {
	expected := map[string][]string{
		"germline_single":       {"FixBed", "TargetBed", "Fastp", "BwaAlign", "Markdup", "SamtoolsSexCheck", "Xamdst", "CreateMitoBed", "MtXamdst", "CollectQCMetrics", "FingerPrint", "QCReport", "DeepVariant", "SplitVcfHap", "Whatshap", "UniversalMergeVcfsHap", "LeftAlignAndTrimVariants", "SplitVcf", "VEP_Parallel", "UniversalMergeVcfs", "SNPInDelReport", "MitochondrialMutect2", "MtVEP", "MTReport", "CNVKitAntitarget", "CNVKitCoverage", "CNVKitFix", "CNVGene", "CNVRegion", "CNVAnnoGene", "CNVAnnoRegion", "AutoMap", "ROHReport", "TIEA_WES", "MeiVEP", "MEIReport", "ExpansionHunter", "Stranger", "STRReport"},
		"germline_trio":         {"FixBed", "TargetBed", "Fastp", "BwaAlign", "Markdup", "SamtoolsSexCheck", "Xamdst", "CreateMitoBed", "MtXamdst", "CollectQCMetrics", "FingerPrint", "QCReport", "DeepVariant", "GLNexus", "Peddy", "SplitVcfHap", "Whatshap", "UniversalMergeVcfsHap", "LeftAlignAndTrimVariants", "SplitVcf", "VEP_Parallel", "UniversalMergeVcfs", "SNPInDelReport", "MitochondrialMutect2", "MtVEP", "MTReport", "CNVKitAntitarget", "CNVKitCoverage", "CNVKitFix", "CNVGene", "CNVRegion", "CNVAnnoGene", "CNVAnnoRegion", "AutoMap", "ROHReport", "AutoMapParent1", "ROHReportParent1", "AutoMapParent2", "ROHReportParent2", "UPD", "TIEA_WES", "MeiVEP", "MEIReport", "ExpansionHunter", "Stranger", "STRReport"},
		"germline_baseline":     {"ValidateInputs", "FixBed", "TargetBed", "CNVKitAntitarget", "Fastp", "BwaAlign", "Markdup", "CNVKitCoverage", "CNVKitReference"},
		"germline_baseline_fix": {"ValidateInputs", "FixBed", "TargetBed", "CNVKitAntitarget", "Fastp", "BwaAlign", "Markdup", "CNVKitCoverage", "NewBatchReference", "UpdateReference"},
	}
	if problems := ValidateProgressProfiles(expected); len(problems) > 0 {
		t.Fatalf("invalid WDL progress profile: %v", problems)
	}
}

func TestReferenceBinPreparationKeepsCalibrationProgress(t *testing.T) {
	progress := CalculateAnalysisProgress("baseline_fix", []model.SepiidaTask{
		{JobName: "call-ValidateInputs", Status: model.SepiidaStatusSuccess},
		{JobName: "call-FixBed", Status: model.SepiidaStatusSuccess},
		{JobName: "call-ReferenceBins", Status: model.SepiidaStatusSuccess},
	}, false)
	if progress.Stages[0].Percent != 100 || progress.Stages[0].Status != "success" {
		t.Fatalf("consolidated reference preparation was not counted: %+v", progress.Stages[0])
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

func TestTrioProbandFamilyPreparationSharesExistingProgressWeight(t *testing.T) {
	legacyTasks := []model.SepiidaTask{{JobName: "call-GLNexus", Status: model.SepiidaStatusSuccess}}
	legacy := CalculateAnalysisProgress("trio", legacyTasks, false)
	running := CalculateAnalysisProgress("trio", append(legacyTasks, model.SepiidaTask{
		JobName: "call-ProbandFamilyVCF", Status: model.SepiidaStatusRunning,
	}), false)
	if running.Stages[3].Status != "running" || len(running.ActiveStages) != 1 || running.ActiveStages[0] != "family_variants" {
		t.Fatalf("proband family preparation is not visible: %+v", running)
	}
	done := CalculateAnalysisProgress("trio", append(legacyTasks, model.SepiidaTask{
		JobName: "call-ProbandFamilyVCF", Status: model.SepiidaStatusSuccess,
	}), false)
	if done.Percent != legacy.Percent || done.Stages[3].Percent != legacy.Stages[3].Percent {
		t.Fatalf("new preparation changed the legacy stage weight: legacy=%+v new=%+v", legacy, done)
	}
}

func TestNormalizeCallAlias(t *testing.T) {
	for input, want := range map[string]string{
		"call-Fastp_2":         "Fastp",
		"SingleWES.Fastp_0":    "Fastp",
		"workflow/Whatshap":    "Whatshap",
		"call-Fastp-0":         "Fastp",
		"call-Markdup-1":       "Markdup",
		"call-VEP_Parallel-23": "VEP_Parallel",
		"call-Whatshap-02":     "Whatshap",
		"call-Nested-0-12":     "Nested",
	} {
		if got := NormalizeCallAlias(input); got != want {
			t.Errorf("NormalizeCallAlias(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTrioProgressCountsActualMiniWDLScatterNames(t *testing.T) {
	var tasks []model.SepiidaTask
	for _, alias := range []string{"Fastp", "BwaAlign", "Markdup", "SamtoolsSexCheck", "Xamdst", "MtXamdst", "CollectQCMetrics", "FingerPrint", "QCReport"} {
		for _, shard := range []string{"0", "1", "2"} {
			tasks = append(tasks, model.SepiidaTask{JobName: "call-" + alias + "-" + shard, Status: model.SepiidaStatusSuccess})
		}
	}
	tasks = append(tasks, model.SepiidaTask{JobName: "call-VEP_Parallel-00", Status: model.SepiidaStatusRunning})
	progress := CalculateAnalysisProgress("trio", tasks, false)
	if progress.Stages[1].Percent != 100 || progress.Stages[2].Percent != 100 || progress.Percent < 45 {
		t.Fatalf("completed trio alignment/QC must count: %+v", progress)
	}
	if len(progress.ActiveStages) != 1 || progress.ActiveStages[0] != "family_variants" {
		t.Fatalf("VEP shard must activate family analysis: %+v", progress.ActiveStages)
	}
}
