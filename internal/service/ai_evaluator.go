package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"gorm.io/gorm"
)

// AIFilter controls which variants are sent to the LLM
type AIFilter struct {
	// Gene list ID to focus on (most important filter)
	GeneListID string `json:"geneListId"`
	// Specific gene names to focus on (alternative to gene list)
	Genes []string `json:"genes"`
	// Classifications to include (default: Pathogenic, Likely_Pathogenic)
	Classifications []string `json:"classifications"`
	// Max gnomAD AF for P/LP variants (default: 0.001 = 0.1%)
	MaxPLPAF float64 `json:"maxPlpAF"`
	// Max gnomAD AF for VUS (default: 0.0005 = 0.05%)
	MaxVUSAF float64 `json:"maxVusAF"`
	// Max VUS to include (default: 10)
	MaxVUS int `json:"maxVus"`
	// Max total variants to send (default: 80)
	MaxTotal int `json:"maxTotal"`
}

// DefaultAIFilter returns sensible defaults for WES
func DefaultAIFilter() AIFilter {
	return AIFilter{
		Classifications: []string{"Pathogenic", "Likely_Pathogenic"},
		MaxPLPAF:        0.001,
		MaxVUSAF:        0.0005,
		MaxVUS:          10,
		MaxTotal:        80,
	}
}

// AIEvaluator collects task data and sends it to LLM for evaluation
type AIEvaluator struct {
	llm          *LLMClient
	sampleRepo   *repository.SampleRepository
	resultRepo   *repository.ResultRepository
	geneListRepo *repository.GeneListRepository
}

// NewAIEvaluator creates a new AI evaluator
func NewAIEvaluator(cfg *config.Config) *AIEvaluator {
	var llm *LLMClient
	if cfg.LLM.Enabled {
		llm = NewLLMClient(
			cfg.LLM.BaseURL,
			cfg.LLM.APIKey,
			cfg.LLM.Model,
			AllowPrivateLLMEndpoints(!strings.EqualFold(cfg.Server.Mode, "release")),
		)
	}
	return &AIEvaluator{
		llm:          llm,
		sampleRepo:   repository.NewSampleRepository(),
		resultRepo:   repository.NewResultRepository(),
		geneListRepo: repository.NewGeneListRepository(),
	}
}

// IsEnabled returns whether AI evaluation is available
func (e *AIEvaluator) IsEnabled() bool {
	return e.llm != nil
}

// Evaluate streams an LLM evaluation with filtering
func (e *AIEvaluator) Evaluate(ctx context.Context, task *model.Task, actor model.OverlayActor, filter AIFilter, onChunk func(string) error) error {
	task, sampleInfo, qc, snvs, totalRaw, err := e.collectData(task, actor, filter)
	if err != nil {
		return err
	}

	prompt := buildEvaluationPrompt(task, sampleInfo, qc, snvs, totalRaw, filter)

	messages := []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	return e.llm.ChatStream(ctx, messages, onChunk)
}

// EvaluateSync returns the full LLM response with filtering
func (e *AIEvaluator) EvaluateSync(ctx context.Context, task *model.Task, actor model.OverlayActor, filter AIFilter) (string, error) {
	task, sampleInfo, qc, snvs, totalRaw, err := e.collectData(task, actor, filter)
	if err != nil {
		return "", err
	}

	prompt := buildEvaluationPrompt(task, sampleInfo, qc, snvs, totalRaw, filter)

	messages := []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	return e.llm.Chat(ctx, messages)
}

// collectData gathers and filters all data for a task
func (e *AIEvaluator) collectData(authorizedTask *model.Task, actor model.OverlayActor, filter AIFilter) (
	task *model.Task, sampleInfo string, qc *model.QCResult,
	snvs []model.SNVIndel, totalRaw int, err error,
) {
	if authorizedTask == nil {
		err = errors.New("task is required")
		return
	}
	task = authorizedTask

	if task.SampleID != "" {
		sample, serr := e.sampleRepo.FindScopedByUUID(task.SampleID, actor)
		if serr == nil {
			sampleInfo = formatSampleForPrompt(sample)
		} else if !errors.Is(serr, gorm.ErrRecordNotFound) {
			err = fmt.Errorf("failed to load task sample: %w", serr)
			return
		}
	}

	qc, err = e.resultRepo.FindQCByTaskID(task.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		qc = nil
		err = nil
	} else if err != nil {
		err = fmt.Errorf("failed to load task QC result: %w", err)
		return
	}

	allSNVs, snvErr := e.resultRepo.FindSNVIndelsByTaskID(task.ID)
	if snvErr != nil {
		err = fmt.Errorf("failed to load task SNV results: %w", snvErr)
		return
	}
	totalRaw = len(allSNVs)

	// Resolve gene set from filter
	geneSet, resolveErr := e.resolveGeneSet(filter, actor)
	if resolveErr != nil {
		err = resolveErr
		return
	}

	// Apply filters
	snvs = filterVariants(allSNVs, filter, geneSet)
	return
}

// resolveGeneSet builds the gene set from filter (gene list ID or explicit genes)
func (e *AIEvaluator) resolveGeneSet(filter AIFilter, actor model.OverlayActor) (map[string]bool, error) {
	geneSet := make(map[string]bool)

	// Load from gene list ID
	if filter.GeneListID != "" {
		gl, err := e.geneListRepo.FindScopedByStringID(filter.GeneListID, actor)
		if err != nil {
			return nil, fmt.Errorf("gene list not found: %w", err)
		}
		genes, err := gl.ParseGenes()
		if err != nil {
			return nil, fmt.Errorf("invalid gene list data: %w", err)
		}
		for _, g := range genes {
			if gene := strings.TrimSpace(g); gene != "" {
				geneSet[gene] = true
			}
		}
	}

	// Add explicit genes
	for _, g := range filter.Genes {
		if gene := strings.TrimSpace(g); gene != "" {
			geneSet[gene] = true
		}
	}

	return geneSet, nil
}

// ValidateFilter performs access-controlled filter resolution before an SSE
// response is opened, allowing authorization and database failures to use a
// normal HTTP status instead of being buried in a successful event stream.
func (e *AIEvaluator) ValidateFilter(filter AIFilter, actor model.OverlayActor) error {
	_, err := e.resolveGeneSet(filter, actor)
	return err
}

// filterVariants applies multi-level filtering for WES data
func filterVariants(snvs []model.SNVIndel, filter AIFilter, geneSet map[string]bool) []model.SNVIndel {
	// A selected but empty gene list is an explicit filter that matches no
	// variants. Falling back to an unrestricted result would disclose more
	// clinical data to the LLM than the caller requested.
	hasGeneFilter := filter.GeneListID != "" || len(filter.Genes) > 0

	// Defaults
	maxPLPAF := filter.MaxPLPAF
	if maxPLPAF <= 0 {
		maxPLPAF = 0.001 // 0.1%
	}
	maxVUSAF := filter.MaxVUSAF
	if maxVUSAF <= 0 {
		maxVUSAF = 0.0005 // 0.05%
	}
	maxVUS := filter.MaxVUS
	if maxVUS <= 0 {
		maxVUS = 10
	}
	maxTotal := filter.MaxTotal
	if maxTotal <= 0 {
		maxTotal = 80
	}

	// Classify and filter in priority order
	var candidates []scoredVariant

	for _, v := range snvs {
		isPLP := v.ACMGClassification == model.ACMGPathogenic || v.ACMGClassification == model.ACMGLikelyPathogenic
		isVUS := v.ACMGClassification == model.ACMGVUS

		// Skip benign/likely_benign entirely
		if !isPLP && !isVUS {
			continue
		}

		// Gene filter: if specified, skip variants not in gene list
		if hasGeneFilter && !geneSet[v.Gene] {
			continue
		}

		// Get gnomAD AF (use 0 if nil)
		af := 0.0
		if v.GnomadAF != nil {
			af = *v.GnomadAF
		}

		if isPLP {
			// P/LP without gene filter: only rare ones
			if !hasGeneFilter && af >= maxPLPAF {
				continue
			}
			// P/LP get highest priority
			priority := 0
			if v.ACMGClassification == model.ACMGPathogenic {
				priority = 0
			} else {
				priority = 1
			}
			candidates = append(candidates, scoredVariant{v, priority})
		} else if isVUS {
			// VUS: much stricter filtering
			if af >= maxVUSAF {
				continue
			}
			// VUS must be in gene list if specified
			if hasGeneFilter && !geneSet[v.Gene] {
				continue
			}
			// VUS: prioritize LoF variants
			priority := 10
			switch v.Consequence {
			case "frameshift_variant", "stop_gained", "splice_donor_variant", "splice_acceptor_variant":
				priority = 5 // LoF = higher priority
			case "missense_variant":
				priority = 8
			default:
				priority = 10
			}
			candidates = append(candidates, scoredVariant{v, priority})
		}
	}

	// Sort by priority (lower score first), then by gene
	sortVariants(candidates)

	// Apply count limits
	var result []model.SNVIndel
	vusCount := 0

	for _, s := range candidates {
		if len(result) >= maxTotal {
			break
		}
		if s.variant.ACMGClassification == model.ACMGVUS {
			vusCount++
			if vusCount > maxVUS {
				continue
			}
		}
		result = append(result, s.variant)
	}

	return result
}

// scoredVariant wraps a variant with a priority score
type scoredVariant struct {
	variant model.SNVIndel
	score   int
}

// sortVariants sorts by priority score then by gene name
func sortVariants(variants []scoredVariant) {
	for i := 1; i < len(variants); i++ {
		for j := i; j > 0; j-- {
			if variants[j].score < variants[j-1].score ||
				(variants[j].score == variants[j-1].score && variants[j].variant.Gene < variants[j-1].variant.Gene) {
				variants[j], variants[j-1] = variants[j-1], variants[j]
			}
		}
	}
}

const systemPrompt = `你是一位资深的临床遗传学分析师。请根据提供的患者信息、测序质控数据和变异检测结果，给出专业的遗传病分析评估报告。

你的评估应包括：
1. **测序质量评估**：基于QC指标判断数据是否可靠
2. **变异致病性分析**：重点关注已知致病/可能致病变异，对VUS给出分析意见
3. **基因型-表型关联**：结合患者临床信息，评估变异与表型的相关性
4. **建议**：是否需要进一步验证（Sanger测序、家系验证等），是否需要扩大检测范围

请使用中文回复，格式清晰，适当使用markdown。`

func buildEvaluationPrompt(
	task *model.Task, sampleInfo string, qc *model.QCResult,
	snvs []model.SNVIndel, totalRaw int, filter AIFilter,
) string {
	var b strings.Builder

	b.WriteString("## 任务信息\n")
	b.WriteString(fmt.Sprintf("- 任务名称: %s\n", task.Name))
	b.WriteString(fmt.Sprintf("- 分析流程: %s %s\n", task.Pipeline, task.PipelineVersion))
	b.WriteString(fmt.Sprintf("- 创建时间: %s\n\n", task.CreatedAt.Format("2006-01-02 15:04")))

	if sampleInfo != "" {
		b.WriteString("## 患者/样本信息\n")
		b.WriteString(sampleInfo)
		b.WriteString("\n")
	}

	if qc != nil {
		b.WriteString("## 测序质控指标\n")
		b.WriteString(fmt.Sprintf("- 总读数: %d\n", qc.TotalReads))
		b.WriteString(fmt.Sprintf("- 比对率: %.2f%%\n", qc.MappedReadsFraction*100))
		b.WriteString(fmt.Sprintf("- 平均测序深度: %.1fX\n", qc.AverageDepth))
		b.WriteString(fmt.Sprintf("- 去重后深度: %.1fX\n", qc.DedupDepth))
		b.WriteString(fmt.Sprintf("- 目标区域覆盖 (30x): %.2f%%\n", qc.PctTargetBases30x*100))
		b.WriteString(fmt.Sprintf("- 重复率: %.2f%%\n", qc.DuplicateRate*100))
		b.WriteString(fmt.Sprintf("- Q30: %.2f%%\n", qc.Q30Rate*100))
		b.WriteString(fmt.Sprintf("- 预测性别: %s\n", qc.PredictedGender))
		b.WriteString(fmt.Sprintf("- 平均目标深度: %.1fX\n", qc.MeanTargetCoverage))
	}

	// Variant section
	b.WriteString("## 变异检测结果 (SNV/InDel)\n\n")
	b.WriteString(fmt.Sprintf("> 全部变异: %d | 筛选后: %d\n", totalRaw, len(snvs)))
	if len(filter.Genes) > 0 {
		b.WriteString(fmt.Sprintf("> 聚焦基因: %s\n", strings.Join(filter.Genes, ", ")))
	}
	b.WriteString(fmt.Sprintf("> 筛选规则: P/LP (gnomAD AF < %.2f%%), VUS (gnomAD AF < %.2f%%, 最多 %d 个)\n\n",
		filter.MaxPLPAF*100, filter.MaxVUSAF*100, filter.MaxVUS))

	if len(snvs) == 0 {
		b.WriteString("*无符合条件的变异*\n\n")
	} else {
		// Group by classification
		var pathogenic, likelyPath, vus []model.SNVIndel
		for _, v := range snvs {
			switch v.ACMGClassification {
			case model.ACMGPathogenic:
				pathogenic = append(pathogenic, v)
			case model.ACMGLikelyPathogenic:
				likelyPath = append(likelyPath, v)
			case model.ACMGVUS:
				vus = append(vus, v)
			}
		}

		if len(pathogenic) > 0 {
			b.WriteString(fmt.Sprintf("### 致病变异 (Pathogenic) — %d 个\n\n", len(pathogenic)))
			b.WriteString(formatSNVTable(pathogenic))
			b.WriteString("\n")
		}
		if len(likelyPath) > 0 {
			b.WriteString(fmt.Sprintf("### 可能致病变异 (Likely Pathogenic) — %d 个\n\n", len(likelyPath)))
			b.WriteString(formatSNVTable(likelyPath))
			b.WriteString("\n")
		}
		if len(vus) > 0 {
			b.WriteString(fmt.Sprintf("### 意义未明变异 (VUS) — %d 个（极罕见，LoF优先）\n\n", len(vus)))
			b.WriteString(formatSNVTable(vus))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n---\n\n请根据以上信息进行综合评估。")

	return b.String()
}

func formatClassFilter(classifications []string) string {
	if len(classifications) == 0 {
		return "致病/可能致病/VUS"
	}
	names := make([]string, len(classifications))
	for i, c := range classifications {
		switch c {
		case "Pathogenic":
			names[i] = "致病"
		case "Likely_Pathogenic":
			names[i] = "可能致病"
		case "VUS":
			names[i] = "VUS"
		case "Likely_Benign":
			names[i] = "可能良性"
		case "Benign":
			names[i] = "良性"
		default:
			names[i] = c
		}
	}
	return strings.Join(names, "+")
}

func formatSampleForPrompt(s *model.Sample) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("- 内部编号: %s\n", s.InternalID))
	b.WriteString(fmt.Sprintf("- 性别: %s\n", s.Gender))
	if s.Age != nil {
		b.WriteString(fmt.Sprintf("- 年龄: %d\n", *s.Age))
	}
	b.WriteString(fmt.Sprintf("- 样本类型: %s\n", s.SampleType))

	if s.ClinicalDiagnosis != "" {
		info := parseClinicalDiagnosisLocal(s.ClinicalDiagnosis)
		if info.MainDiagnosis != "" {
			b.WriteString(fmt.Sprintf("- 主要诊断: %s\n", info.MainDiagnosis))
		}
		if info.OnsetAge != "" {
			b.WriteString(fmt.Sprintf("- 发病年龄: %s\n", info.OnsetAge))
		}
		if info.DiseaseHistory != "" {
			b.WriteString(fmt.Sprintf("- 病史: %s\n", info.DiseaseHistory))
		}
	}

	hpo := s.GetHPOTerms()
	if len(hpo) > 0 {
		b.WriteString("- HPO 表型: ")
		terms := make([]string, len(hpo))
		for i, h := range hpo {
			if h.Name != "" {
				terms[i] = fmt.Sprintf("%s(%s)", h.Name, h.ID)
			} else {
				terms[i] = h.ID
			}
		}
		b.WriteString(strings.Join(terms, ", "))
		b.WriteString("\n")
	}

	fh := s.GetFamilyHistory()
	if fh.HasHistory {
		b.WriteString("- 家族史: 有")
		if len(fh.AffectedMembers) > 0 {
			members := make([]string, len(fh.AffectedMembers))
			for i, m := range fh.AffectedMembers {
				members[i] = fmt.Sprintf("%s(%s)", m.Relation, m.Condition)
			}
			b.WriteString(" - " + strings.Join(members, ", "))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func formatSNVTable(variants []model.SNVIndel) string {
	var b strings.Builder
	b.WriteString("| 基因 | 染色体 | 位置 | 变异 | 类型 | 杂合性 | ACMG | gnomAD AF | 深度 | VAF | HGVSc | HGVSp |\n")
	b.WriteString("|------|--------|------|------|------|--------|------|-----------|------|-----|-------|-------|\n")
	for _, v := range variants {
		af := "-"
		if v.GnomadAF != nil {
			af = fmt.Sprintf("%.6f", *v.GnomadAF)
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %d | %s>%s | %s | %s | %s | %s | %d | %.4f | %s | %s |\n",
			v.Gene, v.Chromosome, v.Position, v.Ref, v.Alt,
			v.VariantType, v.Zygosity, v.ACMGClassification,
			af, v.Depth, v.VAF, v.HGVSc, v.HGVSp))
	}
	return b.String()
}

type clinicalDiagnosisInfo struct {
	MainDiagnosis  string `json:"mainDiagnosis"`
	Symptoms       string `json:"symptoms"`
	OnsetAge       string `json:"onsetAge"`
	DiseaseHistory string `json:"diseaseHistory"`
}

func parseClinicalDiagnosisLocal(s string) clinicalDiagnosisInfo {
	if s == "" {
		return clinicalDiagnosisInfo{}
	}
	var info clinicalDiagnosisInfo
	if err := json.Unmarshal([]byte(s), &info); err == nil {
		return info
	}
	return clinicalDiagnosisInfo{MainDiagnosis: s}
}
