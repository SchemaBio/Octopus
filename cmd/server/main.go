package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SchemaBio/Octopus/internal/assistance"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/middleware"
	"github.com/SchemaBio/Octopus/internal/router"
	"github.com/SchemaBio/Octopus/internal/service"
)

const (
	serverReadHeaderTimeout = 10 * time.Second
	serverIdleTimeout       = 120 * time.Second
	serverShutdownTimeout   = 30 * time.Second
	serverMaxHeaderBytes    = 64 << 10
	serverRequestTimeout    = 60 * time.Second
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "example-template" {
		if err := runExampleTemplateCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "example template command failed:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "research-retention-preflight" {
		if err := runResearchRetentionPreflight(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "retention preflight failed:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "history-backfill" {
		if err := runHistoryBackfill(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "history backfill failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "bam-retention" {
		if err := runBAMRetentionCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "BAM retention command failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "results-parquet" {
		if err := runResultsParquetCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Parquet preparation failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "results-import" {
		if err := runResultsImportCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "result import command failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "results-assessment" {
		if err := runResultsAssessmentCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "assessment evidence preparation failed:", err)
			os.Exit(1)
		}
		return
	}

	cfg := config.Load()

	if err := config.RequireSecureRuntime(cfg.Server.Mode); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	if err := config.ValidateStartup(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Initializing database (%s)...\n", cfg.Database.Driver)
	if err := database.InitDB(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize database: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Running database migrations...")
	if err := database.AutoMigrate(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to auto migrate: %v\n", err)
		os.Exit(1)
	}

	if err := service.BackfillSampleParents(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to migrate sample parents: %v", err)
		os.Exit(1)
	}

	if os.Getenv("CREATE_DEFAULT_ADMIN") == "true" {
		adminEmail := os.Getenv("DEFAULT_ADMIN_EMAIL")
		if adminEmail == "" {
			adminEmail = "admin@octopus.local"
		}
		adminPassword := os.Getenv("DEFAULT_ADMIN_PASSWORD")
		if adminPassword == "" {
			fmt.Fprintln(os.Stderr, "FATAL: DEFAULT_ADMIN_PASSWORD must be set when CREATE_DEFAULT_ADMIN=true")
			os.Exit(1)
		}
		if err := service.ValidateStrongAdminPassword(adminPassword); err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: DEFAULT_ADMIN_PASSWORD must be strong when CREATE_DEFAULT_ADMIN=true: %v\n", err)
			os.Exit(1)
		}

		userSvc := service.NewUserService(cfg)
		adminUser, err := userSvc.CreateDefaultAdmin(adminEmail, adminPassword, "Administrator")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to create default admin: %v\n", err)
		} else {
			fmt.Printf("Default admin user ready: %s (ID: %d)\n", adminUser.Email, adminUser.ID)
		}
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopSepiLens := assistance.StartProcess(ctx)
	aiService := assistance.New(database.GetDB(), cfg.ExternalAuth.Enabled, cfg.Overlay.BaseURL, cfg.Overlay.SharedSecret)
	waitAI := aiService.Start(ctx)

	if cfg.IsSaaS() {
		if err := service.MigrateDownloadProtection(cfg); err != nil {
			log.Fatalf("download protection migration: %v", err)
		}
		if err := service.MigrateExampleData(cfg); err != nil {
			log.Fatalf("example data migration: %v", err)
		}
		if err := database.EnableSaaSRetention(); err != nil {
			log.Fatalf("retention initialization: %v", err)
		}
	}
	// Start Sepiida status sync for running tasks (every 30s)
	taskSvc := service.NewTaskService(cfg)
	retentionSvc := service.NewRetentionService(cfg, taskSvc)
	retentionSvc.AI = aiService
	retentionSvc.Start(ctx)
	service.NewBAMRetentionService(cfg).Start(ctx)
	service.NewExampleDataService(cfg).Start(ctx)
	service.StartDownloadUsageSync(ctx, cfg)
	taskSvc.StartSepiidaSync(ctx, 30*time.Second)
	taskSvc.StartExecutionDelivery(ctx)
	fmt.Println("Sepiida status sync started (interval: 30s)")

	taskSvc.StartDataWaitSync(ctx, 30*time.Second)
	fmt.Println("Data wait sync started (interval: 30s)")

	assetSvc := service.NewDataAssetService(cfg)
	assetSvc.StartRetentionCleanup(ctx, time.Hour)
	if cfg.Storage.RetentionDays > 0 {
		fmt.Printf("Data retention cleanup started (retention: %d days)\n", cfg.Storage.RetentionDays)
	} else {
		fmt.Println("Data retention cleanup disabled (data retained indefinitely)")
	}
	matcher := service.NewSampleMatcher(cfg)
	matcher.Start(ctx, time.Minute)
	fmt.Println("Sample data matcher started (interval: 1m)")
	scanner := service.NewDataScanner(cfg)
	scanner.Start(ctx)
	if scanner.Enabled() {
		fmt.Printf("Data scanner started (interval: %s)\n", cfg.Storage.ScanInterval)
	}

	r := router.New(cfg)
	server := newHTTPServer(":"+cfg.Server.Port, r)
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to listen on %s: %v\n", server.Addr, err)
		cancel()
		waitAI()
		stopSepiLens()
		database.CloseDB()
		os.Exit(1)
	}
	fmt.Printf("Starting schema-platform server on port %s...\n", cfg.Server.Port)
	serveErr := serveHTTPServer(ctx, server, listener)
	cancel()
	waitAI()
	stopSepiLens()
	database.CloseDB()
	if serveErr != nil {
		fmt.Fprintf(os.Stderr, "Server stopped with an error: %v\n", serveErr)
		os.Exit(1)
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           middleware.TimeoutExceptLongRunning(handler, serverRequestTimeout),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
}

func serveHTTPServer(ctx context.Context, server *http.Server, listener net.Listener) error {
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()

	select {
	case err := <-errCh:
		return normalizeServerError(err)
	case <-ctx.Done():
		fmt.Println("\nShutting down...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		closeErr := server.Close()
		serveErr := normalizeServerError(<-errCh)
		return errors.Join(fmt.Errorf("graceful shutdown: %w", err), closeErr, serveErr)
	}
	return normalizeServerError(<-errCh)
}

func normalizeServerError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
