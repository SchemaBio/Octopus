package service

import (
	"fmt"
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
	if template != "single" && template != "trio" {
		return "", "", fmt.Errorf("template must be single or trio")
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
	if template == "trio" {
		prefix = "TrioWES"
	}
	return model.WorkflowThresholdConfig{
		Template: template, ReferenceGenome: genome,
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
	for _, template := range []string{"single", "trio"} {
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
	if req.CNVBinSize <= 0 || req.CNVDupThreshold <= 0 || req.CNVDelThreshold <= 0 || req.SRYSexCutoff < 0 {
		return model.WorkflowThresholdConfig{}, fmt.Errorf("workflow thresholds must be non-negative and bin size must be positive")
	}
	item := model.WorkflowThresholdConfig{Template: template, ReferenceGenome: genome, SRYSexCutoff: req.SRYSexCutoff, CNVBinSize: req.CNVBinSize, CNVDupThreshold: req.CNVDupThreshold, CNVDelThreshold: req.CNVDelThreshold, UpdatedBy: userID}
	err = database.GetDB().Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "template"}, {Name: "reference_genome"}}, DoUpdates: clause.AssignmentColumns([]string{"sry_sex_cutoff", "cnv_bin_size", "cnv_dup_threshold", "cnv_del_threshold", "updated_by", "updated_at"})}).Create(&item).Error
	if err != nil {
		return item, err
	}
	return s.Get(template, genome)
}

func applyWorkflowThresholds(inputs map[string]interface{}, config model.WorkflowThresholdConfig) {
	prefix := "SingleWES"
	if config.Template == "trio" {
		prefix = "TrioWES"
	}
	inputs[prefix+".sry_sex_cutoff"] = config.SRYSexCutoff
	inputs[prefix+".cnv_bin_size"] = config.CNVBinSize
	inputs[prefix+".cnv_dup_threshold"] = config.CNVDupThreshold
	inputs[prefix+".cnv_del_threshold"] = config.CNVDelThreshold
}
