package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server       ServerConfig
	Database     DatabaseConfig
	Task         TaskConfig
	Sepiida      SepiidaConfig
	Parquet      ParquetConfig
	ResultQuery  ResultQueryConfig
	JWT          JWTConfig
	ExternalAuth ExternalAuthConfig
	Overlay      OverlayConfig
	LLM          LLMConfig
	Storage      StorageConfig
	Report       ReportConfig
	IGV          IGVConfig
}

type ServerConfig struct {
	Port           string
	Mode           string
	AllowedOrigins string   // comma-separated CORS allowed origins
	TrustedProxies []string // explicit IP/CIDR list allowed to supply forwarded client IPs
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
	ServerURL          string        // Sepiida server URL
	QueryKey           string        // Query API key
	Enabled            bool          // Enable Sepiida integration
	FirstReportTimeout time.Duration // Maximum wait for the first workflow report
	QueryGracePeriod   time.Duration // Additional grace when Sepiida reads are unavailable
}

type ParquetConfig struct {
	Enabled      bool     // Enable parquet generation
	OutputDir    string   // Parquet output directory (default: same as archive)
	FilePatterns []string // File patterns to convert (e.g: "*.csv", "*.tsv", "*.txt")
}

type ResultQueryConfig struct {
	TempDir       string
	ReferenceDir  string
	CacheDir      string
	AssessmentDir string
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

type ExternalAuthConfig struct {
	Enabled        bool
	SharedSecret   string
	CallbackSecret string
	HeaderName     string
	UserIDHeader   string
	EmailHeader    string
	RoleHeader     string
	OrgIDHeader    string
}

type OverlayConfig struct {
	Enabled           bool
	BaseURL           string
	SharedSecret      string
	Timeout           time.Duration
	DispatchTimeout   time.Duration
	FailOpen          bool
	TaskAdmissionPath string
	TaskEventPath     string
	TaskDispatchPath  string
	TaskCancelPath    string
}

type LLMConfig struct {
	BaseURL           string   // OpenAI-compatible API base URL (e.g. https://api.openai.com/v1)
	APIKey            string   // API key
	Model             string   // Model name (e.g. gpt-4o)
	Enabled           bool     // Enable AI evaluation
	AllowedModels     []string // AI proxy allowed model list, comma-separated, "*" means no restriction
	ProxyMaxBodyBytes int64    // AI proxy max request body size in bytes
}

type StorageConfig struct {
	ResultDownloadLinkTTL         time.Duration
	ResultDownloadRefreshInterval time.Duration
	ResultDownloadMaxIssues       int
	ResultDownloadTrafficLimit    int64  // bits per second, enforced by COS per request
	ResultDownloadPauseFile       string // existing file pauses new download signatures
	BAMRetentionDays              int    // 0 disables the policy; SaaS uses exactly 7 days
	BAMCleanupEnabled             bool   // enable physical deletion after reviewing a dry run
	Provider                      string // local or s3
	LocalDir                      string // local upload root directory
	MaxSizeMB                     int    // maximum upload file size in MB; default 20 GiB, 0 means unlimited
	RetentionDays                 int    // 0 keeps data indefinitely; SaaS deployments use 7
	PresignExpiry                 time.Duration
	// CVM inputs may wait for spot capacity; keep their download URL valid for
	// the retry window plus instance bootstrap time.
	CVMInputPresignExpiry time.Duration
	S3Endpoint            string
	S3PublicEndpoint      string
	S3Region              string
	S3Bucket              string
	CVMReferenceBucket    string
	CVMReferenceAccessKey string
	CVMReferenceSecretKey string
	S3AccessKey           string
	S3SecretKey           string
	S3SessionToken        string
	S3UsePathStyle        bool
	ScanLocalDir          string
	S3ScanPrefix          string
	ScanOrgID             string
	ScanUserID            int
	ScanInterval          time.Duration
}

type ReportConfig struct {
	PackageMaxSizeMB int
	RequestTimeout   time.Duration
}

// IGVConfig describes self-hosted immutable reference assets. Task archives
// remain private; only the result service can mint short-lived COS URLs for
// the task-specific tracks.
type IGVConfig struct {
	ReferenceProxyBaseURL string
	HG19                  IGVReferenceConfig
	HG38                  IGVReferenceConfig
	TrackURLExpiry        time.Duration
}

type IGVReferenceConfig struct {
	FASTAURL          string
	FAIURL            string
	AliasURL          string
	CytobandURL       string
	GeneTrackURL      string
	GeneTrackIndexURL string
}

// Load loads configuration from environment and files
func Load() *Config {
	storageProvider := strings.ToLower(strings.TrimSpace(getEnv("STORAGE_PROVIDER", "local")))
	cosRegion := firstNonEmptyEnv("COS_REGION", "TENCENT_REGION")
	cosBucket := strings.TrimSpace(getEnv("COS_BUCKET", ""))
	s3Endpoint := strings.TrimSpace(getEnv("S3_ENDPOINT", ""))
	s3PublicEndpoint := strings.TrimSpace(getEnv("S3_PUBLIC_ENDPOINT", ""))
	s3Region := getEnv("S3_REGION", "us-east-1")
	s3Bucket := strings.TrimSpace(getEnv("S3_BUCKET", ""))
	s3AccessKey, s3SecretKey := firstCredentialPair(
		[2]string{"S3_ACCESS_KEY", "S3_SECRET_KEY"},
	)
	parquetCacheDir := strings.TrimSpace(getEnv("PARQUET_CACHE_DIR", "/data/parquet-cache"))
	if storageProvider == "cos" {
		if cosRegion == "" {
			cosRegion = "ap-guangzhou"
		}
		serviceEndpoint := "https://cos." + cosRegion + ".myqcloud.com"
		if s3Endpoint == "" {
			s3Endpoint = serviceEndpoint
		}
		if s3PublicEndpoint == "" {
			s3PublicEndpoint = serviceEndpoint
		}
		s3Region = cosRegion
		if s3Bucket == "" {
			s3Bucket = cosBucket
		}
		if s3AccessKey == "" && s3SecretKey == "" {
			s3AccessKey, s3SecretKey = firstCredentialPair(
				[2]string{"COS_SECRET_ID", "COS_SECRET_KEY"},
				[2]string{"TENCENT_SECRET_ID", "TENCENT_SECRET_KEY"},
			)
		}
	}
	return &Config{
		Server: ServerConfig{
			Port:           getEnv("SERVER_PORT", "8080"),
			Mode:           getEnv("GIN_MODE", "debug"),
			AllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:3001,http://localhost:3002"),
			TrustedProxies: splitComma(getEnv("TRUSTED_PROXIES", "")),
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
			ServerURL:          getEnv("SEPIIDA_URL", "http://localhost:9090"),
			QueryKey:           getEnvOrFile("SEPIIDA_QUERY_KEY", ""),
			Enabled:            getEnv("SEPIIDA_ENABLED", "true") == "true",
			FirstReportTimeout: parseDuration(getEnv("SEPIIDA_FIRST_REPORT_TIMEOUT", "10m")),
			QueryGracePeriod:   parseDuration(getEnv("SEPIIDA_QUERY_GRACE_PERIOD", "30m")),
		},
		Parquet: ParquetConfig{
			Enabled:      getEnv("PARQUET_ENABLED", "true") == "true",
			OutputDir:    getEnv("PARQUET_DIR", ""),           // empty means same as archive
			FilePatterns: []string{"*.csv", "*.tsv", "*.txt"}, // default patterns
		},
		ResultQuery: ResultQueryConfig{
			TempDir:       strings.TrimSpace(getEnv("RESULT_ENGINE_TEMP_DIR", filepath.Join(parquetCacheDir, "tmp"))),
			ReferenceDir:  strings.TrimSpace(getEnv("ASSESSMENT_REFERENCE_DIR", filepath.Join(parquetCacheDir, "reference"))),
			CacheDir:      parquetCacheDir,
			AssessmentDir: strings.TrimSpace(getEnv("PARQUET_ASSESSMENT_DIR", filepath.Join(parquetCacheDir, "assessments"))),
		},
		JWT: JWTConfig{
			Secret:                    getEnvOrFile("JWT_SECRET", "octopus-secret-key-change-in-production"),
			Issuer:                    getEnv("JWT_ISSUER", "octopus"),
			ExpireDuration:            parseDuration(getEnv("JWT_EXPIRE", "24h")),
			RefreshDuration:           parseDuration(getEnv("JWT_REFRESH", "168h")),
			CookieDomain:              getEnv("JWT_COOKIE_DOMAIN", ""),
			CookieSecure:              getEnv("JWT_COOKIE_SECURE", "false") == "true",
			ClientPasswordHashEnabled: getEnv("CLIENT_PASSWORD_HASH_ENABLED", "false") == "true",
		},
		ExternalAuth: ExternalAuthConfig{
			Enabled:        getEnv("EXTERNAL_AUTH_ENABLED", "false") == "true",
			SharedSecret:   getEnvOrFile("EXTERNAL_AUTH_SHARED_SECRET", ""),
			CallbackSecret: getEnvOrFile("CVM_CALLBACK_SECRET", ""),
			HeaderName:     getEnv("EXTERNAL_AUTH_HEADER", "X-Octopus-External-Auth"),
			UserIDHeader:   getEnv("EXTERNAL_AUTH_USER_ID_HEADER", "X-Octopus-User-ID"),
			EmailHeader:    getEnv("EXTERNAL_AUTH_EMAIL_HEADER", "X-Octopus-User-Email"),
			RoleHeader:     getEnv("EXTERNAL_AUTH_ROLE_HEADER", "X-Octopus-User-Role"),
			OrgIDHeader:    getEnv("EXTERNAL_AUTH_ORG_ID_HEADER", "X-Octopus-Org-ID"),
		},
		Overlay: OverlayConfig{
			Enabled:           getEnv("OVERLAY_ENABLED", "false") == "true",
			BaseURL:           getEnv("OVERLAY_BASE_URL", ""),
			SharedSecret:      getEnvOrFile("OVERLAY_SHARED_SECRET", ""),
			Timeout:           parseDuration(getEnv("OVERLAY_TIMEOUT", "5s")),
			DispatchTimeout:   parseDuration(getEnv("OVERLAY_DISPATCH_TIMEOUT", "30s")),
			FailOpen:          getEnv("OVERLAY_FAIL_OPEN", "false") == "true",
			TaskAdmissionPath: getEnv("OVERLAY_TASK_ADMISSION_PATH", "/api/v1/overlay/tasks/admit"),
			TaskEventPath:     getEnv("OVERLAY_TASK_EVENT_PATH", "/api/v1/overlay/tasks/events"),
			TaskDispatchPath:  getEnv("OVERLAY_TASK_DISPATCH_PATH", "/api/v1/overlay/tasks/dispatch"),
			TaskCancelPath:    getEnv("OVERLAY_TASK_CANCEL_PATH", "/api/v1/overlay/tasks/cancel"),
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
			ResultDownloadLinkTTL:         parseDuration(getEnv("RESULT_DOWNLOAD_LINK_TTL", "30m")),
			ResultDownloadRefreshInterval: parseDuration(getEnv("RESULT_DOWNLOAD_REFRESH_INTERVAL", "60s")),
			ResultDownloadMaxIssues:       parseIntEnv("RESULT_DOWNLOAD_MAX_ISSUES", 12),
			ResultDownloadTrafficLimit:    int64(parseIntEnv("RESULT_DOWNLOAD_TRAFFIC_LIMIT_BPS", 83886080)),
			ResultDownloadPauseFile:       getEnv("RESULT_DOWNLOAD_PAUSE_FILE", "/data/archive/.downloads-paused"),
			Provider:                      normalizeStorageProvider(storageProvider),
			LocalDir:                      getEnv("STORAGE_LOCAL_DIR", "/mnt/data/uploads"),
			MaxSizeMB:                     parseIntEnv("UPLOAD_MAX_SIZE_MB", 20480),
			RetentionDays:                 parseIntEnv("DATA_RETENTION_DAYS", 0),
			BAMRetentionDays:              parseIntEnv("BAM_RETENTION_DAYS", 0),
			BAMCleanupEnabled:             getEnv("BAM_CLEANUP_ENABLED", "false") == "true",
			PresignExpiry:                 parseDuration(getEnv("STORAGE_PRESIGN_EXPIRE", "15m")),
			CVMInputPresignExpiry:         parseDuration(getEnv("CVM_INPUT_PRESIGN_EXPIRE", "1h")),
			S3Endpoint:                    s3Endpoint,
			S3PublicEndpoint:              s3PublicEndpoint,
			S3Region:                      s3Region,
			S3Bucket:                      s3Bucket,
			CVMReferenceBucket:            strings.TrimSpace(getEnv("CVM_REFERENCE_BUCKET", "schemabio-1327430028")),
			CVMReferenceAccessKey:         strings.TrimSpace(getEnvOrFile("CVM_REFERENCE_SECRET_ID", "")),
			CVMReferenceSecretKey:         strings.TrimSpace(getEnvOrFile("CVM_REFERENCE_SECRET_KEY", "")),
			S3AccessKey:                   s3AccessKey,
			S3SecretKey:                   s3SecretKey,
			S3SessionToken:                getEnvOrFile("S3_SESSION_TOKEN", ""),
			S3UsePathStyle:                getEnv("S3_USE_PATH_STYLE", "false") == "true",
			ScanLocalDir:                  strings.TrimSpace(getEnv("DATA_SCAN_LOCAL_DIR", "")),
			S3ScanPrefix:                  strings.Trim(strings.TrimSpace(getEnv("S3_SCAN_PREFIX", "")), "/"),
			ScanOrgID:                     strings.TrimSpace(getEnv("DATA_SCAN_ORG_ID", "")),
			ScanUserID:                    parseIntEnv("DATA_SCAN_USER_ID", 1),
			ScanInterval:                  parseDuration(getEnv("DATA_SCAN_INTERVAL", "1m")),
		},
		Report: ReportConfig{
			PackageMaxSizeMB: parseIntEnv("REPORT_PACKAGE_MAX_SIZE_MB", 20*1024),
			RequestTimeout:   parseDuration(getEnv("REPORT_REQUEST_TIMEOUT", "5m")),
		},
		IGV: IGVConfig{
			ReferenceProxyBaseURL: strings.TrimRight(strings.TrimSpace(getEnv("IGV_REFERENCE_PROXY_BASE_URL", "")), "/"),
			HG19: IGVReferenceConfig{
				FASTAURL:          strings.TrimSpace(getEnv("IGV_HG19_FASTA_URL", "")),
				FAIURL:            strings.TrimSpace(getEnv("IGV_HG19_FAI_URL", "")),
				AliasURL:          strings.TrimSpace(getEnv("IGV_HG19_ALIAS_URL", "")),
				CytobandURL:       strings.TrimSpace(getEnv("IGV_HG19_CYTOBAND_URL", "")),
				GeneTrackURL:      strings.TrimSpace(getEnv("IGV_HG19_GENE_TRACK_URL", "")),
				GeneTrackIndexURL: strings.TrimSpace(getEnv("IGV_HG19_GENE_TRACK_INDEX_URL", "")),
			},
			HG38: IGVReferenceConfig{
				FASTAURL:          strings.TrimSpace(getEnv("IGV_HG38_FASTA_URL", "")),
				FAIURL:            strings.TrimSpace(getEnv("IGV_HG38_FAI_URL", "")),
				AliasURL:          strings.TrimSpace(getEnv("IGV_HG38_ALIAS_URL", "")),
				CytobandURL:       strings.TrimSpace(getEnv("IGV_HG38_CYTOBAND_URL", "")),
				GeneTrackURL:      strings.TrimSpace(getEnv("IGV_HG38_GENE_TRACK_URL", "")),
				GeneTrackIndexURL: strings.TrimSpace(getEnv("IGV_HG38_GENE_TRACK_INDEX_URL", "")),
			},
			TrackURLExpiry: parseDuration(getEnv("IGV_TRACK_URL_EXPIRE", "10m")),
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
	if provider == "cos" {
		return "s3"
	}
	if provider != "local" && provider != "s3" {
		return "local"
	}
	return provider
}

func firstNonEmptyEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func firstCredentialPair(pairs ...[2]string) (string, string) {
	for _, pair := range pairs {
		id := strings.TrimSpace(getEnvOrFile(pair[0], ""))
		key := strings.TrimSpace(getEnvOrFile(pair[1], ""))
		if id != "" || key != "" {
			return id, key
		}
	}
	return "", ""
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
