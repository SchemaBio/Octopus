package workflow

import (
	"regexp"
	"sort"
	"strings"

	"github.com/SchemaBio/Octopus/internal/model"
)

const ProgressProfileVersion = "germline-v2-progress-1"

type progressStageProfile struct {
	code, label string
	weight      int
	calls       []string
}

type progressProfile struct{ stages []progressStageProfile }

var progressProfiles = map[string]progressProfile{
	"germline_single": {stages: []progressStageProfile{
		{"preparation", "准备", 3, []string{"FixBed", "TargetBed"}},
		{"alignment", "预处理与比对", 30, []string{"Fastp", "BwaAlign", "Markdup"}},
		{"quality_control", "质控", 8, []string{"SamtoolsSexCheck", "Xamdst", "MtXamdst", "CollectQCMetrics", "FingerPrint", "QCReport"}},
		{"small_variants", "SNV/Indel 分析", 34, []string{"DeepVariant", "SplitVcfHap", "Whatshap", "UniversalMergeVcfsHap", "LeftAlignAndTrimVariants", "SplitVcf", "VEP_Parallel", "UniversalMergeVcfs", "SNPInDelReport"}},
		{"secondary_analysis", "MT/CNV/ROH/MEI/STR 分析", 22, []string{"CreateMitoBed", "MitochondrialMutect2", "MtVEP", "MTReport", "CNVKitAntitarget", "CNVKitCoverage", "CNVKitFix", "CNVGene", "CNVRegion", "CNVAnnoGene", "CNVAnnoRegion", "AutoMap", "ROHReport", "TIEA_WES", "MeiVEP", "MEIReport", "ExpansionHunter", "Stranger", "STRReport"}},
	}},
	"germline_trio": {stages: []progressStageProfile{
		{"preparation", "准备", 3, []string{"FixBed", "TargetBed"}},
		{"alignment", "三成员预处理与比对", 34, []string{"Fastp", "BwaAlign", "Markdup"}},
		{"quality_control", "三成员质控", 10, []string{"SamtoolsSexCheck", "Xamdst", "MtXamdst", "CollectQCMetrics", "FingerPrint", "QCReport"}},
		{"family_variants", "家系变异分析", 28, []string{"DeepVariant", "GLNexus", "Peddy", "SplitVcfHap", "Whatshap", "UniversalMergeVcfsHap", "LeftAlignAndTrimVariants", "SplitVcf", "VEP_Parallel", "UniversalMergeVcfs", "SNPInDelReport"}},
		{"secondary_analysis", "MT/CNV/ROH/UPD/MEI/STR 分析", 22, []string{"CreateMitoBed", "MitochondrialMutect2", "MtVEP", "MTReport", "CNVKitAntitarget", "CNVKitCoverage", "CNVKitFix", "CNVGene", "CNVRegion", "CNVAnnoGene", "CNVAnnoRegion", "AutoMap", "ROHReport", "AutoMapParent1", "ROHReportParent1", "AutoMapParent2", "ROHReportParent2", "UPD", "TIEA_WES", "MeiVEP", "MEIReport", "ExpansionHunter", "Stranger", "STRReport"}},
	}},
	"germline_baseline": {stages: []progressStageProfile{
		{"preparation", "准备", 5, []string{"FixBed", "TargetBed", "CNVKitAntitarget"}},
		{"preprocessing", "FASTQ 预处理", 10, []string{"Fastp"}},
		{"alignment", "比对与去重", 55, []string{"BwaAlign", "Markdup"}},
		{"coverage", "覆盖度计算", 17, []string{"CNVKitCoverage"}},
		{"reference", "参考基线生成", 10, []string{"CNVKitReference"}},
	}},
	"germline_baseline_fix": {stages: []progressStageProfile{
		{"preparation", "输入检查与准备", 5, []string{"ValidateInputs", "FixBed", "TargetBed", "CNVKitAntitarget"}},
		{"preprocessing", "FASTQ 预处理", 10, []string{"Fastp"}},
		{"alignment", "比对与去重", 55, []string{"BwaAlign", "Markdup"}},
		{"coverage", "覆盖度计算", 14, []string{"CNVKitCoverage"}},
		{"new_reference", "新批次参考生成", 7, []string{"NewBatchReference"}},
		{"reference_merge", "基线合并", 6, []string{"UpdateReference"}},
	}},
}

var scatterSuffix = regexp.MustCompile(`_[0-9]+$`)

// NormalizeCallAlias reduces MiniWDL task names to their WDL call alias.
func NormalizeCallAlias(name string) string {
	name = strings.TrimSpace(name)
	for _, separator := range []string{"/", "\\", ".", ":", "#"} {
		if index := strings.LastIndex(name, separator); index >= 0 {
			name = name[index+len(separator):]
		}
	}
	name = strings.TrimPrefix(name, "call-")
	name = strings.TrimPrefix(name, "call_")
	return scatterSuffix.ReplaceAllString(name, "")
}

// CalculateAnalysisProgress maps observed logical calls to a fixed WDL profile.
// Unobserved calls retain their fixed denominator; scatter instances share an
// alias's weight and are averaged. The caller persists max(previous, Percent).
func CalculateAnalysisProgress(template string, tasks []model.SepiidaTask, workflowSucceeded bool) *model.AnalysisProgress {
	profile, ok := progressProfiles[normalizeProgressTemplate(template)]
	if !ok {
		return &model.AnalysisProgress{Percent: 1, ProfileVersion: ProgressProfileVersion, ActiveStages: []string{}, Stages: []model.AnalysisStage{}}
	}

	observed := make(map[string][]model.SepiidaStatus)
	for _, task := range tasks {
		alias := NormalizeCallAlias(task.JobName)
		if alias == "" {
			alias = NormalizeCallAlias(task.Name)
		}
		if alias != "" {
			observed[alias] = append(observed[alias], task.Status)
		}
	}

	result := &model.AnalysisProgress{ProfileVersion: ProgressProfileVersion, ActiveStages: []string{}}
	weighted := 0.0
	for _, stage := range profile.stages {
		stageDone := 0.0
		active, allSucceeded := false, true
		for _, alias := range stage.calls {
			statuses := observed[alias]
			if len(statuses) == 0 {
				allSucceeded = false
				continue
			}
			callDone := 0.0
			for _, status := range statuses {
				switch status {
				case model.SepiidaStatusSuccess:
					callDone++
				case model.SepiidaStatusRunning:
					callDone += 0.15
					active = true
					allSucceeded = false
				default:
					allSucceeded = false
				}
			}
			stageDone += (callDone / float64(len(statuses))) / float64(len(stage.calls))
		}
		stagePercent := int(stageDone * 100)
		status := "pending"
		if allSucceeded {
			status = "success"
			stagePercent = 100
		} else if active {
			status = "running"
			result.ActiveStages = append(result.ActiveStages, stage.code)
		}
		weighted += float64(stage.weight) * stageDone
		result.Stages = append(result.Stages, model.AnalysisStage{Code: stage.code, Label: stage.label, Weight: stage.weight, Percent: stagePercent, Status: status})
	}
	percent := 1 + int(weighted)
	if workflowSucceeded {
		percent = 98
		for i := range result.Stages {
			result.Stages[i].Percent = 100
			result.Stages[i].Status = "success"
		}
		result.ActiveStages = []string{}
	}
	if percent > 98 {
		percent = 98
	}
	result.Percent = percent
	return result
}

func normalizeProgressTemplate(template string) string {
	value := strings.TrimSpace(template)
	if _, ok := progressProfiles[value]; ok {
		return value
	}
	switch strings.ToLower(value) {
	case "single", "singlewes":
		return "germline_single"
	case "trio", "triowes":
		return "germline_trio"
	case "baseline", "cnvbaseline":
		return "germline_baseline"
	case "baseline_fix", "cnvbaselinefix":
		return "germline_baseline_fix"
	default:
		return ""
	}
}

// ValidateProgressProfiles guards accidental duplicate/missing call weighting.
func ValidateProgressProfiles(expected map[string][]string) []string {
	var problems []string
	for profileName, want := range expected {
		profile, ok := progressProfiles[profileName]
		if !ok {
			problems = append(problems, profileName+": profile missing")
			continue
		}
		counts := map[string]int{}
		weight := 0
		for _, stage := range profile.stages {
			weight += stage.weight
			for _, call := range stage.calls {
				counts[call]++
			}
		}
		if weight != 97 {
			problems = append(problems, profileName+": stage weights must total 97")
		}
		wantSet := map[string]bool{}
		for _, call := range want {
			wantSet[call] = true
		}
		for call, count := range counts {
			if count != 1 {
				problems = append(problems, profileName+": call weighted more than once: "+call)
			}
			if !wantSet[call] {
				problems = append(problems, profileName+": unexpected call: "+call)
			}
		}
		for call := range wantSet {
			if counts[call] == 0 {
				problems = append(problems, profileName+": unweighted call: "+call)
			}
		}
	}
	sort.Strings(problems)
	return problems
}
