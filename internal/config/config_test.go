package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSecretsFromDefaultFiles(t *testing.T) {
	// Create temp directory for secret files
	dir := t.TempDir()

	// Write secret files
	jwtSecretFile := filepath.Join(dir, "jwt_secret")
	if err := os.WriteFile(jwtSecretFile, []byte("file-jwt-secret-value-from-docker-secrets"), 0644); err != nil {
		t.Fatal(err)
	}

	queryKeyFile := filepath.Join(dir, "query_key")
	if err := os.WriteFile(queryKeyFile, []byte("file-query-key-value\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set env vars pointing to files
	t.Setenv("JWT_SECRET_FILE", jwtSecretFile)
	t.Setenv("SEPIIDA_QUERY_KEY_FILE", queryKeyFile)
	// Clear the direct env vars so fallback to file
	t.Setenv("JWT_SECRET", "")
	t.Setenv("SEPIIDA_QUERY_KEY", "")

	jwtVal := getEnvOrFile("JWT_SECRET", "default-jwt")
	if jwtVal != "file-jwt-secret-value-from-docker-secrets" {
		t.Errorf("expected secret from file, got %q", jwtVal)
	}

	queryVal := getEnvOrFile("SEPIIDA_QUERY_KEY", "")
	if queryVal != "file-query-key-value" {
		t.Errorf("expected trimmed query key from file, got %q", queryVal)
	}
}

func TestLoadSecretsFromDefaultFilesWithComments(t *testing.T) {
	dir := t.TempDir()

	secretFile := filepath.Join(dir, "secret_with_comments")
	content := "# This is a comment\nactual-secret-value\n# Another comment\n"
	if err := os.WriteFile(secretFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TEST_SECRET_FILE", secretFile)
	t.Setenv("TEST_SECRET", "")

	val := getEnvOrFile("TEST_SECRET", "default")
	if val != "actual-secret-value" {
		t.Errorf("expected secret without comments, got %q", val)
	}
}

func TestSplitComma(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b , c ", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}},
		{",,,", nil},
		{"single", []string{"single"}},
	}
	for _, tt := range tests {
		result := splitComma(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("splitComma(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("splitComma(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}

func TestGetEnvFloat(t *testing.T) {
	t.Setenv("TEST_FLOAT", "3.14")
	val := getEnvFloat("TEST_FLOAT", 1.0)
	if val != 3.14 {
		t.Errorf("expected 3.14, got %f", val)
	}

	// Test default
	val = getEnvFloat("NONEXISTENT_FLOAT", 2.5)
	if val != 2.5 {
		t.Errorf("expected 2.5, got %f", val)
	}

	// Test invalid
	t.Setenv("INVALID_FLOAT", "notanumber")
	val = getEnvFloat("INVALID_FLOAT", 3.0)
	if val != 3.0 {
		t.Errorf("expected 3.0, got %f", val)
	}
}

func TestLoadLLMProxyDefaults(t *testing.T) {
	// Test default AllowedModels
	t.Setenv("LLM_MODEL", "gpt-4o")
	t.Setenv("LLM_ALLOWED_MODELS", "")
	models := loadAllowedModels("gpt-4o")
	if len(models) != 1 || models[0] != "gpt-4o" {
		t.Errorf("expected [gpt-4o], got %v", models)
	}

	// Test custom AllowedModels
	t.Setenv("LLM_ALLOWED_MODELS", "gpt-4o,gpt-3.5-turbo,claude-3")
	models = loadAllowedModels("gpt-4o")
	if len(models) != 3 {
		t.Errorf("expected 3 models, got %d: %v", len(models), models)
	}

	// Test wildcard
	t.Setenv("LLM_ALLOWED_MODELS", "*")
	models = loadAllowedModels("gpt-4o")
	if len(models) != 1 || models[0] != "*" {
		t.Errorf("expected [*], got %v", models)
	}
}

func TestValidateStartupRejectsWeakReleaseJWTSecret(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Mode: "release"},
		JWT:    JWTConfig{Secret: "short"},
	}
	err := ValidateStartup(cfg)
	if err == nil {
		t.Error("expected error for weak JWT secret in release mode")
	}

	cfg.JWT.Secret = "octopus-secret-key-change-in-production"
	err = ValidateStartup(cfg)
	if err == nil {
		t.Error("expected error for default JWT secret in release mode")
	}

	cfg.JWT.Secret = "xK9mP2vN7bQ4wR6tY8uI1oA3sD5fG0hJ"
	cfg.Server.AllowedOrigins = "https://app.example.com"
	cfg.JWT.CookieSecure = true
	err = ValidateStartup(cfg)
	if err != nil {
		t.Errorf("unexpected error for strong secret: %v", err)
	}
}

func TestValidateStartupAllowsDebugMode(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Mode: "debug"},
		JWT:    JWTConfig{Secret: "short"},
	}
	err := ValidateStartup(cfg)
	if err != nil {
		t.Errorf("expected no error in debug mode, got: %v", err)
	}
}

func TestValidateStartupRejectsReleaseLLMHTTPURL(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Mode: "release", AllowedOrigins: "https://app.example.com"},
		JWT:    JWTConfig{Secret: "xK9mP2vN7bQ4wR6tY8uI1oA3sD5fG0hJ", CookieSecure: true},
		LLM: LLMConfig{
			Enabled: true,
			BaseURL: "http://llm.example.com/v1",
			APIKey:  "secret",
		},
	}

	if err := ValidateStartup(cfg); err == nil {
		t.Fatal("expected release LLM HTTP URL to fail")
	}
}

func TestValidateStartupRejectsReleaseSepiidaWithoutStrongKey(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Mode: "release", AllowedOrigins: "https://app.example.com"},
		JWT:    JWTConfig{Secret: "xK9mP2vN7bQ4wR6tY8uI1oA3sD5fG0hJ", CookieSecure: true},
		Sepiida: SepiidaConfig{
			Enabled:   true,
			ServerURL: "http://sepiida.internal:9090",
		},
	}

	if err := ValidateStartup(cfg); err == nil {
		t.Fatal("expected release Sepiida without query key to fail")
	}

	cfg.Sepiida.QueryKey = "short"
	if err := ValidateStartup(cfg); err == nil {
		t.Fatal("expected weak release Sepiida query key to fail")
	}

	cfg.Sepiida.QueryKey = "Q9x4N2v8P6s1T7w3R5y0U2i9O4p6A8d1"
	if err := ValidateStartup(cfg); err != nil {
		t.Fatalf("unexpected error for strong Sepiida query key: %v", err)
	}
}

func TestValidateStartupRejectsSepiidaURLUserInfo(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Mode: "debug"},
		Sepiida: SepiidaConfig{
			Enabled:   true,
			ServerURL: "http://user:pass@sepiida.internal:9090",
			QueryKey:  "debug-query-key",
		},
	}

	if err := ValidateStartup(cfg); err == nil {
		t.Fatal("expected Sepiida URL with userinfo to fail")
	}
}

func TestValidateStartupRejectsUnsafeReleaseCORS(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Mode: "release", AllowedOrigins: "*"},
		JWT:    JWTConfig{Secret: "xK9mP2vN7bQ4wR6tY8uI1oA3sD5fG0hJ", CookieSecure: true},
	}
	if err := ValidateStartup(cfg); err == nil {
		t.Fatal("expected wildcard release CORS origin to fail")
	}

	cfg.Server.AllowedOrigins = "http://app.example.com"
	if err := ValidateStartup(cfg); err == nil {
		t.Fatal("expected non-HTTPS release CORS origin to fail")
	}
}

func TestLoadAllowedModels(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		model    string
		expected int
	}{
		{"default", "", "gpt-4o", 1},
		{"custom", "gpt-4o,gpt-3.5", "gpt-4o", 2},
		{"wildcard", "*", "gpt-4o", 1},
		{"with_spaces", " gpt-4o , gpt-3.5 ", "gpt-4o", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LLM_ALLOWED_MODELS", tt.envValue)
			result := loadAllowedModels(tt.model)
			if len(result) != tt.expected {
				t.Errorf("expected %d models, got %d: %v", tt.expected, len(result), result)
			}
		})
	}
}
