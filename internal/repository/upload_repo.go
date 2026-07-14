package repository

import (
	"time"

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

// UploadFileAuditRow is the scanned row for the file-level audit list: the
// UploadFile columns plus the owning job's org (joined from upload_jobs).
type UploadFileAuditRow struct {
	UUID       string
	JobID      uint
	JobUUID    string
	FileName   string
	StorageKey string
	FileSize   int64
	ReadType   model.ReadType
	Status     model.FileStatus
	OrgID      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PaginateFilesByQuery lists upload files at file level (not nested in jobs)
// with org/user scoping via a JOIN to upload_jobs (which carries ExternalOrgID).
// Mirrors the scope switch in TaskRepository.PaginateByQuery. Returns audit rows
// that include the owning job's org_id (joined) without N+1 queries.
func (r *UploadFileRepository) PaginateFilesByQuery(q *model.UploadFileListQuery) ([]UploadFileAuditRow, int64, error) {
	db := r.db.Model(&model.UploadFile{}).
		Joins("JOIN upload_jobs ON upload_jobs.id = upload_files.job_id")

	if !q.IncludeAll {
		switch {
		case q.ExternalOrgID != "":
			db = db.Where("upload_jobs.external_org_id = ?", q.ExternalOrgID)
		case q.UserID != 0:
			db = db.Where("upload_jobs.user_id = ?", q.UserID)
		default:
			db = db.Where("1 = 0")
		}
	}
	if q.OrgID != "" {
		db = db.Where("upload_jobs.external_org_id = ?", q.OrgID)
	}
	if q.Status != "" {
		db = db.Where("upload_files.status = ?", q.Status)
	}
	if q.Search != "" {
		db = db.Where("upload_files.file_name ILIKE ?", "%"+q.Search+"%")
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

	var rows []UploadFileAuditRow
	offset := (page - 1) * pageSize
	err := db.Select(`upload_files.uuid, upload_files.job_id, upload_files.job_uuid, upload_files.file_name,
		upload_files.storage_key, upload_files.file_size, upload_files.read_type, upload_files.status,
		upload_jobs.external_org_id AS org_id, upload_files.created_at, upload_files.updated_at`).
		Order("upload_files.created_at DESC").Offset(offset).Limit(pageSize).Scan(&rows).Error
	return rows, total, err
}

// SumFileSize returns total bytes of completed (uploaded) files under the same
// scope. Used by the /upload/files/stats aggregate endpoint.
func (r *UploadFileRepository) SumFileSize(q *model.UploadFileListQuery) (int64, error) {
	db := r.db.Model(&model.UploadFile{}).
		Joins("JOIN upload_jobs ON upload_jobs.id = upload_files.job_id")

	if !q.IncludeAll {
		switch {
		case q.ExternalOrgID != "":
			db = db.Where("upload_jobs.external_org_id = ?", q.ExternalOrgID)
		case q.UserID != 0:
			db = db.Where("upload_jobs.user_id = ?", q.UserID)
		default:
			db = db.Where("1 = 0")
		}
	}
	if q.OrgID != "" {
		db = db.Where("upload_jobs.external_org_id = ?", q.OrgID)
	}
	db = db.Where("upload_files.status = ?", model.FileStatusCompleted)

	var sum int64
	err := db.Select("COALESCE(SUM(upload_files.file_size), 0)").Scan(&sum).Error
	return sum, err
}
