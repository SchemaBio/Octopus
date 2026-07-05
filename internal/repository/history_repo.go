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

func (r *HistoryRepository) scopedHistory(modelValue interface{}, table string, query *model.HistoryListQuery) *gorm.DB {
	db := r.db.Model(modelValue).Where(table+".reviewed = ?", true)
	if query != nil && !query.IncludeAll {
		db = db.Joins("JOIN tasks ON tasks.uuid = " + table + ".task_id")
		if query.ExternalOrgID != "" {
			db = db.Where("tasks.external_org_id = ?", query.ExternalOrgID)
		} else {
			db = db.Where("tasks.created_by = ?", query.CreatedBy)
		}
	}
	return db
}

func (r *HistoryRepository) scopedSNVDetectionRecords(gene, hgvsc, hgvsp string, query *model.HistoryListQuery) []model.DetectionRecord {
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

	db := r.db.Model(&model.SNVIndel{}).
		Select(`result_snv_indels.id, result_snv_indels.task_id, tasks.name as task_name,
			tasks.pipeline, tasks.pipeline_version, tasks.sample_id, tasks.internal_id,
			result_snv_indels.reviewed_at, result_snv_indels.reviewed_by`).
		Joins("JOIN tasks ON tasks.uuid = result_snv_indels.task_id").
		Where("result_snv_indels.gene = ? AND result_snv_indels.hgvsc = ? AND result_snv_indels.hgvsp = ? AND result_snv_indels.reviewed = ?",
			gene, hgvsc, hgvsp, true)
	if query != nil && !query.IncludeAll {
		if query.ExternalOrgID != "" {
			db = db.Where("tasks.external_org_id = ?", query.ExternalOrgID)
		} else {
			db = db.Where("tasks.created_by = ?", query.CreatedBy)
		}
	}

	var rows []row
	db.Scan(&rows)

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
	baseQuery := r.scopedHistory(&model.SNVIndel{}, "result_snv_indels", query).
		Select(`
			gene, hgvsc, hgvsp,
			MIN(transcript) AS transcript,
			MIN(acmg_classification) AS acmg_classification,
			MIN(consequence) AS consequence,
			MIN(rs_id) AS rs_id,
			MIN(clinvar_dn) AS clinvar_id,
			MIN(gnomad_af) AS gnomad_af,
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
	countQuery := r.scopedHistory(&model.SNVIndel{}, "result_snv_indels", query).
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
		records := r.scopedSNVDetectionRecords(row.Gene, row.HGVSc, row.HGVSp, query)
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

	base := r.scopedHistory(&model.CNVSegment{}, "result_cnv_segments", query).
		Select(`chromosome, start_position, end_position,
			GREATEST(end_position - start_position + 1, 0) AS length,
			type,
			CAST(ROUND(AVG(COALESCE(copy_ratio, CASE WHEN type = 'DUP' THEN 1.5 ELSE 0.5 END)) * 2) AS INTEGER) AS copy_number,
			MIN(COALESCE(NULLIF(dosage_genes, ''), NULLIF(gen_cc_ad_genes, ''), '[]')) AS genes,
			AVG(COALESCE(weight, 1.0)) AS confidence,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("chromosome, start_position, end_position, type")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where(
			"chromosome LIKE ? OR dosage_genes LIKE ? OR gen_cc_ad_genes LIKE ? OR iscn LIKE ? OR classification LIKE ?",
			s, s, s, s, s,
		)
	}

	var total int64
	countQuery := r.scopedHistory(&model.CNVSegment{}, "result_cnv_segments", query).
		Select("COUNT(DISTINCT chromosome || '-' || start_position || '-' || end_position || '-' || type)")
	if query.Search != "" {
		s := "%" + query.Search + "%"
		countQuery = countQuery.Where(
			"chromosome LIKE ? OR dosage_genes LIKE ? OR gen_cc_ad_genes LIKE ? OR iscn LIKE ? OR classification LIKE ?",
			s, s, s, s, s,
		)
	}
	countQuery.Count(&total)

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

	base := r.scopedHistory(&model.CNVExon{}, "result_cnv_exons", query).
		Select(`gene, transcript, CAST(exon_count AS TEXT) AS exon,
			MIN(chromosome) AS chromosome,
			MIN(start_position) AS start_position,
			MAX(end_position) AS end_position,
			type,
			CAST(ROUND(AVG(COALESCE(copy_ratio, 1.0)) * 2) AS INTEGER) AS copy_number,
			AVG(COALESCE(copy_ratio, depth_ratio, ratio2, 1.0)) AS ratio,
			AVG(COALESCE(weight, quality, 1.0)) AS confidence,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("gene, transcript, exon_count, type")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where("gene LIKE ? OR transcript LIKE ? OR CAST(exon_count AS TEXT) LIKE ?", s, s, s)
	}

	var total int64
	countQuery := r.scopedHistory(&model.CNVExon{}, "result_cnv_exons", query).
		Select("COUNT(DISTINCT gene || '-' || transcript || '-' || exon_count || '-' || type)")
	if query.Search != "" {
		s := "%" + query.Search + "%"
		countQuery = countQuery.Where("gene LIKE ? OR transcript LIKE ? OR CAST(exon_count AS TEXT) LIKE ?", s, s, s)
	}
	countQuery.Count(&total)

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

	base := r.scopedHistory(&model.STR{}, "result_strs", query).
		Select(`gene, '' AS transcript, chromosome || ':' || position AS locus, repeat_unit,
			0 AS normal_range_min, normal_range_max, status,
			MIN(ref_repeats) as min_count, MAX(ref_repeats) as max_count,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("gene, chromosome, position, repeat_unit, normal_range_max, status")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where(
			"gene LIKE ? OR chromosome LIKE ? OR repeat_unit LIKE ? OR disease LIKE ? OR inheritance LIKE ?",
			s, s, s, s, s,
		)
	}

	var total int64
	countQuery := r.scopedHistory(&model.STR{}, "result_strs", query).
		Select("COUNT(DISTINCT gene || '-' || chromosome || '-' || position)")
	if query.Search != "" {
		s := "%" + query.Search + "%"
		countQuery = countQuery.Where(
			"gene LIKE ? OR chromosome LIKE ? OR repeat_unit LIKE ? OR disease LIKE ? OR inheritance LIKE ?",
			s, s, s, s, s,
		)
	}
	countQuery.Count(&total)

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
			Status:          rw.Status,
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
		TEType     string
		Direction  string
		Length     int64
		Impact     string
		Count      int
		FirstAt    string
		LastAt     string
	}

	base := r.scopedHistory(&model.MEIVariant{}, "result_mei_variants", query).
		Select(`chromosome, position, gene, te_type,
			MIN(direction) AS direction,
			CAST(ROUND(AVG(avg_soft_clip_length)) AS BIGINT) AS length,
			MIN(COALESCE(NULLIF(impact, ''), NULLIF(location, ''), NULLIF(consequence, ''))) AS impact,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("chromosome, position, gene, te_type")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where("gene LIKE ? OR chromosome LIKE ?", s, s)
	}

	var total int64
	r.scopedHistory(&model.MEIVariant{}, "result_mei_variants", query).
		Select("COUNT(DISTINCT chromosome || '-' || position || '-' || gene || '-' || te_type)").Count(&total)

	page, pageSize := normalizePage(query)

	var rows []row
	err := base.Order("count DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	results := make([]model.GroupedMEI, len(rows))
	for i, rw := range rows {
		results[i] = model.GroupedMEI{
			GroupID:         fmt.Sprintf("%s-%d-%s-%s", rw.Chromosome, rw.Position, rw.Gene, rw.TEType),
			Chromosome:      rw.Chromosome,
			Position:        rw.Position,
			Gene:            rw.Gene,
			TEType:          rw.TEType,
			Direction:       rw.Direction,
			Length:          rw.Length,
			Impact:          rw.Impact,
			DetectionCount:  rw.Count,
			FirstDetectedAt: rw.FirstAt,
			LastDetectedAt:  rw.LastAt,
			Records:         []model.DetectionRecord{},
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

	base := r.scopedHistory(&model.MitochondrialVariant{}, "result_mt_variants", query).
		Select(`position, ref, alt,
			MIN(gene) AS gene,
			MIN(COALESCE(NULLIF(clinvar_sig, ''), 'VUS')) AS pathogenicity,
			MIN(COALESCE(NULLIF(mitophen_phenotypes, ''), NULLIF(clinvar_dn, ''), '')) AS associated_disease,
			'' AS haplogroup,
			MIN(heteroplasmy) as min_het, MAX(heteroplasmy) as max_het,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("position, ref, alt")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where(
			"gene LIKE ? OR mt_gene LIKE ? OR mitophen_phenotypes LIKE ? OR clinvar_dn LIKE ? OR ref LIKE ? OR alt LIKE ?",
			s, s, s, s, s, s,
		)
	}

	var total int64
	countQuery := r.scopedHistory(&model.MitochondrialVariant{}, "result_mt_variants", query).
		Select("COUNT(DISTINCT position || '-' || ref || '-' || alt)")
	if query.Search != "" {
		s := "%" + query.Search + "%"
		countQuery = countQuery.Where(
			"gene LIKE ? OR mt_gene LIKE ? OR mitophen_phenotypes LIKE ? OR clinvar_dn LIKE ? OR ref LIKE ? OR alt LIKE ?",
			s, s, s, s, s, s,
		)
	}
	countQuery.Count(&total)

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
			Pathogenicity:     rw.Pathogenicity,
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

	base := r.scopedHistory(&model.UPDRegion{}, "result_upd_regions", query).
		Select(`chromosome, start_position, end_position, length, type, genes, parent_of_origin,
			COUNT(*) as count, MIN(reviewed_at) as first_at, MAX(reviewed_at) as last_at`).
		Group("chromosome, start_position, end_position, type")

	if query.Search != "" {
		s := "%" + query.Search + "%"
		base = base.Where("chromosome LIKE ? OR genes LIKE ?", s, s)
	}

	var total int64
	r.scopedHistory(&model.UPDRegion{}, "result_upd_regions", query).
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
	if pageSize > 100 {
		pageSize = 100
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
