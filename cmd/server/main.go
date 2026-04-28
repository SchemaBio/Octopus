package main

import (
	"fmt"
	"os"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/router"
)

func main() {
	// Load configuration
	cfg := config.Load()

	fmt.Printf("Starting schema-platform server on port %s...\n", cfg.Server.Port)

	// Initialize router and start server
	r := router.New(cfg)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		os.Exit(1)
	}
}