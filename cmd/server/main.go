package main

import (
	"fmt"
	"os"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/database"
	"github.com/bioinfo/schema-platform/internal/router"
	"github.com/bioinfo/schema-platform/internal/service"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize database
	fmt.Printf("Initializing database (%s)...\n", cfg.Database.Driver)
	if err := database.InitDB(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize database: %v\n", err)
		os.Exit(1)
	}

	// Auto migrate tables
	fmt.Println("Running database migrations...")
	if err := database.AutoMigrate(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to auto migrate: %v\n", err)
		os.Exit(1)
	}

	// Create default admin user
	userSvc := service.NewUserService(cfg)
	adminUser, err := userSvc.CreateDefaultAdmin("admin@schema.bio", "admin123", "Administrator")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to create default admin: %v\n", err)
	} else {
		fmt.Printf("Default admin user ready: %s (ID: %d)\n", adminUser.Email, adminUser.ID)
	}

	fmt.Printf("Starting schema-platform server on port %s...\n", cfg.Server.Port)

	// Initialize router and start server
	r := router.New(cfg)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		database.CloseDB()
		os.Exit(1)
	}
}
