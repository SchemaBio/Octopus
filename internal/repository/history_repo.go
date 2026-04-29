package repository

import (
	"encoding/json"
	"fmt"

	"github.com/bioinfo/schema-platform/internal/database"
	"github.com/bioinfo/schema-platform/internal/model"
	"gorm.io/gorm"
)

// HistoryRepository provides history aggregation queries
type HistoryRepository struct {
	db *gorm.DB
}

// NewHistoryRepository creates a new history repository
func NewHistoryRepository() *HistoryRepository {
	return &HistoryRepository{
		db: database.GetDB(),
	}
}

// GroupedSNVRow is the raw row from GROUP BY query
type GroupedSNVRow struct {
	Gene               string
	HGVSc              string
	HGVSp              string
	Transcript         string
	ACMGClassification string
	Consequence        string
	RsID               string
	ClinvarID          string
	GnomadAF           *float64
	DetectionCount     int
	FirstDetectedAt    string
	LastDetectedAt     string
}

// GetGroupedSNVIndels returns grouped SNV/Indel history
func (r *HistoryRepository) GetGroupedSNVIndels(query *model.HistoryListQuery) ([]model.GroupedSNVIndel, int64, error) {
	// First, get grouped results using raw SQL
	baseQuery := r.db.Model(&model.SNVIndel{}).
		Where("reviewed = ?", true).
		Select(`
			gene, hgvsc, hgvsp, transcript, acmg_classification, consequence,
			rs_id, clinvar_id, gnomad_af,
			COUNT(*) as detection_count,
			MIN(reviewed_at) as first_detected_at,
			MAX(reviewed_at) as last_detected_at
		`).
		Group("gene, hgvsc, hgvsp")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		baseQuery = baseQuery.Where("gene LIKE ? OR hgvsc LIKE ? OR hgvsp LIKE ?", s, s, s)
	}

	// Count distinct groups
	var total int64
	countQuery := r.db.Model(&model.SNVIndel{}).
		Where("reviewed = ?", true).
		Select("COUNT(DISTINCT gene || '-' || hgvsc || '-' || hgvsp)")
	if query.Search != "" {
		s := "%" + query.Search + "%"
		countQuery = countQuery.Where("gene LIKE ? OR hgvsc LIKE ? OR hgvsp LIKE ?", s, s, s)
	}
	countQuery.Count(&total)

	// Apply pagination and sorting
	orderClause := "detection_count DESC"
	if query.SortColumn != "" {
		dir := "ASC"
		if query.SortDir == "desc" {
			dir = "DESC"
		}
		switch query.SortColumn {
		case "gene":
			orderClause = fmt.Sprintf("gene %s", dir)
		case "hgvsc":
			orderClause = fmt.Sprintf("hgvsc %s", dir)
		case "detectionCount":
			orderClause = fmt.Sprintf("detection_count %s", dir)
		case "firstDetectedAt":
			orderClause = fmt.Sprintf("first_detected_at %s", dir)
		case "lastDetectedAt":
			orderClause = fmt.Sprintf("last_detected_at %s", dir)
		}
	}

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 20
	}

	var rows []GroupedSNVRow
	err := baseQuery.Order(orderClause).Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	// For each group, get the detection records
	results := make([]model.GroupedSNVIndel, len(rows))
	for i, row := range rows {
		groupID := row.Gene + "-" + row.HGVSc + "-" + row.HGVSp
		records := r.getSNVDetectionRecords(row.Gene, row.HGVSc, row.HGVSp)
		results[i] = model.GroupedSNVIndel{
			GroupID:            groupID,
			Gene:               row.Gene,
			HGVSc:              row.HGVSc,
			HGVSp:              row.HGVSp,
			Transcript:         row.Transcript,
			ACMGClassification: model.ACMGClassification(row.ACMGClassification),
			Consequence:        row.Consequence,
			RsID:               row.RsID,
			ClinvarID:          row.ClinvarID,
			GnomadAF:           row.GnomadAF,
			DetectionCount:     row.DetectionCount,
			FirstDetectedAt:    row.FirstDetectedAt,
			LastDetectedAt:     row.LastDetectedAt,
			Records:            records,
		}
	}

	return results, total, nil
}

func (r *HistoryRepository) getSNVDetectionRecords(gene, hgvsc, hgvsp string) []model.DetectionRecord {
	type row struct {
		ID              string
		TaskID          string
		TaskName        string
		Pipeline        string
		PipelineVersion string
		SampleID        string
		InternalID      string
		ReviewedAt      string
		ReviewedBy      string
	}

	var rows []row
	r.db.Model(&model.SNVIndel{}).
		Select(`result_snv_indels.id, result_snv_indels.task_id, tasks.name as task_name,
			tasks.pipeline, tasks.pipeline_version, tasks.sample_id, tasks.internal_id,
			result_snv_indels.reviewed_at, result_snv_indels.reviewed_by`).
		Joins("LEFT JOIN tasks ON tasks.uuid = result_snv_indels.task_id").
		Where("result_snv_indels.gene = ? AND result_snv_indels.hgvsc = ? AND result_snv_indels.hgvsp = ? AND result_snv_indels.reviewed = ?",
			gene, hgvsc, hgvsp, true).
		Scan(&rows)

	records := make([]model.DetectionRecord, len(rows))
	for i, row := range rows {
		records[i] = model.DetectionRecord{
			RecordID:        row.ID,
			TaskID:          row.TaskID,
			TaskName:        row.TaskName,
			Pipeline:        row.Pipeline,
			PipelineVersion: row.PipelineVersion,
			SampleID:        row.SampleID,
			InternalID:      row.InternalID,
			ReviewedAt:      row.ReviewedAt,
			ReviewedBy:      row.ReviewedBy,
		}
	}
	return records
}

// GetGroupedCNVSegments returns grouped CNV segment history
func (r *HistoryRepository) GetGroupedCNVSegments(query *model.HistoryListQuery) ([]model.GroupedCNVSegment, int64, error) {
	type row struct {
		Chromosome    string
		StartPosition int64
		EndPosition   int64
		Length        int64
		Type          string
		CopyNumber    int
		Genes         string
		Confidence    float64
		Count         int
		FirstAt       string
		LastAt        string
	}

	base := r.db.Model(&model.CNVSegment{}).Where("reviewed = ?", true).
		Select(`chromosome, start_position, end_position, length, type, copy_number, genes, confidence,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("chromosome, start_position, end_position, type")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where("chromosome LIKE ? OR genes LIKE ?", s, s)
	}

	var total int64
	r.db.Model(&model.CNVSegment{}).Where("reviewed = ?", true).
		Select("COUNT(DISTINCT chromosome || '-' || start_position || '-' || end_position || '-' || type)").Count(&total)

	page, pageSize := normalizePage(query)

	var rows []row
	err := base.Order("count DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	results := make([]model.GroupedCNVSegment, len(rows))
	for i, rw := range rows {
		genes := parseJSONStringArray(rw.Genes)
		results[i] = model.GroupedCNVSegment{
			GroupID:         fmt.Sprintf("%s-%d-%d-%s", rw.Chromosome, rw.StartPosition, rw.EndPosition, rw.Type),
			Chromosome:      rw.Chromosome,
			StartPosition:   rw.StartPosition,
			EndPosition:     rw.EndPosition,
			Length:          rw.Length,
			Type:            rw.Type,
			CopyNumber:      rw.CopyNumber,
			Genes:           genes,
			Confidence:      rw.Confidence,
			DetectionCount:  rw.Count,
			FirstDetectedAt: rw.FirstAt,
			LastDetectedAt:  rw.LastAt,
			Records:         []model.DetectionRecord{},
		}
	}

	return results, total, nil
}

// GetGroupedCNVExons returns grouped CNV exon history
func (r *HistoryRepository) GetGroupedCNVExons(query *model.HistoryListQuery) ([]model.GroupedCNVExon, int64, error) {
	type row struct {
		Gene          string
		Transcript    string
		Exon          string
		Chromosome    string
		StartPosition int64
		EndPosition   int64
		Type          string
		CopyNumber    int
		Ratio         float64
		Confidence    float64
		Count         int
		FirstAt       string
		LastAt        string
	}

	base := r.db.Model(&model.CNVExon{}).Where("reviewed = ?", true).
		Select(`gene, transcript, exon, chromosome, start_position, end_position, type, copy_number, ratio, confidence,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("gene, transcript, exon, type")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where("gene LIKE ? OR exon LIKE ?", s, s)
	}

	var total int64
	r.db.Model(&model.CNVExon{}).Where("reviewed = ?", true).
		Select("COUNT(DISTINCT gene || '-' || transcript || '-' || exon || '-' || type)").Count(&total)

	page, pageSize := normalizePage(query)

	var rows []row
	err := base.Order("count DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	results := make([]model.GroupedCNVExon, len(rows))
	for i, rw := range rows {
		results[i] = model.GroupedCNVExon{
			GroupID:         fmt.Sprintf("%s-%s-%s-%s", rw.Gene, rw.Transcript, rw.Exon, rw.Type),
			Gene:            rw.Gene,
			Transcript:      rw.Transcript,
			Exon:            rw.Exon,
			Chromosome:      rw.Chromosome,
			StartPosition:   rw.StartPosition,
			EndPosition:     rw.EndPosition,
			Type:            rw.Type,
			CopyNumber:      rw.CopyNumber,
			Ratio:           rw.Ratio,
			Confidence:      rw.Confidence,
			DetectionCount:  rw.Count,
			FirstDetectedAt: rw.FirstAt,
			LastDetectedAt:  rw.LastAt,
			Records:         []model.DetectionRecord{},
		}
	}

	return results, total, nil
}

// GetGroupedSTRs returns grouped STR history
func (r *HistoryRepository) GetGroupedSTRs(query *model.HistoryListQuery) ([]model.GroupedSTR, int64, error) {
	type row struct {
		Gene           string
		Transcript     string
		Locus          string
		RepeatUnit     string
		NormalRangeMin int
		NormalRangeMax int
		Status         string
		MinCount       int
		MaxCount       int
		Count          int
		FirstAt        string
		LastAt         string
	}

	base := r.db.Model(&model.STR{}).Where("reviewed = ?", true).
		Select(`gene, transcript, locus, repeat_unit, normal_range_min, normal_range_max, status,
			MIN(repeat_count) as min_count, MAX(repeat_count) as max_count,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("gene, locus")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where("gene LIKE ? OR locus LIKE ?", s, s)
	}

	var total int64
	r.db.Model(&model.STR{}).Where("reviewed = ?", true).
		Select("COUNT(DISTINCT gene || '-' || locus)").Count(&total)

	page, pageSize := normalizePage(query)

	var rows []row
	err := base.Order("count DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	results := make([]model.GroupedSTR, len(rows))
	for i, rw := range rows {
		results[i] = model.GroupedSTR{
			GroupID:         fmt.Sprintf("%s-%s", rw.Gene, rw.Locus),
			Gene:            rw.Gene,
			Transcript:      rw.Transcript,
			Locus:           rw.Locus,
			RepeatUnit:      rw.RepeatUnit,
			NormalRangeMin:  rw.NormalRangeMin,
			NormalRangeMax:  rw.NormalRangeMax,
			Status:          model.STRStatus(rw.Status),
			MinRepeatCount:  rw.MinCount,
			MaxRepeatCount:  rw.MaxCount,
			DetectionCount:  rw.Count,
			FirstDetectedAt: rw.FirstAt,
			LastDetectedAt:  rw.LastAt,
			Records:         []model.DetectionRecord{},
		}
	}

	return results, total, nil
}

// GetGroupedMEIs returns grouped MEI history
func (r *HistoryRepository) GetGroupedMEIs(query *model.HistoryListQuery) ([]model.GroupedMEI, int64, error) {
	type row struct {
		Chromosome string
		Position   int64
		Gene       string
		MEIType    string
		Strand     string
		Length     int64
		Impact     string
		ACMGClass  string
		Count      int
		FirstAt    string
		LastAt     string
	}

	base := r.db.Model(&model.MEIVariant{}).Where("reviewed = ?", true).
		Select(`chromosome, position, gene, mei_type, strand, length, impact, acmg_classification,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("chromosome, position, gene, mei_type")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where("gene LIKE ? OR chromosome LIKE ?", s, s)
	}

	var total int64
	r.db.Model(&model.MEIVariant{}).Where("reviewed = ?", true).
		Select("COUNT(DISTINCT chromosome || '-' || position || '-' || gene || '-' || mei_type)").Count(&total)

	page, pageSize := normalizePage(query)

	var rows []row
	err := base.Order("count DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	results := make([]model.GroupedMEI, len(rows))
	for i, rw := range rows {
		var acmg model.ACMGClassification
		if rw.ACMGClass != "" {
			acmg = model.ACMGClassification(rw.ACMGClass)
		}
		results[i] = model.GroupedMEI{
			GroupID:            fmt.Sprintf("%s-%d-%s-%s", rw.Chromosome, rw.Position, rw.Gene, rw.MEIType),
			Chromosome:         rw.Chromosome,
			Position:           rw.Position,
			Gene:               rw.Gene,
			MEIType:            model.MEIType(rw.MEIType),
			Strand:             rw.Strand,
			Length:             rw.Length,
			Impact:             rw.Impact,
			ACMGClassification: acmg,
			DetectionCount:     rw.Count,
			FirstDetectedAt:    rw.FirstAt,
			LastDetectedAt:     rw.LastAt,
			Records:            []model.DetectionRecord{},
		}
	}

	return results, total, nil
}

// GetGroupedMTVariants returns grouped MT variant history
func (r *HistoryRepository) GetGroupedMTVariants(query *model.HistoryListQuery) ([]model.GroupedMTVariant, int64, error) {
	type row struct {
		Position          int64
		Ref               string
		Alt               string
		Gene              string
		Pathogenicity     string
		AssociatedDisease string
		Haplogroup        string
		MinHet            float64
		MaxHet            float64
		Count             int
		FirstAt           string
		LastAt            string
	}

	base := r.db.Model(&model.MitochondrialVariant{}).Where("reviewed = ?", true).
		Select(`position, ref, alt, gene, pathogenicity, associated_disease, haplogroup,
			MIN(heteroplasmy) as min_het, MAX(heteroplasmy) as max_het,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("position, ref, alt")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where("gene LIKE ? OR associated_disease LIKE ?", s, s)
	}

	var total int64
	r.db.Model(&model.MitochondrialVariant{}).Where("reviewed = ?", true).
		Select("COUNT(DISTINCT position || '-' || ref || '-' || alt)").Count(&total)

	page, pageSize := normalizePage(query)

	var rows []row
	err := base.Order("count DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	results := make([]model.GroupedMTVariant, len(rows))
	for i, rw := range rows {
		results[i] = model.GroupedMTVariant{
			GroupID:           fmt.Sprintf("%d-%s-%s", rw.Position, rw.Ref, rw.Alt),
			Position:          rw.Position,
			Ref:               rw.Ref,
			Alt:               rw.Alt,
			Gene:              rw.Gene,
			Pathogenicity:     model.MitochondrialPathogenicity(rw.Pathogenicity),
			AssociatedDisease: rw.AssociatedDisease,
			Haplogroup:        rw.Haplogroup,
			MinHeteroplasmy:   rw.MinHet,
			MaxHeteroplasmy:   rw.MaxHet,
			DetectionCount:    rw.Count,
			FirstDetectedAt:   rw.FirstAt,
			LastDetectedAt:    rw.LastAt,
			Records:           []model.DetectionRecord{},
		}
	}

	return results, total, nil
}

// GetGroupedUPDRegions returns grouped UPD region history
func (r *HistoryRepository) GetGroupedUPDRegions(query *model.HistoryListQuery) ([]model.GroupedUPDRegion, int64, error) {
	type row struct {
		Chromosome    string
		StartPosition int64
		EndPosition   int64
		Length        int64
		Type          string
		Genes         string
		ParentOrigin  string
		Count         int
		FirstAt       string
		LastAt        string
	}

	base := r.db.Model(&model.UPDRegion{}).Where("reviewed = ?", true).
		Select(`chromosome, start_position, end_position, length, type, genes, parent_of_origin,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("chromosome, start_position, end_position, type")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where("chromosome LIKE ? OR genes LIKE ?", s, s)
	}

	var total int64
	r.db.Model(&model.UPDRegion{}).Where("reviewed = ?", true).
		Select("COUNT(DISTINCT chromosome || '-' || start_position || '-' || end_position || '-' || type)").Count(&total)

	page, pageSize := normalizePage(query)

	var rows []row
	err := base.Order("count DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	results := make([]model.GroupedUPDRegion, len(rows))
	for i, rw := range rows {
		genes := parseJSONStringArray(rw.Genes)
		var parent model.ParentOfOrigin
		if rw.ParentOrigin != "" {
			parent = model.ParentOfOrigin(rw.ParentOrigin)
		}
		results[i] = model.GroupedUPDRegion{
			GroupID:         fmt.Sprintf("%s-%s-%d-%d", rw.Chromosome, rw.Type, rw.StartPosition, rw.EndPosition),
			Chromosome:      rw.Chromosome,
			StartPosition:   rw.StartPosition,
			EndPosition:     rw.EndPosition,
			Length:          rw.Length,
			Type:            model.UPDType(rw.Type),
			Genes:           genes,
			ParentOfOrigin:  parent,
			DetectionCount:  rw.Count,
			FirstDetectedAt: rw.FirstAt,
			LastDetectedAt:  rw.LastAt,
			Records:         []model.DetectionRecord{},
		}
	}

	return results, total, nil
}

// Helper functions

func normalizePage(query *model.HistoryListQuery) (int, int) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	return page, pageSize
}

func parseJSONStringArray(s string) []string {
	if s == "" {
		return []string{}
	}
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return []string{s}
	}
	return arr
}
