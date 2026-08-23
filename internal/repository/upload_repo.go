package repository

import (
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
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

func (r *UploadJobRepository) PaginateByQuery(query *model.UploadJobListQuery, actor model.OverlayActor) ([]model.UploadJob, int64, error) {
	db := r.db.Model(&model.UploadJob{}).Where("status <> ?", model.UploadJobStatusDeleted)
	if actor.Role != string(model.SystemRoleSuperAdmin) {
		if actor.OrgID != "" {
			db = db.Where("external_org_id = ? AND user_id = ?", actor.OrgID, actor.UserID)
		} else if actor.UserID != 0 {
			db = db.Where("external_org_id = '' AND user_id = ?", actor.UserID)
		} else {
			db = db.Where("1 = 0")
		}
	}

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
	UUID                       string
	DataAssetID                string
	JobID                      uint
	JobUUID                    string
	FileName                   string
	StorageKey                 string
	FileSize                   int64
	ReadType                   model.ReadType
	Status                     model.FileStatus
	OrgID                      string
	UploadPolicyVersion        string
	UploadPolicyAcknowledgedAt *time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// PaginateFilesByQuery lists upload files at file level (not nested in jobs)
// with org/user scoping via a JOIN to upload_jobs (which carries ExternalOrgID).
// Mirrors the scope switch in TaskRepository.PaginateByQuery. Returns audit rows
// that include the owning job's org_id (joined) without N+1 queries.
func (r *UploadFileRepository) PaginateFilesByQuery(q *model.UploadFileListQuery) ([]UploadFileAuditRow, int64, error) {
	db := r.db.Model(&model.UploadFile{}).
		Joins("JOIN upload_jobs ON upload_jobs.id = upload_files.job_id").
		Joins("LEFT JOIN data_assets ON data_assets.upload_file_id = upload_files.id")

	if !q.IncludeAll {
		switch {
		case q.ExternalOrgID != "":
			db = db.Where("upload_jobs.external_org_id = ?", q.ExternalOrgID)
		case q.UserID != 0:
			db = db.Where("upload_jobs.external_org_id = '' AND upload_jobs.user_id = ?", q.UserID)
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
	err := db.Select(`upload_files.uuid, data_assets.uuid AS data_asset_id, upload_files.job_id, upload_files.job_uuid, upload_files.file_name,
		upload_files.storage_key, upload_files.file_size, upload_files.read_type, upload_files.status,
		upload_jobs.external_org_id AS org_id, upload_jobs.upload_policy_version,
		upload_jobs.upload_policy_acknowledged_at, upload_files.created_at, upload_files.updated_at`).
		Order("upload_files.created_at DESC").Offset(offset).Limit(pageSize).Scan(&rows).Error
	return rows, total, err
}

func (r *UploadFileRepository) completedStorageAssets(q *model.UploadFileListQuery) *gorm.DB {
	db := r.db.Model(&model.DataAsset{})

	if !q.IncludeAll {
		switch {
		case q.ExternalOrgID != "":
			db = db.Where("data_assets.external_org_id = ?", q.ExternalOrgID)
		case q.UserID != 0:
			db = db.Where("data_assets.external_org_id = '' AND data_assets.created_by = ?", q.UserID)
		default:
			db = db.Where("1 = 0")
		}
	}
	if q.OrgID != "" {
		db = db.Where("data_assets.external_org_id = ?", q.OrgID)
	}
	db = db.Where("data_assets.status = ?", model.FileStatusCompleted)
	return db
}

// CompletedStorageStats returns count and bytes from the same authoritative
// completed-asset inventory. It includes BED assets hidden from Data Center
// and scanner-discovered assets that do not have an upload_files row.
func (r *UploadFileRepository) CompletedStorageStats(q *model.UploadFileListQuery) (int64, int64, error) {
	var stats struct {
		Total      int64
		TotalBytes int64
	}
	err := r.completedStorageAssets(q).
		Select("COUNT(*) AS total, COALESCE(SUM(data_assets.file_size), 0) AS total_bytes").
		Scan(&stats).Error
	return stats.Total, stats.TotalBytes, err
}
