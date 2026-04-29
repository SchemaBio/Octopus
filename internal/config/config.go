package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Task     TaskConfig
	Sepiida  SepiidaConfig
	Parquet  ParquetConfig
	JWT      JWTConfig
	LLM      LLMConfig
}

type ServerConfig struct {
	Port string
	Mode string
}

type DatabaseConfig struct {
	Driver string
	DSN    string
}

type TaskConfig struct {
	OutputDir       string // default output directory (UUID directories parent)
	TemplateDir     string // WDL templates directory
	ArchiveDir      string // archive directory for completed results
	ArchiveCleanup  bool   // delete output directory after archiving
	MaxConcurrent   int    // max concurrent tasks

	// Executor configurations
	DefaultExecutor  string // default executor: local, slurm, lsf
	MiniWDLPath      string // miniwdl executable (local mode)
	MiniWDLSlurmPath string // miniwdl-slurm executable (slurm mode)
	MiniWDLLSFPath   string // miniwdl-lsf executable (lsf mode)
}

type SepiidaConfig struct {
	ServerURL string // Sepiida server URL
	QueryKey  string // Query API key
	Enabled   bool   // Enable Sepiida integration
}

type ParquetConfig struct {
	Enabled      bool     // Enable parquet generation
	OutputDir    string   // Parquet output directory (default: same as archive)
	FilePatterns []string // File patterns to convert (e.g: "*.csv", "*.tsv", "*.txt")
}

type JWTConfig struct {
	Secret          string        // JWT signing secret
	Issuer          string        // JWT issuer
	ExpireDuration  time.Duration // Access token expiry
	RefreshDuration time.Duration // Refresh token expiry
}

type LLMConfig struct {
	BaseURL string // OpenAI-compatible API base URL (e.g. https://api.openai.com/v1)
	APIKey  string // API key
	Model   string // Model name (e.g. gpt-4o)
	Enabled bool   // Enable AI evaluation
}

// Load loads configuration from environment and files
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
			Mode: getEnv("GIN_MODE", "debug"),
		},
		Database: DatabaseConfig{
			Driver: getEnv("DB_DRIVER", "sqlite"),
			DSN:    getEnv("DB_DSN", "./data/schema-platform.db"),
		},
		Task: TaskConfig{
			OutputDir:       getEnv("OUTPUT_DIR", "/mnt/data/output"),
			TemplateDir:     getEnv("TEMPLATE_DIR", "/home/ubuntu/schema-germline"),
			ArchiveDir:      getEnv("ARCHIVE_DIR", "/mnt/data/archive"),
			ArchiveCleanup:  getEnv("ARCHIVE_CLEANUP", "false") == "true",
			MaxConcurrent:   10,

			DefaultExecutor:  getEnv("DEFAULT_EXECUTOR", "local"),
			MiniWDLPath:      getEnv("MINIWDL_PATH", "miniwdl"),
			MiniWDLSlurmPath: getEnv("MINIWDL_SLURM_PATH", "miniwdl-slurm"),
			MiniWDLLSFPath:   getEnv("MINIWDL_LSF_PATH", "miniwdl-lsf"),
		},
		Sepiida: SepiidaConfig{
			ServerURL: getEnv("SEPIIDA_URL", "http://localhost:9090"),
			QueryKey:  getEnv("SEPIIDA_QUERY_KEY", ""),
			Enabled:   getEnv("SEPIIDA_ENABLED", "true") == "true",
		},
		Parquet: ParquetConfig{
			Enabled:      getEnv("PARQUET_ENABLED", "true") == "true",
			OutputDir:    getEnv("PARQUET_DIR", ""), // empty means same as archive
			FilePatterns: []string{"*.csv", "*.tsv", "*.txt"}, // default patterns
		},
		JWT: JWTConfig{
			Secret:          getEnv("JWT_SECRET", "octopus-secret-key-change-in-production"),
			Issuer:          getEnv("JWT_ISSUER", "octopus"),
			ExpireDuration:  parseDuration(getEnv("JWT_EXPIRE", "24h")),
			RefreshDuration: parseDuration(getEnv("JWT_REFRESH", "168h")),
		},
		LLM: LLMConfig{
			BaseURL: getEnv("LLM_BASE_URL", ""),
			APIKey:  getEnv("LLM_API_KEY", ""),
			Model:   getEnv("LLM_MODEL", "gpt-4o"),
			Enabled: getEnv("LLM_ENABLED", "false") == "true",
		},
	}
}

// getEnv gets environment variable with default value (internal use)
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseDuration parses duration string (supports h, m, s, and numeric as hours)
func parseDuration(s string) time.Duration {
	// Try standard parsing first
	d, err := time.ParseDuration(s)
	if err == nil {
		return d
	}

	// Try parsing as numeric (hours)
	hours, err := strconv.Atoi(s)
	if err == nil {
		return time.Duration(hours) * time.Hour
	}

	// Default fallback: 24 hours
	return 24 * time.Hour
}