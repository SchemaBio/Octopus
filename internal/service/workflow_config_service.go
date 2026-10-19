package service

import (
	"fmt"
	"math"
	"strings"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/workflow"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WorkflowConfigService struct{}

func NewWorkflowConfigService() *WorkflowConfigService { return &WorkflowConfigService{} }

func normalizeWorkflowScope(template, genome string) (string, string, error) {
	template = strings.ToLower(strings.TrimSpace(template))
	if template != "single" && template != "trio" && template != "vcf2anno" {
		return "", "", fmt.Errorf("template must be single, trio or vcf2anno")
	}
	switch strings.ToLower(strings.TrimSpace(genome)) {
	case "hg19", "grch37":
		genome = "hg19"
	case "hg38", "grch38":
		genome = "hg38"
	default:
		return "", "", fmt.Errorf("reference_genome must be hg19/GRCh37 or hg38/GRCh38")
	}
	return template, genome, nil
}

func defaultThresholdConfig(template, genome string) (model.WorkflowThresholdConfig, error) {
	inputs, err := workflow.InputsForGenome(template, genome)
	if err != nil {
		return model.WorkflowThresholdConfig{}, err
	}
	prefix := "SingleWES"
	if template == "vcf2anno" {
		prefix = "VCF2Anno"
	}
	if template == "trio" {
		prefix = "TrioWES"
	}
	return model.WorkflowThresholdConfig{
		Template: template, ReferenceGenome: genome,
		SexMinSites: int(numberAsFloat(inputs[prefix+".sex_min_sites"])), FemaleMaxRatio: numberAsFloat(inputs[prefix+".female_max_ratio"]), MaleMinRatio: numberAsFloat(inputs[prefix+".male_min_ratio"]),
		SRYSexCutoff:    numberAsFloat(inputs[prefix+".sry_sex_cutoff"]),
		CNVBinSize:      int(numberAsFloat(inputs[prefix+".cnv_bin_size"])),
		CNVDupThreshold: numberAsFloat(inputs[prefix+".cnv_dup_threshold"]),
		CNVDelThreshold: numberAsFloat(inputs[prefix+".cnv_del_threshold"]),
	}, nil
}

func numberAsFloat(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

func (s *WorkflowConfigService) List() ([]model.WorkflowThresholdConfig, error) {
	result := make([]model.WorkflowThresholdConfig, 0, 4)
	for _, template := range []string{"single", "trio", "vcf2anno"} {
		for _, genome := range []string{"hg19", "hg38"} {
			item, err := s.Get(template, genome)
			if err != nil {
				return nil, err
			}
			result = append(result, item)
		}
	}
	return result, nil
}

func (s *WorkflowConfigService) Get(template, genome string) (model.WorkflowThresholdConfig, error) {
	template, genome, err := normalizeWorkflowScope(template, genome)
	if err != nil {
		return model.WorkflowThresholdConfig{}, err
	}
	var item model.WorkflowThresholdConfig
	err = database.GetDB().Where("template = ? AND reference_genome = ?", template, genome).First(&item).Error
	if err == nil {
		return item, nil
	}
	if err != gorm.ErrRecordNotFound {
		return item, err
	}
	return defaultThresholdConfig(template, genome)
}

func (s *WorkflowConfigService) Update(template, genome string, req model.WorkflowThresholdUpdateRequest, userID uint) (model.WorkflowThresholdConfig, error) {
	template, genome, err := normalizeWorkflowScope(template, genome)
	if err != nil {
		return model.WorkflowThresholdConfig{}, err
	}
	if template == "vcf2anno" {
		if req.SexMinSites < 1 || math.IsNaN(req.FemaleMaxRatio) || math.IsNaN(req.MaleMinRatio) || !(0 <= req.FemaleMaxRatio && req.FemaleMaxRatio < req.MaleMinRatio && req.MaleMinRatio <= 1) {
			return model.WorkflowThresholdConfig{}, fmt.Errorf("require sex_min_sites >= 1 and 0 <= female_max_ratio < male_min_ratio <= 1")
		}
	} else if req.CNVBinSize <= 0 || req.CNVDupThreshold <= 0 || req.CNVDelThreshold <= 0 || req.SRYSexCutoff < 0 {
		return model.WorkflowThresholdConfig{}, fmt.Errorf("workflow thresholds must be non-negative and bin size must be positive")
	}
	item := model.WorkflowThresholdConfig{Template: template, ReferenceGenome: genome, SRYSexCutoff: req.SRYSexCutoff, CNVBinSize: req.CNVBinSize, CNVDupThreshold: req.CNVDupThreshold, CNVDelThreshold: req.CNVDelThreshold, UpdatedBy: userID, SexMinSites: req.SexMinSites, FemaleMaxRatio: req.FemaleMaxRatio, MaleMinRatio: req.MaleMinRatio}
	err = database.GetDB().Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "template"}, {Name: "reference_genome"}}, DoUpdates: clause.AssignmentColumns([]string{"sex_min_sites", "female_max_ratio", "male_min_ratio", "sry_sex_cutoff", "cnv_bin_size", "cnv_dup_threshold", "cnv_del_threshold", "updated_by", "updated_at"})}).Create(&item).Error
	if err != nil {
		return item, err
	}
	return s.Get(template, genome)
}

func applyWorkflowThresholds(inputs map[string]interface{}, config model.WorkflowThresholdConfig) {
	if config.Template == "vcf2anno" {
		inputs["VCF2Anno.sex_min_sites"] = config.SexMinSites
		inputs["VCF2Anno.female_max_ratio"] = config.FemaleMaxRatio
		inputs["VCF2Anno.male_min_ratio"] = config.MaleMinRatio
		return
	}
	prefix := "SingleWES"
	if config.Template == "trio" {
		prefix = "TrioWES"
	}
	inputs[prefix+".sry_sex_cutoff"] = config.SRYSexCutoff
	inputs[prefix+".cnv_bin_size"] = config.CNVBinSize
	inputs[prefix+".cnv_dup_threshold"] = config.CNVDupThreshold
	inputs[prefix+".cnv_del_threshold"] = config.CNVDelThreshold
}
