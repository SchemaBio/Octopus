package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/database"
	"github.com/bioinfo/schema-platform/internal/router"
	"github.com/bioinfo/schema-platform/internal/service"
)

func main() {
	cfg := config.Load()

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

	if cfg.Server.Mode != "release" || os.Getenv("CREATE_DEFAULT_ADMIN") == "true" {
		adminEmail := os.Getenv("DEFAULT_ADMIN_EMAIL")
		if adminEmail == "" {
			adminEmail = "admin@octopus.local"
		}
		adminPassword := os.Getenv("DEFAULT_ADMIN_PASSWORD")
		if adminPassword == "" {
			adminPassword = "admin123"
		}
		if cfg.Server.Mode == "release" {
			if err := service.ValidateStrongAdminPassword(adminPassword); err != nil {
				fmt.Fprintf(os.Stderr, "FATAL: DEFAULT_ADMIN_PASSWORD must be strong when CREATE_DEFAULT_ADMIN=true in release mode: %v\n", err)
				os.Exit(1)
			}
		}

		userSvc := service.NewUserService(cfg)
		adminUser, err := userSvc.CreateDefaultAdmin(adminEmail, adminPassword, "Administrator")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to create default admin: %v\n", err)
		} else {
			fmt.Printf("Default admin user ready: %s (ID: %d)\n", adminUser.Email, adminUser.ID)
		}
	}

	// Start Sepiida status sync for running tasks (every 30s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	taskSvc := service.NewTaskService(cfg)
	taskSvc.StartSepiidaSync(ctx, 30*time.Second)
	fmt.Println("Sepiida status sync started (interval: 30s)")

	taskSvc.StartDataWaitSync(ctx, 30*time.Second)
	fmt.Println("Data wait sync started (interval: 30s)")

	fmt.Printf("Starting schema-platform server on port %s...\n", cfg.Server.Port)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
		database.CloseDB()
		os.Exit(0)
	}()

	r := router.New(cfg)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		database.CloseDB()
		os.Exit(1)
	}
}
