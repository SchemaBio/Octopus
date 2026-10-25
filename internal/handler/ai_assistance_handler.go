package handler

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/SchemaBio/Octopus/internal/assistance"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"github.com/SchemaBio/Octopus/internal/service"
	"github.com/gin-gonic/gin"
)

type AIAssistanceHandler struct {
	svc   *assistance.Service
	tasks *repository.TaskRepository
	saas  bool
}

func NewAIAssistanceHandler(cfg *config.Config) *AIAssistanceHandler {
	return &AIAssistanceHandler{assistance.New(database.GetDB(), cfg.ExternalAuth.Enabled, cfg.Overlay.BaseURL, cfg.Overlay.SharedSecret), repository.NewTaskRepository(), cfg.ExternalAuth.Enabled}
}
func aiIdentity(c *gin.Context) (string, string, uint) {
	user, _, _, _ := middleware.GetCurrentUser(c)
	org, _ := middleware.GetCurrentOrg(c)
	return model.TenantIDForIdentity(org, user), org, user
}
func (h *AIAssistanceHandler) Settings(c *gin.Context) {
	_, _, role, _ := middleware.GetCurrentUser(c)
	out, err := h.svc.Settings(c.Request.Context(), role == string(model.SystemRoleSuperAdmin))
	if err != nil {
		Error(c, 503, "AI 服务不可用")
		return
	}
	Success(c, out)
}

// A POST body keeps phenotype text out of URLs and access logs. This is a
// cache read only: it does not contact SepiLens or the configured model.
func (h *AIAssistanceHandler) CachedHPO(c *gin.Context) {
	var in struct {
		Diagnosis string `json:"diagnosis"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Diagnosis) == "" || utf8.RuneCountInString(in.Diagnosis) > 10000 {
		ErrorBadRequest(c, "表型文本格式无效")
		return
	}
	scope, _, user := aiIdentity(c)
	result, err := h.svc.CachedHPO(c.Request.Context(), scope, user, in.Diagnosis)
	if err != nil {
		Error(c, 503, "已保存的 HPO 结果暂不可用")
		return
	}
	Success(c, result)
}
func (h *AIAssistanceHandler) SaveSettings(c *gin.Context) {
	var draft assistance.Draft
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if c.ShouldBindJSON(&draft) != nil {
		ErrorBadRequest(c, "AI 配置格式无效")
		return
	}
	if h.saas && !assistance.ValidateReason(draft.Reason) {
		ErrorBadRequest(c, "操作原因不能超过 500 字")
		return
	}
	_, org, user := aiIdentity(c)
	out, err := h.svc.SaveSettingsAs(c.Request.Context(), draft, user, org)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, out)
}

func (h *AIAssistanceHandler) TestProvider(c *gin.Context) {
	var draft assistance.Draft
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if c.ShouldBindJSON(&draft) != nil || !assistance.ValidateReason(draft.Reason) {
		ErrorBadRequest(c, "模型配置格式无效或操作原因超过 500 字")
		return
	}
	_, org, user := aiIdentity(c)
	states, err := h.svc.TestProvider(c.Request.Context(), draft, user, org)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	Success(c, states)
}

// Decode before the existing advisory-lock interpretation guard, which reads
// the server-validated task UUID from route parameters.
func (h *AIAssistanceHandler) DecodeInput(c *gin.Context) {
	var in assistance.Input
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if c.ShouldBindJSON(&in) != nil {
		ErrorBadRequest(c, "AI 请求格式无效")
		c.Abort()
		return
	}
	if _, ok := assistance.Engines[in.Feature]; !ok {
		ErrorBadRequest(c, "不支持的 AI 功能")
		c.Abort()
		return
	}
	if in.Feature != "hpo" && in.TaskID == "" {
		ErrorBadRequest(c, "位点评估需要关联任务")
		c.Abort()
		return
	}
	if in.TaskID != "" {
		c.Params = append(c.Params, gin.Param{Key: "id", Value: in.TaskID})
	}
	c.Set("ai_input", in)
}
func (h *AIAssistanceHandler) Generate(c *gin.Context) {
	value, ok := c.Get("ai_input")
	if !ok {
		ErrorBadRequest(c, "AI 请求格式无效")
		return
	}
	in := value.(assistance.Input)
	if in.Feature == "acmg" || in.Feature == "svcv4" {
		task, allowed := requireTaskAccess(c, h.tasks, in.TaskID)
		if !allowed {
			return
		}
		if reference := service.TaskReferenceGenome(task); reference != "" {
			in.Reference = reference
		}
		if task.SampleID != "" {
			sample, err := repository.NewSampleRepository().FindScopedByUUID(task.SampleID, taskActorFromContext(c))
			if err != nil {
				ErrorNotFound(c, "关联样本不可用")
				return
			}
			in.SampleContext = acmgTaskSampleContext(sample, task)
		}
	}
	scope, org, user := aiIdentity(c)
	r, err := h.svc.Submit(c.Request.Context(), scope, org, user, strings.TrimSpace(c.GetHeader("Idempotency-Key")), in)
	if err != nil {
		status := 400
		if errors.Is(err, assistance.ErrConflict) {
			status = 409
		}
		if errors.Is(err, assistance.ErrUnavailable) {
			status = 503
		}
		Error(c, status, err.Error())
		return
	}
	view, err := h.svc.View(*r)
	if err != nil {
		Error(c, 503, "AI 结果暂不可用")
		return
	}
	c.JSON(202, gin.H{"data": view})
}
func (h *AIAssistanceHandler) authorized(c *gin.Context) (assistance.Request, bool) {
	scope, _, user := aiIdentity(c)
	r, err := h.svc.Get(scope, user, c.Param("requestID"))
	if err != nil {
		ErrorNotFound(c, "AI 请求不存在")
		return r, false
	}
	if r.TaskID != "" {
		if _, ok := requireTaskAccess(c, h.tasks, r.TaskID); !ok {
			return r, false
		}
	}
	return r, true
}
func (h *AIAssistanceHandler) Get(c *gin.Context) {
	r, ok := h.authorized(c)
	if !ok {
		return
	}
	v, err := h.svc.View(r)
	if err != nil {
		Error(c, 503, "AI 结果暂不可用")
		return
	}
	Success(c, v)
}
func (h *AIAssistanceHandler) Cancel(c *gin.Context) {
	r, ok := h.authorized(c)
	if !ok {
		return
	}
	if err := h.svc.Cancel(r); err != nil {
		ErrorInternal(c, "取消失败")
		return
	}
	h.Get(c)
}

func (h *AIAssistanceHandler) DeleteResult(c *gin.Context) {
	r, ok := h.authorized(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteResult(r); err != nil {
		Error(c, 409, "请求尚未完成或正在对账，不能删除")
		return
	}
	h.Get(c)
}
