package repository

import (
	"github.com/bioinfo/schema-platform/internal/model"
)

type UploadJobRepository struct {
	*Repository[model.UploadJob]
}

func NewUploadJobRepository() *UploadJobRepository {
	return &UploadJobRepository{
		Repository: NewRepository[model.UploadJob](),
	}
}

func (r *UploadJobRepository) FindByUUID(uuid string) (*model.UploadJob, error) {
	return r.FindOneByCondition(map[string]interface{}{"uuid": uuid})
}

func (r *UploadJobRepository) FindByUserID(userID uint) ([]model.UploadJob, error) {
	return r.FindByCondition(map[string]interface{}{"user_id": userID})
}

func (r *UploadJobRepository) PaginateByQuery(query *model.UploadJobListQuery, userID uint) ([]model.UploadJob, int64, error) {
	db := r.db.Model(&model.UploadJob{}).Where("user_id = ?", userID)

	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if query.FileType != "" {
		db = db.Where("file_type = ?", query.FileType)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

	var jobs []model.UploadJob
	offset := (page - 1) * pageSize
	err := db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&jobs).Error
	return jobs, total, err
}

type UploadFileRepository struct {
	*Repository[model.UploadFile]
}

func NewUploadFileRepository() *UploadFileRepository {
	return &UploadFileRepository{
		Repository: NewRepository[model.UploadFile](),
	}
}

func (r *UploadFileRepository) FindByUUID(uuid string) (*model.UploadFile, error) {
	return r.FindOneByCondition(map[string]interface{}{"uuid": uuid})
}

func (r *UploadFileRepository) FindByJobID(jobID uint) ([]model.UploadFile, error) {
	return r.FindByCondition(map[string]interface{}{"job_id": jobID})
}

func (r *UploadFileRepository) DeleteByJobID(jobID uint) error {
	return r.db.Where("job_id = ?", jobID).Delete(&model.UploadFile{}).Error
}

func (r *UploadFileRepository) UpdateStatus(id uint, status model.FileStatus) error {
	return r.db.Model(&model.UploadFile{}).Where("id = ?", id).Update("status", status).Error
}

func (r *UploadFileRepository) UpdateFileSize(id uint, fileSize int64) error {
	return r.db.Model(&model.UploadFile{}).Where("id = ?", id).Update("file_size", fileSize).Error
}
