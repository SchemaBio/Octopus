package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type personalViewState struct {
	SearchQuery    string                 `json:"searchQuery"`
	Filters        map[string]interface{} `json:"filters"`
	ColumnFilters  []model.ParquetFilter  `json:"columnFilters"`
	SortColumn     string                 `json:"sortColumn"`
	SortDirection  string                 `json:"sortDirection"`
	Page           int                    `json:"page"`
	PageSize       int                    `json:"pageSize"`
	GeneListID     string                 `json:"geneListId"`
	VisibleColumns []string               `json:"visibleColumns"`
}

func validatePersonalView(raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > 32768 {
		return fmt.Errorf("invalid saved view size")
	}
	var state personalViewState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return fmt.Errorf("invalid saved view fields")
	}
	if len(state.SearchQuery) > 256 || len(state.Filters) > 40 || len(state.ColumnFilters) > 40 || len(state.VisibleColumns) > 256 || state.Page < 1 || state.PageSize < 1 || state.PageSize > 200 || (state.SortDirection != "" && state.SortDirection != "asc" && state.SortDirection != "desc") {
		return fmt.Errorf("saved view exceeds limits")
	}
	return nil
}
func (h *ResultHandler) ListPersonalViews(c *gin.Context) {
	if !validPersonalViewTable(c.Param("table")) {
		ErrorBadRequest(c, "invalid result table")
		return
	}
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	user, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	var rows = []model.ResultSavedView{}
	if err := database.DB.WithContext(c.Request.Context()).Where("tenant_id=? AND task_uuid=? AND user_id=? AND \"table\"=?", model.TenantIDForTask(task), task.UUID, user, c.Param("table")).Order("updated_at DESC").Find(&rows).Error; err != nil {
		ErrorInternal(c, "saved view read failed")
		return
	}
	c.Header("Cache-Control", "no-store")
	Success(c, rows)
}
func (h *ResultHandler) SavePersonalView(c *gin.Context) {
	if !validPersonalViewTable(c.Param("table")) {
		ErrorBadRequest(c, "invalid result table")
		return
	}
	task, ok := requireTaskAccess(c, h.taskRepo, c.Param("id"))
	if !ok {
		return
	}
	user, _, _, ok := middleware.GetCurrentUser(c)
	if !ok {
		ErrorUnauthorized(c, "Unauthorized")
		return
	}
	var req struct {
		Name            string          `json:"name"`
		State           json.RawMessage `json:"state"`
		ExpectedVersion uint64          `json:"expectedVersion"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Name) == "" || len(req.Name) > 80 || validatePersonalView(req.State) != nil {
		ErrorBadRequest(c, "invalid saved view")
		return
	}
	name := strings.TrimSpace(req.Name)
	var saved model.ResultSavedView
	err := database.DB.WithContext(c.Request.Context()).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var current model.ResultSavedView
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND task_uuid=? AND user_id=? AND \"table\"=? AND name=?", model.TenantIDForTask(task), task.UUID, user, c.Param("table"), name)
		err := q.First(&current).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if current.Version != req.ExpectedVersion {
			return fmt.Errorf("view conflict")
		}
		saved = current
		if current.ID == "" {
			saved = model.ResultSavedView{ID: uuid.NewString(), TenantID: model.TenantIDForTask(task), TaskUUID: task.UUID, UserID: user, Table: c.Param("table"), Name: name}
		}
		saved.Version++
		saved.StateJSON = string(req.State)
		saved.UpdatedAt = time.Now().UTC()
		var result *gorm.DB
		if current.ID == "" {
			result = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&saved)
		} else {
			result = tx.Model(&model.ResultSavedView{}).Where("id=? AND version=?", saved.ID, req.ExpectedVersion).Updates(map[string]interface{}{"state_json": saved.StateJSON, "version": saved.Version, "updated_at": saved.UpdatedAt})
		}
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("view conflict")
		}
		return nil
	})
	if err != nil {
		ErrorConflict(c, "筛选方案已更新，请刷新方案列表后再保存")
		return
	}
	Success(c, saved)
}

func validPersonalViewTable(table string) bool {
	switch table {
	case "snv-indel", "cnv-segment", "cnv-exon", "str", "mei", "mt", "upd", "roh":
		return true
	}
	return false
}
