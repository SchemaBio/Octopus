package config

import (
	"os"
	"strconv"
	"strings"
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
	Storage  StorageConfig
}

type ServerConfig struct {
	Port           string
	Mode           string
	AllowedOrigins string // comma-separated CORS allowed origins
}

type DatabaseConfig struct {
	Driver string
	DSN    string
}

type TaskConfig struct {
	OutputDir      string // default output directory (UUID directories parent)
	TemplateDir    string // WDL templates directory
	ArchiveDir     string // archive directory for completed results
	ArchiveCleanup bool   // delete output directory after archiving
	MaxConcurrent  int    // max concurrent tasks

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
	Secret                    string        // JWT signing secret
	Issuer                    string        // JWT issuer
	ExpireDuration            time.Duration // Access token expiry
	RefreshDuration           time.Duration // Refresh token expiry
	CookieDomain              string        // Domain for Set-Cookie (empty = current domain)
	CookieSecure              bool          // Secure flag for cookies (requires HTTPS)
	ClientPasswordHashEnabled bool          // Enable SHA-256 client-side password hash compatibility
}

type LLMConfig struct {
	BaseURL         string   // OpenAI-compatible API base URL (e.g. https://api.openai.com/v1)
	APIKey          string   // API key
	Model           string   // Model name (e.g. gpt-4o)
	Enabled         bool     // Enable AI evaluation
	AllowedModels   []string // AI proxy allowed model list, comma-separated, "*" means no restriction
	ProxyMaxBodyBytes int64  // AI proxy max request body size in bytes
}

type StorageConfig struct {
	Provider  string // local only for the open-source Octopus backend
	LocalDir  string // local upload root directory
	MaxSizeMB int    // maximum upload file size in MB, 0 means unlimited
}

// Load loads configuration from environment and files
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port:           getEnv("SERVER_PORT", "8080"),
			Mode:           getEnv("GIN_MODE", "debug"),
			AllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:3001,http://localhost:3002"),
		},
		Database: DatabaseConfig{
			Driver: getEnv("DB_DRIVER", "postgres"),
			DSN:    getEnv("DB_DSN", "host=localhost user=octopus password=octopus dbname=octopus port=5432 sslmode=disable TimeZone=Asia/Shanghai"),
		},
		Task: TaskConfig{
			OutputDir:      getEnv("OUTPUT_DIR", "/mnt/data/output"),
			TemplateDir:    getEnv("TEMPLATE_DIR", "/home/ubuntu/schema-germline"),
			ArchiveDir:     getEnv("ARCHIVE_DIR", "/mnt/data/archive"),
			ArchiveCleanup: getEnv("ARCHIVE_CLEANUP", "false") == "true",
			MaxConcurrent:  10,

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
			OutputDir:    getEnv("PARQUET_DIR", ""),           // empty means same as archive
			FilePatterns: []string{"*.csv", "*.tsv", "*.txt"}, // default patterns
		},
		JWT: JWTConfig{
			Secret:                    getEnv("JWT_SECRET", "octopus-secret-key-change-in-production"),
			Issuer:                    getEnv("JWT_ISSUER", "octopus"),
			ExpireDuration:            parseDuration(getEnv("JWT_EXPIRE", "24h")),
			RefreshDuration:           parseDuration(getEnv("JWT_REFRESH", "168h")),
			CookieDomain:              getEnv("JWT_COOKIE_DOMAIN", ""),
			CookieSecure:              getEnv("JWT_COOKIE_SECURE", "false") == "true",
			ClientPasswordHashEnabled: getEnv("CLIENT_PASSWORD_HASH_ENABLED", "false") == "true",
		},
		LLM: LLMConfig{
			BaseURL:           getEnv("LLM_BASE_URL", ""),
			APIKey:            getEnv("LLM_API_KEY", ""),
			Model:             getEnv("LLM_MODEL", "gpt-4o"),
			Enabled:           getEnv("LLM_ENABLED", "false") == "true",
			AllowedModels:     loadAllowedModels(getEnv("LLM_MODEL", "gpt-4o")),
			ProxyMaxBodyBytes: int64(parseIntEnv("LLM_PROXY_MAX_BODY_MB", 2)) << 20,
		},
		Storage: StorageConfig{
			Provider:  normalizeStorageProvider(getEnv("STORAGE_PROVIDER", "local")),
			LocalDir:  getEnv("STORAGE_LOCAL_DIR", "/mnt/data/uploads"),
			MaxSizeMB: parseIntEnv("UPLOAD_MAX_SIZE_MB", 0),
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

func parseIntEnv(key string, defaultValue int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultValue
	}
	return n
}

func normalizeStorageProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "local"
	}
	if provider != "local" {
		return "local"
	}
	return provider
}

// splitComma splits a comma-separated string, trimming whitespace and filtering empty values.
func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// loadAllowedModels returns the allowed models list for AI proxy.
// Defaults to []string{model} (only the configured model).
func loadAllowedModels(model string) []string {
	env := os.Getenv("LLM_ALLOWED_MODELS")
	if env == "" {
		return []string{model}
	}
	return splitComma(env)
}

// getEnvFloat gets a float64 environment variable with default value.
func getEnvFloat(key string, defaultValue float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return defaultValue
	}
	return f
}

// getEnvOrFile reads a secret from env var first, falling back to a file path.
// This supports Docker-style secrets mounted as files.
func getEnvOrFile(envKey, defaultValue string) string {
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	filePath := os.Getenv(envKey + "_FILE")
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err == nil {
			// Strip comment lines and trim whitespace
			lines := strings.Split(string(data), "\n")
			var result []string
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					result = append(result, line)
				}
			}
			if len(result) > 0 {
				return strings.Join(result, "\n")
			}
		}
	}
	return defaultValue
}
