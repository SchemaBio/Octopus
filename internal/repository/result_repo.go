package repository

import (
	"github.com/bioinfo/schema-platform/internal/database"
	"github.com/bioinfo/schema-platform/internal/model"
	"gorm.io/gorm"
)

// ResultRepository provides generic result operations
type ResultRepository struct {
	db *gorm.DB
}

// NewResultRepository creates a new result repository
func NewResultRepository() *ResultRepository {
	return &ResultRepository{
		db: database.GetDB(),
	}
}

// ========== SNV/Indel ==========

func (r *ResultRepository) FindSNVIndelsByTaskID(taskID string) ([]model.SNVIndel, error) {
	var results []model.SNVIndel
	err := r.db.Where("task_id = ?", taskID).Find(&results).Error
	return results, err
}

func (r *ResultRepository) PaginateSNVIndels(query *model.SNVIndelListQuery) ([]model.SNVIndel, int64, error) {
	db := r.db.Model(&model.SNVIndel{}).Where("task_id = ?", query.TaskID)
	if query.Search != "" {
		s := "%" + query.Search + "%"
		db = db.Where("gene LIKE ? OR hgvsc LIKE ? OR hgvsp LIKE ?", s, s, s)
	}
	if query.Gene != "" {
		db = db.Where("gene = ?", query.Gene)
	}
	if query.Classification != "" {
		db = db.Where("acmg_classification = ?", query.Classification)
	}
	if query.GeneListID != "" {
		// Filter by gene list - get genes from gene_list_genes table
		db = db.Where("gene IN (SELECT gene FROM gene_list_genes WHERE gene_list_id = ?)", query.GeneListID)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 { page = 1 }
	pageSize := query.PageSize
	if pageSize < 1 { pageSize = 10 }

	var results []model.SNVIndel
	err = db.Order("acmg_classification ASC, gene ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *ResultRepository) FindSNVIndelByID(id string) (*model.SNVIndel, error) {
	var result model.SNVIndel
	err := r.db.Where("id = ?", id).First(&result).Error
	return &result, err
}

func (r *ResultRepository) UpdateSNVIndelReview(id string, reviewed bool, reviewer string) error {
	return r.db.Model(&model.SNVIndel{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reviewed":    reviewed,
		"reviewed_by": reviewer,
	}).Error
}

func (r *ResultRepository) UpdateSNVIndelReport(id string, reported bool, reporter string) error {
	return r.db.Model(&model.SNVIndel{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reported":    reported,
		"reported_by": reporter,
	}).Error
}

// ========== CNV Segment ==========

func (r *ResultRepository) PaginateCNVSegments(query *model.CNVSegmentListQuery) ([]model.CNVSegment, int64, error) {
	db := r.db.Model(&model.CNVSegment{}).Where("task_id = ?", query.TaskID)
	if query.Search != "" {
		s := "%" + query.Search + "%"
		db = db.Where("chromosome LIKE ? OR genes LIKE ?", s, s)
	}
	if query.Type != "" {
		db = db.Where("type = ?", query.Type)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 { page = 1 }
	pageSize := query.PageSize
	if pageSize < 1 { pageSize = 10 }

	var results []model.CNVSegment
	err := db.Order("chromosome ASC, start_position ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *ResultRepository) FindCNVSegmentByID(id string) (*model.CNVSegment, error) {
	var result model.CNVSegment
	err := r.db.Where("id = ?", id).First(&result).Error
	return &result, err
}

func (r *ResultRepository) UpdateCNVSegmentReview(id string, reviewed bool, reviewer string) error {
	return r.db.Model(&model.CNVSegment{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reviewed": reviewed, "reviewed_by": reviewer,
	}).Error
}

func (r *ResultRepository) UpdateCNVSegmentReport(id string, reported bool, reporter string) error {
	return r.db.Model(&model.CNVSegment{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reported": reported, "reported_by": reporter,
	}).Error
}

// ========== CNV Exon ==========

func (r *ResultRepository) PaginateCNVExons(query *model.CNVExonListQuery) ([]model.CNVExon, int64, error) {
	db := r.db.Model(&model.CNVExon{}).Where("task_id = ?", query.TaskID)
	if query.Search != "" {
		s := "%" + query.Search + "%"
		db = db.Where("gene LIKE ? OR transcript LIKE ?", s, s)
	}
	if query.Gene != "" {
		db = db.Where("gene = ?", query.Gene)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 { page = 1 }
	pageSize := query.PageSize
	if pageSize < 1 { pageSize = 10 }

	var results []model.CNVExon
	err := db.Order("gene ASC, exon ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *ResultRepository) FindCNVExonByID(id string) (*model.CNVExon, error) {
	var result model.CNVExon
	err := r.db.Where("id = ?", id).First(&result).Error
	return &result, err
}

func (r *ResultRepository) UpdateCNVExonReview(id string, reviewed bool, reviewer string) error {
	return r.db.Model(&model.CNVExon{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reviewed": reviewed, "reviewed_by": reviewer,
	}).Error
}

func (r *ResultRepository) UpdateCNVExonReport(id string, reported bool, reporter string) error {
	return r.db.Model(&model.CNVExon{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reported": reported, "reported_by": reporter,
	}).Error
}

// ========== STR ==========

func (r *ResultRepository) PaginateSTRs(query *model.STRListQuery) ([]model.STR, int64, error) {
	db := r.db.Model(&model.STR{}).Where("task_id = ?", query.TaskID)
	if query.Search != "" {
		s := "%" + query.Search + "%"
		db = db.Where("gene LIKE ? OR locus LIKE ?", s, s)
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 { page = 1 }
	pageSize := query.PageSize
	if pageSize < 1 { pageSize = 10 }

	var results []model.STR
	err := db.Order("gene ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *ResultRepository) FindSTRByID(id string) (*model.STR, error) {
	var result model.STR
	err := r.db.Where("id = ?", id).First(&result).Error
	return &result, err
}

func (r *ResultRepository) UpdateSTRReview(id string, reviewed bool, reviewer string) error {
	return r.db.Model(&model.STR{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reviewed": reviewed, "reviewed_by": reviewer,
	}).Error
}

func (r *ResultRepository) UpdateSTRReport(id string, reported bool, reporter string) error {
	return r.db.Model(&model.STR{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reported": reported, "reported_by": reporter,
	}).Error
}

// ========== MEI ==========

func (r *ResultRepository) PaginateMEIs(query *model.MEIListQuery) ([]model.MEIVariant, int64, error) {
	db := r.db.Model(&model.MEIVariant{}).Where("task_id = ?", query.TaskID)
	if query.Search != "" {
		s := "%" + query.Search + "%"
		db = db.Where("gene LIKE ? OR chromosome LIKE ?", s, s)
	}
	if query.MEIType != "" {
		db = db.Where("mei_type = ?", query.MEIType)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 { page = 1 }
	pageSize := query.PageSize
	if pageSize < 1 { pageSize = 10 }

	var results []model.MEIVariant
	err := db.Order("chromosome ASC, position ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *ResultRepository) FindMEIByID(id string) (*model.MEIVariant, error) {
	var result model.MEIVariant
	err := r.db.Where("id = ?", id).First(&result).Error
	return &result, err
}

func (r *ResultRepository) UpdateMEIReview(id string, reviewed bool, reviewer string) error {
	return r.db.Model(&model.MEIVariant{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reviewed": reviewed, "reviewed_by": reviewer,
	}).Error
}

func (r *ResultRepository) UpdateMEIReport(id string, reported bool, reporter string) error {
	return r.db.Model(&model.MEIVariant{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reported": reported, "reported_by": reporter,
	}).Error
}

// ========== Mitochondrial ==========

func (r *ResultRepository) PaginateMTVariants(query *model.MTListQuery) ([]model.MitochondrialVariant, int64, error) {
	db := r.db.Model(&model.MitochondrialVariant{}).Where("task_id = ?", query.TaskID)
	if query.Search != "" {
		s := "%" + query.Search + "%"
		db = db.Where("gene LIKE ? OR associated_disease LIKE ?", s, s)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 { page = 1 }
	pageSize := query.PageSize
	if pageSize < 1 { pageSize = 10 }

	var results []model.MitochondrialVariant
	err := db.Order("position ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *ResultRepository) FindMTVariantByID(id string) (*model.MitochondrialVariant, error) {
	var result model.MitochondrialVariant
	err := r.db.Where("id = ?", id).First(&result).Error
	return &result, err
}

func (r *ResultRepository) UpdateMTVariantReview(id string, reviewed bool, reviewer string) error {
	return r.db.Model(&model.MitochondrialVariant{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reviewed": reviewed, "reviewed_by": reviewer,
	}).Error
}

func (r *ResultRepository) UpdateMTVariantReport(id string, reported bool, reporter string) error {
	return r.db.Model(&model.MitochondrialVariant{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reported": reported, "reported_by": reporter,
	}).Error
}

// ========== UPD ==========

func (r *ResultRepository) PaginateUPDRegions(query *model.UPDListQuery) ([]model.UPDRegion, int64, error) {
	db := r.db.Model(&model.UPDRegion{}).Where("task_id = ?", query.TaskID)
	if query.Search != "" {
		s := "%" + query.Search + "%"
		db = db.Where("chromosome LIKE ? OR genes LIKE ?", s, s)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 { page = 1 }
	pageSize := query.PageSize
	if pageSize < 1 { pageSize = 10 }

	var results []model.UPDRegion
	err := db.Order("chromosome ASC, start_position ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *ResultRepository) FindUPDRegionByID(id string) (*model.UPDRegion, error) {
	var result model.UPDRegion
	err := r.db.Where("id = ?", id).First(&result).Error
	return &result, err
}

func (r *ResultRepository) UpdateUPDRegionReview(id string, reviewed bool, reviewer string) error {
	return r.db.Model(&model.UPDRegion{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reviewed": reviewed, "reviewed_by": reviewer,
	}).Error
}

func (r *ResultRepository) UpdateUPDRegionReport(id string, reported bool, reporter string) error {
	return r.db.Model(&model.UPDRegion{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reported": reported, "reported_by": reporter,
	}).Error
}

// ========== QC ==========

func (r *ResultRepository) FindQCByTaskID(taskID string) (*model.QCResult, error) {
	var result model.QCResult
	err := r.db.Where("task_id = ?", taskID).First(&result).Error
	return &result, err
}

func (r *ResultRepository) CreateQC(result *model.QCResult) error {
	return r.db.Create(result).Error
}

// ========== Generic create for import ==========

func (r *ResultRepository) CreateSNVIndels(results []model.SNVIndel) error {
	if len(results) == 0 {
		return nil
	}
	return r.db.CreateInBatches(results, 100).Error
}

func (r *ResultRepository) CreateCNVSegments(results []model.CNVSegment) error {
	if len(results) == 0 {
		return nil
	}
	return r.db.CreateInBatches(results, 100).Error
}

func (r *ResultRepository) CreateCNVExons(results []model.CNVExon) error {
	if len(results) == 0 {
		return nil
	}
	return r.db.CreateInBatches(results, 100).Error
}

func (r *ResultRepository) CreateSTRs(results []model.STR) error {
	if len(results) == 0 {
		return nil
	}
	return r.db.CreateInBatches(results, 100).Error
}

func (r *ResultRepository) CreateMEIs(results []model.MEIVariant) error {
	if len(results) == 0 {
		return nil
	}
	return r.db.CreateInBatches(results, 100).Error
}

func (r *ResultRepository) CreateMTVariants(results []model.MitochondrialVariant) error {
	if len(results) == 0 {
		return nil
	}
	return r.db.CreateInBatches(results, 100).Error
}

func (r *ResultRepository) CreateUPDRegions(results []model.UPDRegion) error {
	if len(results) == 0 {
		return nil
	}
	return r.db.CreateInBatches(results, 100).Error
}
