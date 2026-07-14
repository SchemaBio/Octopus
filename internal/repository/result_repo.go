package repository

import (
	"fmt"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
		db = db.Where("gene LIKE ? OR hgv_sc LIKE ? OR hgv_sp LIKE ?", s, s, s)
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
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

	var results []model.SNVIndel
	err := db.Order("acmg_classification ASC, gene ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
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
		db = db.Where(
			"chromosome LIKE ? OR dosage_genes LIKE ? OR gen_ccad_genes LIKE ? OR iscn LIKE ? OR classification LIKE ?",
			s, s, s, s, s,
		)
	}
	if query.Type != "" {
		db = db.Where("type = ?", query.Type)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

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
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

	var results []model.CNVExon
	err := db.Order("gene ASC, start_position ASC, end_position ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
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
		db = db.Where(
			"gene LIKE ? OR chromosome LIKE ? OR repeat_unit LIKE ? OR disease LIKE ? OR inheritance LIKE ?",
			s, s, s, s, s,
		)
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

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
	if query.TEType != "" {
		db = db.Where("te_type = ?", query.TEType)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

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
		db = db.Where(
			"gene LIKE ? OR mt_gene LIKE ? OR mitophen_phenotypes LIKE ? OR clinvar_dn LIKE ? OR ref LIKE ? OR alt LIKE ?",
			s, s, s, s, s, s,
		)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

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
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

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

// ========== ROH ==========

func (r *ResultRepository) FindROHRegionsByTaskID(taskID string) ([]model.ROHRegion, error) {
	var results []model.ROHRegion
	err := r.db.Where("task_id = ?", taskID).Find(&results).Error
	return results, err
}

func (r *ResultRepository) PaginateROHRegions(query *model.ROHListQuery) ([]model.ROHRegion, int64, error) {
	db := r.db.Model(&model.ROHRegion{}).Where("task_id = ?", query.TaskID)
	if query.Search != "" {
		s := "%" + query.Search + "%"
		db = db.Where("chr LIKE ? OR recessive_genes LIKE ?", s, s)
	}

	var total int64
	db.Count(&total)

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

	var results []model.ROHRegion
	err := db.Order("chr ASC, begin ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *ResultRepository) CreateROHRegions(results []model.ROHRegion) error {
	if len(results) == 0 {
		return nil
	}
	return r.db.CreateInBatches(results, 100).Error
}

// ========== Delete by TaskID (for re-import) ==========

func (r *ResultRepository) DeleteSNVIndelsByTaskID(taskID string) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.SNVIndel{}).Error
}

func (r *ResultRepository) DeleteCNVSegmentsByTaskID(taskID string) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.CNVSegment{}).Error
}

func (r *ResultRepository) DeleteCNVExonsByTaskID(taskID string) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.CNVExon{}).Error
}

func (r *ResultRepository) DeleteSTRsByTaskID(taskID string) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.STR{}).Error
}

func (r *ResultRepository) DeleteMEIsByTaskID(taskID string) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.MEIVariant{}).Error
}

func (r *ResultRepository) DeleteMTVariantsByTaskID(taskID string) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.MitochondrialVariant{}).Error
}

func (r *ResultRepository) DeleteUPDRegionsByTaskID(taskID string) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.UPDRegion{}).Error
}

func (r *ResultRepository) DeleteROHRegionsByTaskID(taskID string) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.ROHRegion{}).Error
}

func (r *ResultRepository) DeleteQCByTaskID(taskID string) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.QCResult{}).Error
}

// ROH Review/Report
func (r *ResultRepository) UpdateROHRegionReview(id string, reviewed bool, reviewer string) error {
	return r.db.Model(&model.ROHRegion{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reviewed": reviewed, "reviewed_by": reviewer,
	}).Error
}

func (r *ResultRepository) UpdateROHRegionReport(id string, reported bool, reporter string) error {
	return r.db.Model(&model.ROHRegion{}).Where("id = ?", id).Updates(map[string]interface{}{
		"reported": reported, "reported_by": reporter,
	}).Error
}

// ========== Generic variant operations ==========

// variantModel maps variant type string to its GORM model for generic operations.
var variantModel = map[string]interface{}{
	"snv-indel":   &model.SNVIndel{},
	"cnv-segment": &model.CNVSegment{},
	"cnv-exon":    &model.CNVExon{},
	"str":         &model.STR{},
	"mei":         &model.MEIVariant{},
	"mt":          &model.MitochondrialVariant{},
	"upd":         &model.UPDRegion{},
	"roh":         &model.ROHRegion{},
}

// UpdateVariantReview generically updates the review status of any variant type,
// scoped to the current task to prevent cross-task variant ID tampering.
func (r *ResultRepository) UpdateVariantReview(variantType, taskID, id string, reviewed bool, reviewer string) error {
	m, ok := variantModel[variantType]
	if !ok {
		return fmt.Errorf("unknown variant type: %s", variantType)
	}
	tx := r.db.Model(m).Where("id = ? AND task_id = ?", id, taskID).Updates(map[string]interface{}{
		"reviewed":    reviewed,
		"reviewed_by": reviewer,
	})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateVariantReport generically updates the report status of any variant type,
// scoped to the current task to prevent cross-task variant ID tampering.
func (r *ResultRepository) UpdateVariantReport(variantType, taskID, id string, reported bool, reporter string) error {
	m, ok := variantModel[variantType]
	if !ok {
		return fmt.Errorf("unknown variant type: %s", variantType)
	}
	tx := r.db.Model(m).Where("id = ? AND task_id = ?", id, taskID).Updates(map[string]interface{}{
		"reported":    reported,
		"reported_by": reporter,
	})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// VariantExists verifies the requested variant row belongs to the current task.
func (r *ResultRepository) VariantExists(variantType, taskID, id string) (bool, error) {
	m, ok := variantModel[variantType]
	if !ok {
		return false, fmt.Errorf("unknown variant type: %s", variantType)
	}
	var count int64
	if err := r.db.Model(m).Where("id = ? AND task_id = ?", id, taskID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListCNVAssessments returns persisted CNV assessments scoped to a task and
// result type. If ids is non-empty, only those variant IDs are returned.
func (r *ResultRepository) ListCNVAssessments(taskID, variantType string, ids []string) ([]model.CNVAssessment, error) {
	db := r.db.Where("task_id = ? AND variant_type = ?", taskID, variantType)
	if len(ids) > 0 {
		db = db.Where("variant_id IN ?", ids)
	}
	var assessments []model.CNVAssessment
	err := db.Order("updated_at DESC").Find(&assessments).Error
	return assessments, err
}

func (r *ResultRepository) FindCNVAssessment(taskID, variantType, variantID string) (*model.CNVAssessment, error) {
	var assessment model.CNVAssessment
	err := r.db.Where(
		"task_id = ? AND variant_type = ? AND variant_id = ?",
		taskID, variantType, variantID,
	).First(&assessment).Error
	return &assessment, err
}

// UpsertCNVAssessment inserts or replaces an assessment for one CNV variant.
func (r *ResultRepository) UpsertCNVAssessment(taskID, variantType, variantID, payload, actor string) (*model.CNVAssessment, error) {
	assessment := model.CNVAssessment{
		ID:          uuid.New().String(),
		TaskID:      taskID,
		VariantType: variantType,
		VariantID:   variantID,
		PayloadJSON: payload,
		CreatedBy:   actor,
		UpdatedBy:   actor,
	}
	if err := r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "task_id"},
			{Name: "variant_type"},
			{Name: "variant_id"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"payload_json": payload,
			"updated_by":   actor,
			"updated_at":   gorm.Expr("CURRENT_TIMESTAMP"),
		}),
	}).Create(&assessment).Error; err != nil {
		return nil, err
	}
	return r.FindCNVAssessment(taskID, variantType, variantID)
}
