package config

import (
	"fmt"
	"strings"
)

// ValidateStartup checks the configuration for common mistakes at server startup.
func ValidateStartup(cfg *Config) error {
	// In release mode, enforce stronger JWT secret
	if cfg.Server.Mode == "release" {
		if err := validateJWTSecret(cfg.JWT.Secret); err != nil {
			return fmt.Errorf("JWT configuration error: %w", err)
		}
	}
	if cfg.ExternalAuth.Enabled && cfg.ExternalAuth.SharedSecret == "" {
		return fmt.Errorf("EXTERNAL_AUTH_SHARED_SECRET must be set when external auth is enabled")
	}
	if cfg.Overlay.Enabled {
		if strings.TrimSpace(cfg.Overlay.BaseURL) == "" {
			return fmt.Errorf("OVERLAY_BASE_URL must be set when overlay is enabled")
		}
		if cfg.Overlay.SharedSecret == "" {
			return fmt.Errorf("OVERLAY_SHARED_SECRET must be set when overlay is enabled")
		}
	}

	return nil
}

// validateJWTSecret checks that the JWT secret is not a default/weak value in release mode.
func validateJWTSecret(secret string) error {
	if secret == "" {
		return fmt.Errorf("JWT_SECRET must not be empty in release mode")
	}
	if len(secret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 characters in release mode")
	}
	// Check for default development secrets
	lower := strings.ToLower(secret)
	if strings.Contains(lower, "change-in-production") ||
		strings.Contains(lower, "secret-key") ||
		strings.Contains(lower, "default") {
		return fmt.Errorf("JWT_SECRET appears to be a default value; please set a strong secret in production")
	}
	return nil
}
