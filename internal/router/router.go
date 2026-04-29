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
		// Authentication (public)
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

		// WDL templates (public read)
		templates := v1.Group("/templates")
		{
			templates.GET("", handler.ListTemplates)
			templates.GET("/:name", handler.GetTemplate)
		}

		// Sepiida integration (protected)
		sepiidaHandler := handler.NewSepiidaHandler(cfg)
		sepiida := v1.Group("/sepiida")
		sepiida.Use(middleware.JWTAuth(cfg))
		{
			sepiida.GET("/health", sepiidaHandler.HealthCheck)
			sepiida.GET("/workflows", sepiidaHandler.ListWorkflows)
		}

		// Task management (protected)
		taskHandler := handler.NewTaskHandler(cfg)
		tasks := v1.Group("/tasks")
		tasks.Use(middleware.JWTAuth(cfg))
		{
			tasks.POST("", taskHandler.CreateTask)
			tasks.GET("", taskHandler.ListTasks)
			tasks.GET("/:id", taskHandler.GetTask)
			tasks.GET("/:id/progress", taskHandler.GetTaskProgress)
			tasks.DELETE("/:id", taskHandler.CancelTask)
			tasks.GET("/:id/logs", taskHandler.GetTaskLogs)
		}

		// Archive management (protected)
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

		// Sample management (protected)
		sampleHandler := handler.NewSampleHandler(cfg)
		samples := v1.Group("/samples")
		samples.Use(middleware.JWTAuth(cfg))
		{
			samples.POST("", sampleHandler.CreateSample)
			samples.GET("", sampleHandler.ListSamples)
			samples.POST("/assign", sampleHandler.AssignProject)
			samples.GET("/:id", sampleHandler.GetSample)
			samples.PUT("/:id", sampleHandler.UpdateSample)
			samples.DELETE("/:id", sampleHandler.DeleteSample)
		}

		// Project management (protected)
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
	}

	return r
}