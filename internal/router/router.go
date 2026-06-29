package router

import (
	"net/http"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/handler"
	"github.com/bioinfo/schema-platform/internal/middleware"

	"github.com/gin-gonic/gin"
)

// redirectAuthLegacy issues a 308 Permanent Redirect to the new /auth/* path.
// 308 preserves the request method and body for legacy POST clients.
func redirectAuthLegacy(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Redirect(http.StatusPermanentRedirect, target)
	}
}

func New(cfg *config.Config) *gin.Engine {
	gin.SetMode(cfg.Server.Mode)

	r := gin.New()
	r.Use(middleware.Logger())
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS(&cfg.Server))
	r.Use(middleware.CSRF())

	// Health check (public)
	r.GET("/health", handler.HealthCheck)

	// API v1
	v1 := r.Group("/api/v1")
	{
		// ========== Authentication (public) ==========
		authHandler := handler.NewAuthHandler(cfg)
		auth := v1.Group("/auth")
		{
			auth.POST("/login", authHandler.Login)
			auth.POST("/register", authHandler.Register)
			auth.POST("/refresh", authHandler.Refresh)
			auth.POST("/logout", authHandler.Logout)
			auth.POST("/forgot-password", authHandler.ForgotPassword)
			auth.POST("/reset-password", authHandler.ResetPassword)
		}

		// Legacy auth routes (pre-/auth/ prefix) kept for older deploy scripts.
		v1.POST("/login", redirectAuthLegacy("/api/v1/auth/login"))
		v1.POST("/register", redirectAuthLegacy("/api/v1/auth/register"))
		v1.POST("/refresh", redirectAuthLegacy("/api/v1/auth/refresh"))
		v1.POST("/logout", redirectAuthLegacy("/api/v1/auth/logout"))
		v1.POST("/forgot-password", redirectAuthLegacy("/api/v1/auth/forgot-password"))
		v1.POST("/reset-password", redirectAuthLegacy("/api/v1/auth/reset-password"))

		// Protected auth routes
		authProtected := v1.Group("/auth")
		authProtected.Use(middleware.JWTAuth(cfg))
		{
			authProtected.GET("/me", authHandler.Me)
		}

		// ========== User management ==========
		userHandler := handler.NewUserHandler(cfg)
		users := v1.Group("/users")
		users.Use(middleware.JWTAuth(cfg))
		{
			users.GET("", userHandler.ListUsers)
			users.GET("/:id", userHandler.GetUser)
		}
		// Admin-only user management
		usersAdmin := v1.Group("/users")
		usersAdmin.Use(middleware.JWTAuth(cfg))
		usersAdmin.Use(middleware.RequireAdmin())
		{
			usersAdmin.POST("", userHandler.CreateUser)
			usersAdmin.PUT("/:id", userHandler.UpdateUser)
			usersAdmin.DELETE("/:id", userHandler.DeleteUser)
		}

		// ========== WDL templates (public read) ==========
		templates := v1.Group("/templates")
		{
			templates.GET("", handler.ListTemplates)
			templates.GET("/:name", handler.GetTemplate)
			templates.GET("/:name/inputs", handler.GetTemplateInputs)
		}

		// ========== Sepiida integration ==========
		sepiidaHandler := handler.NewSepiidaHandler(cfg)
		sepiida := v1.Group("/sepiida")
		sepiida.Use(middleware.JWTAuth(cfg))
		{
			sepiida.GET("/health", sepiidaHandler.HealthCheck)
		}
		// Admin-only sepiida workflows
		sepiidaAdmin := v1.Group("/sepiida")
		sepiidaAdmin.Use(middleware.JWTAuth(cfg))
		sepiidaAdmin.Use(middleware.RequireAdmin())
		{
			sepiidaAdmin.GET("/workflows", sepiidaHandler.ListWorkflows)
		}

		// ========== Task management (protected) ==========
		taskHandler := handler.NewTaskHandler(cfg)
		aiHandler := handler.NewAIHandler(cfg)
		exportHandler := handler.NewExportHandler(cfg)
		reportHandler := handler.NewReportHandler(cfg)
		tasks := v1.Group("/tasks")
		tasks.Use(middleware.JWTAuth(cfg))
		{
			tasks.POST("", taskHandler.CreateTask)
			tasks.GET("", taskHandler.ListTasks)
			tasks.GET("/:id", taskHandler.GetTask)
			tasks.PUT("/:id", taskHandler.UpdateTask)
			tasks.GET("/:id/progress", taskHandler.GetTaskProgress)
			tasks.DELETE("/:id", taskHandler.CancelTask)
			tasks.GET("/:id/logs", taskHandler.GetTaskLogs)
			tasks.GET("/:id/sample", taskHandler.GetTaskSample)
			tasks.POST("/:id/start", taskHandler.StartTask)
			tasks.POST("/:id/stop", taskHandler.StopTask)
			tasks.POST("/:id/retry", taskHandler.RetryTask)
			tasks.POST("/:id/results/import/retry", taskHandler.RetryResultImport)
			tasks.POST("/:id/ai-evaluate", aiHandler.Evaluate)

			// AI proxy for frontend page-agent.
			aiProxy := v1.Group("/ai/proxy")
			aiProxy.Use(middleware.JWTAuth(cfg))
			{
				aiProxy.Any("/*path", aiHandler.ProxyAgent)
			}
			// Export
			tasks.GET("/:id/export/excel", exportHandler.ExportExcel)
			tasks.GET("/:id/export/parquet", exportHandler.ExportParquet)
			tasks.GET("/:id/export/vcf", exportHandler.ExportVCF)
			tasks.GET("/:id/export/mt-vcf", exportHandler.ExportMTVCF)
			// Reports
			tasks.GET("/:id/reports", reportHandler.ListReports)
			tasks.POST("/:id/reports", reportHandler.CreateReport)
			tasks.POST("/:id/reports/upload", reportHandler.UploadReport)
			tasks.PATCH("/:id/reports/:reportId/status", reportHandler.UpdateReportStatus)
			tasks.DELETE("/:id/reports/:reportId", reportHandler.DeleteReport)
			tasks.GET("/:id/reports/:reportId/download-url", reportHandler.GetReportDownloadURL)
		}

		// ========== Report templates ==========
		reportTemplates := v1.Group("/report-templates")
		reportTemplates.Use(middleware.JWTAuth(cfg))
		{
			reportTemplates.GET("", reportHandler.ListTemplates)
		}
		// Admin-only report template management
		reportTemplatesAdmin := v1.Group("/report-templates")
		reportTemplatesAdmin.Use(middleware.JWTAuth(cfg))
		reportTemplatesAdmin.Use(middleware.RequireAdmin())
		{
			reportTemplatesAdmin.POST("", reportHandler.CreateTemplate)
		}

		// ========== Result management (protected) ==========
		resultHandler := handler.NewResultHandler(cfg)
		results := v1.Group("/tasks/:id/results")
		results.Use(middleware.JWTAuth(cfg))
		{
			results.GET("/qc", resultHandler.GetQC)
			results.GET("/snv-indel", resultHandler.ListSNVIndels)
			results.GET("/cnv-segment", resultHandler.ListCNVSegments)
			results.GET("/cnv-exon", resultHandler.ListCNVExons)
			results.GET("/str", resultHandler.ListSTRs)
			results.GET("/mei", resultHandler.ListMEIs)
			results.GET("/mt", resultHandler.ListMTVariants)
			results.GET("/upd", resultHandler.ListUPDRegions)
			results.GET("/roh", resultHandler.ListROHRegions)
			results.PUT("/:type/:vid/review", resultHandler.ReviewVariant)
			results.PUT("/:type/:vid/report", resultHandler.ReportVariant)
		}

		// ========== Archive management (protected) ==========
		archiveHandler := handler.NewArchiveHandler(cfg)
		archive := v1.Group("/archive")
		archive.Use(middleware.JWTAuth(cfg))
		{
			archive.GET("/:uuid", archiveHandler.ArchiveStatus)
			archive.GET("/:uuid/outputs", archiveHandler.ListOutputKeys)
			archive.GET("/:uuid/output/:key", archiveHandler.QueryOutput)
			archive.GET("/:uuid/status", archiveHandler.GetStatus)
			archive.PUT("/:uuid/status", archiveHandler.UpdateStatus)
			archive.GET("/:uuid/parquet", archiveHandler.GetParquet)
			archive.GET("/:uuid/data", archiveHandler.GetCombinedData)
			archive.POST("/:uuid/import", archiveHandler.ImportToDatabase)
		}

		// ========== Sample management (protected) ==========
		sampleHandler := handler.NewSampleHandler(cfg)
		samples := v1.Group("/samples")
		samples.Use(middleware.JWTAuth(cfg))
		{
			samples.POST("", sampleHandler.CreateSample)
			samples.GET("", sampleHandler.ListSamples)
			samples.GET("/:id", sampleHandler.GetSample)
			samples.PUT("/:id", sampleHandler.UpdateSample)
			samples.DELETE("/:id", sampleHandler.DeleteSample)
		}

		// ========== Project management (protected) ==========
		projectHandler := handler.NewProjectHandler(cfg)
		projects := v1.Group("/projects")
		projects.Use(middleware.JWTAuth(cfg))
		{
			projects.POST("", projectHandler.CreateProject)
			projects.GET("", projectHandler.ListProjects)
			projects.GET("/:id", projectHandler.GetProject)
			projects.GET("/:id/summary", projectHandler.GetProjectSummary)
			projects.PUT("/:id", projectHandler.UpdateProject)
			projects.DELETE("/:id", projectHandler.DeleteProject)
		}

		// ========== Pedigree management (protected) ==========
		pedigreeHandler := handler.NewPedigreeHandler(cfg)
		pedigrees := v1.Group("/pedigrees")
		pedigrees.Use(middleware.JWTAuth(cfg))
		{
			pedigrees.POST("", pedigreeHandler.Create)
			pedigrees.GET("", pedigreeHandler.List)
			pedigrees.GET("/:id", pedigreeHandler.Get)
			pedigrees.PUT("/:id", pedigreeHandler.Update)
			pedigrees.DELETE("/:id", pedigreeHandler.Delete)
			pedigrees.PUT("/:id/proband/:memberId", pedigreeHandler.SetProband)
			pedigrees.GET("/:id/members", pedigreeHandler.ListMembers)
			pedigrees.POST("/:id/members", pedigreeHandler.CreateMember)
			pedigrees.GET("/:id/members/:memberId", pedigreeHandler.GetMember)
			pedigrees.PUT("/:id/members/:memberId", pedigreeHandler.UpdateMember)
			pedigrees.DELETE("/:id/members/:memberId", pedigreeHandler.DeleteMember)
		}

		// ========== Gene List management (protected) ==========
		geneListHandler := handler.NewGeneListHandler(cfg)
		geneLists := v1.Group("/gene-lists")
		geneLists.Use(middleware.JWTAuth(cfg))
		{
			geneLists.POST("", geneListHandler.Create)
			geneLists.GET("", geneListHandler.List)
			geneLists.GET("/:id", geneListHandler.Get)
			geneLists.PUT("/:id", geneListHandler.Update)
			geneLists.DELETE("/:id", geneListHandler.Delete)
		}

		// ========== Pipeline management (protected) ==========
		pipelineHandler := handler.NewPipelineHandler(cfg)
		pipelines := v1.Group("/pipelines")
		pipelines.Use(middleware.JWTAuth(cfg))
		{
			pipelines.POST("", pipelineHandler.CreatePipeline)
			pipelines.GET("", pipelineHandler.ListPipelines)
			pipelines.GET("/:id", pipelineHandler.GetPipeline)
			pipelines.PUT("/:id", pipelineHandler.UpdatePipeline)
			pipelines.DELETE("/:id", pipelineHandler.DeletePipeline)
		}

		// ========== Parquet data API (protected) ==========
		parquetHandler := handler.NewParquetHandler(cfg)
		parquet := v1.Group("/tasks/:id/parquet")
		parquet.Use(middleware.JWTAuth(cfg))
		{
			parquet.GET("", parquetHandler.ListTables)
			parquet.GET("/:table/rows", parquetHandler.GetTableRows)
		}

		// ========== History (protected) ==========
		historyHandler := handler.NewHistoryHandler(cfg)
		history := v1.Group("/history")
		history.Use(middleware.JWTAuth(cfg))
		{
			history.GET("/snv-indel", historyHandler.ListGroupedSNVIndels)
			history.GET("/cnv-segment", historyHandler.ListGroupedCNVSegments)
			history.GET("/cnv-exon", historyHandler.ListGroupedCNVExons)
			history.GET("/str", historyHandler.ListGroupedSTRs)
			history.GET("/mei", historyHandler.ListGroupedMEIs)
			history.GET("/mt", historyHandler.ListGroupedMTVariants)
			history.GET("/upd", historyHandler.ListGroupedUPDRegions)
		}

		// ========== Dashboard (protected) ==========
		dashboardHandler := handler.NewDashboardHandler(cfg)
		dashboard := v1.Group("/dashboard")
		dashboard.Use(middleware.JWTAuth(cfg))
		{
			dashboard.GET("/stats", dashboardHandler.GetStats)
		}

		// ========== Data Upload (protected) ==========
		uploadHandler := handler.NewUploadHandler(cfg)
		uploads := v1.Group("/upload")
		uploads.Use(middleware.JWTAuth(cfg))
		{
			uploads.POST("/jobs", uploadHandler.CreateJob)
			uploads.GET("/jobs", uploadHandler.ListJobs)
			uploads.GET("/jobs/:uuid", uploadHandler.GetJob)
			uploads.DELETE("/jobs/:uuid", uploadHandler.DeleteJob)
			uploads.POST("/local/:file_uuid", uploadHandler.UploadLocal)
			uploads.GET("/files/:file_uuid/download", uploadHandler.GetDownloadURL)
		}
	}

	return r
}
