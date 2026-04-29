package router

import (
	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/handler"
	"github.com/bioinfo/schema-platform/internal/middleware"

	"github.com/gin-gonic/gin"
)

func New(cfg *config.Config) *gin.Engine {
	gin.SetMode(cfg.Server.Mode)

	r := gin.New()
	r.Use(middleware.Logger())
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS())

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
		}

		// Protected auth routes
		authProtected := v1.Group("/auth")
		authProtected.Use(middleware.JWTAuth(cfg))
		{
			authProtected.GET("/me", authHandler.Me)
		}

		// ========== User management (protected) ==========
		userHandler := handler.NewUserHandler(cfg)
		users := v1.Group("/users")
		users.Use(middleware.JWTAuth(cfg))
		{
			users.GET("", userHandler.ListUsers)
			users.GET("/:id", userHandler.GetUser)
			users.POST("", userHandler.CreateUser)
			users.PUT("/:id", userHandler.UpdateUser)
			users.DELETE("/:id", userHandler.DeleteUser)
		}

		// ========== Organization management (protected) ==========
		orgHandler := handler.NewOrgHandler(cfg)
		orgs := v1.Group("/orgs")
		orgs.Use(middleware.JWTAuth(cfg))
		{
			orgs.GET("", orgHandler.ListOrganizations)
			orgs.POST("/switch", orgHandler.SwitchOrganization)
		}

		// ========== WDL templates (public read) ==========
		templates := v1.Group("/templates")
		{
			templates.GET("", handler.ListTemplates)
			templates.GET("/:name", handler.GetTemplate)
		}

		// ========== Sepiida integration (protected) ==========
		sepiidaHandler := handler.NewSepiidaHandler(cfg)
		sepiida := v1.Group("/sepiida")
		sepiida.Use(middleware.JWTAuth(cfg))
		{
			sepiida.GET("/health", sepiidaHandler.HealthCheck)
			sepiida.GET("/workflows", sepiidaHandler.ListWorkflows)
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
			tasks.POST("/:id/ai-evaluate", aiHandler.Evaluate)
			// Export
			tasks.GET("/:id/export/excel", exportHandler.ExportExcel)
			tasks.GET("/:id/export/parquet", exportHandler.ExportParquet)
			tasks.GET("/:id/export/vcf", exportHandler.ExportVCF)
			tasks.GET("/:id/export/mt-vcf", exportHandler.ExportMTVCF)
			// Reports
			tasks.GET("/:id/reports", reportHandler.ListReports)
			tasks.POST("/:id/reports", reportHandler.CreateReport)
		}

		// ========== Report templates (protected) ==========
		reportTemplates := v1.Group("/report-templates")
		reportTemplates.Use(middleware.JWTAuth(cfg))
		{
			reportTemplates.GET("", reportHandler.ListTemplates)
			reportTemplates.POST("", reportHandler.CreateTemplate)
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
	}

	return r
}
