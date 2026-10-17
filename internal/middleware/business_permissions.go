package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/gin-gonic/gin"
)

func IsSharedInstance(c *gin.Context) bool { return c.GetBool("shared_instance") }

// Invoked after authentication, before any business handler or side effect.
func enforceBusinessPermissions(c *gin.Context, cfg *config.Config) bool {
	if cfg == nil || cfg.Edition != "opensource" {
		return true
	}
	if c.GetBool("external_auth") {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "external identity is not valid for self-hosted business access"})
		return false
	}
	c.Set("shared_instance", true)
	permission, extra, err := requestPermission(c)
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "permission service unavailable" {
			status = http.StatusServiceUnavailable
		}
		if strings.HasSuffix(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return false
	}
	if permission == "" {
		return true
	}
	db := database.GetDB()
	if db == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "permission service unavailable"})
		return false
	}
	id, _, _, ok := GetCurrentUser(c)
	var user model.User
	if !ok || db.First(&user, id).Error != nil || !user.IsActive {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Account is not active"})
		return false
	}
	for _, required := range append([]string{permission}, extra...) {
		if !model.HasPermission(user.EffectivePermissions(), required) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "permission_denied", "required_permission": required, "error": "business permission required"})
			return false
		}
	}
	return true
}

func permissionBody(c *gin.Context) (map[string]json.RawMessage, error) {
	if c.Request.Body == nil {
		return nil, fmt.Errorf("request body is required")
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, (16<<20)+1))
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil || len(raw) > 16<<20 {
		return nil, fmt.Errorf("invalid or oversized request body")
	}
	var body map[string]json.RawMessage
	if err = json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("invalid request body")
	}
	return body, nil
}

func jsonString(body map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(body[key], &value)
	return value
}

func taskPermission(template string) string {
	if template == "baseline_fix" {
		return "baselines.manage"
	}
	return "tasks.manage"
}

// Business methods are classified semantically: POST queries/exports remain read-only.
func requestPermission(c *gin.Context) (string, []string, error) {
	path := strings.TrimPrefix(c.FullPath(), "/api/v1")
	method := c.Request.Method
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return "", nil, nil
	}
	simple := map[string]string{"/samples": "samples.write", "/projects": "samples.write", "/pedigrees": "samples.write", "/pipelines": "pipelines.manage", "/gene-lists": "gene_lists.manage", "/cnv-baselines": "baselines.manage", "/report-templates": "report_services.manage", "/admin/workflow-configs": "pipelines.manage"}
	for prefix, permission := range simple {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return permission, nil, nil
		}
	}
	if strings.HasPrefix(path, "/upload/") || strings.HasPrefix(path, "/data/assets/") {
		var fileType string
		extra := []string{}
		if path == "/upload/jobs" && method == http.MethodPost {
			body, err := permissionBody(c)
			if err != nil {
				return "", nil, err
			}
			fileType = jsonString(body, "file_type")
			var files []model.UploadFileCreateItem
			if err := json.Unmarshal(body["files"], &files); err != nil {
				return "", nil, fmt.Errorf("invalid upload files")
			}
			for _, file := range files {
				if file.ReadType == model.ReadTypeBed && fileType != "bed" {
					return "", nil, fmt.Errorf("BED files require the BED upload type")
				}
			}

			if jsonString(body, "sample_id") != "" || jsonString(body, "internal_id") != "" {
				extra = append(extra, "samples.write")
			}
		} else {
			db := database.GetDB()
			if db == nil {
				return "", nil, fmt.Errorf("permission service unavailable")
			}
			if strings.HasPrefix(path, "/data/") {
				var asset model.DataAsset
				if err := db.Where("uuid = ? AND COALESCE(external_org_id, '') = ''", c.Param("uuid")).First(&asset).Error; err != nil {
					return "", nil, fmt.Errorf("data asset not found")
				}
				if c.Request.Method == http.MethodPut && asset.ReadType != model.ReadTypeBed {
					extra = append(extra, "samples.write")
				}
				if asset.ReadType == model.ReadTypeBed {
					fileType = "bed"
				}
			} else {
				var job model.UploadJob
				if c.Param("uuid") != "" {
					if err := db.Where("uuid = ? AND COALESCE(external_org_id, '') = ''", c.Param("uuid")).First(&job).Error; err != nil {
						return "", nil, fmt.Errorf("upload job not found")
					}
				} else {
					var file model.UploadFile
					if err := db.Where("uuid = ?", c.Param("file_uuid")).First(&file).Error; err != nil {
						return "", nil, fmt.Errorf("upload file not found")
					}
					if err := db.Where("id = ? AND COALESCE(external_org_id, '') = ''", file.JobID).First(&job).Error; err != nil {
						return "", nil, fmt.Errorf("upload job not found")
					}
				}
				fileType = string(job.FileType)
				// Historical jobs may predate strict file-type validation.
				// Classify stored children as well as their parent job.
				var bedFiles int64
				if err := db.Model(&model.UploadFile{}).Where("job_id = ? AND read_type = ?", job.ID, model.ReadTypeBed).Count(&bedFiles).Error; err != nil {
					return "", nil, fmt.Errorf("permission service unavailable")
				}
				if bedFiles > 0 && fileType != "bed" {
					extra = append(extra, "bed.manage")
				}
				if job.SampleID != "" || job.InternalID != "" {
					extra = append(extra, "samples.write")
				}
			}
		}
		if strings.EqualFold(fileType, "bed") {
			return "bed.manage", extra, nil
		}
		return "data.manage", extra, nil
	}
	if strings.HasPrefix(path, "/ai-assistance/requests") || path == "/ai/proxy/*path" {
		return "interpretation_reports.manage", nil, nil
	}
	if strings.HasPrefix(path, "/archive/") {
		return "interpretation_reports.manage", nil, nil
	}
	if path == "/tasks" {
		body, err := permissionBody(c)
		if err != nil {
			return "", nil, err
		}
		// The generic endpoint must never turn the baseline permission into ordinary task access.
		if jsonString(body, "template") == "baseline_fix" {
			return "tasks.manage", []string{"baselines.manage"}, nil
		}
		return "tasks.manage", nil, nil
	}
	if path == "/tasks/estimate" || path == "/tasks/batch/preview" {
		return "", nil, nil
	}
	if strings.HasPrefix(path, "/tasks/") {
		if strings.Contains(path, "/views") {
			return "", nil, nil
		} // Personal preferences.
		for _, suffix := range []string{"/query", "/export", "/igv/urls", "/result-package/prepare", "/downloads/prepare", "/downloads/quote", "/downloads/issue"} {
			if strings.HasSuffix(path, suffix) {
				return "", nil, nil
			}
		}
		if strings.Contains(path, "/results/") && !strings.Contains(path, "/results/import/retry") || strings.Contains(path, "/reports") || strings.HasSuffix(path, "/interpretation-completion") || strings.HasSuffix(path, "/ai-evaluate") {
			return "interpretation_reports.manage", nil, nil
		}
		if c.Param("id") == "" {
			return "tasks.manage", nil, nil
		}
		db := database.GetDB()
		if db == nil {
			return "", nil, fmt.Errorf("permission service unavailable")
		}
		var task model.Task
		if err := db.Where("uuid = ? AND COALESCE(external_org_id, '') = ''", c.Param("id")).First(&task).Error; err != nil {
			return "", nil, fmt.Errorf("task not found")
		}
		return taskPermission(task.Template), nil, nil
	}
	return "", nil, nil
}

func RequireAdminInSaaS(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg == nil || cfg.Edition != "opensource" {
			RequireAdmin()(c)
		}
	}
}
