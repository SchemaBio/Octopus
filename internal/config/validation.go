package config

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateStartup checks the configuration for common mistakes at server startup.
func ValidateStartup(cfg *Config) error {
	// In release mode, enforce stronger JWT secret
	if cfg.Server.Mode == "release" {
		if err := validateJWTSecret(cfg.JWT.Secret); err != nil {
			return fmt.Errorf("JWT configuration error: %w", err)
		}
		if err := validateReleaseCORSOrigins(cfg.Server.AllowedOrigins); err != nil {
			return err
		}
		if !cfg.JWT.CookieSecure {
			return fmt.Errorf("JWT_COOKIE_SECURE must be true in release mode")
		}
	}
	if cfg.ExternalAuth.Enabled && cfg.ExternalAuth.SharedSecret == "" {
		return fmt.Errorf("EXTERNAL_AUTH_SHARED_SECRET must be set when external auth is enabled")
	}
	if cfg.Server.Mode == "release" && cfg.ExternalAuth.Enabled {
		if err := validateSharedSecret("EXTERNAL_AUTH_SHARED_SECRET", cfg.ExternalAuth.SharedSecret); err != nil {
			return err
		}
	}
	if cfg.Overlay.Enabled {
		if strings.TrimSpace(cfg.Overlay.BaseURL) == "" {
			return fmt.Errorf("OVERLAY_BASE_URL must be set when overlay is enabled")
		}
		if cfg.Overlay.SharedSecret == "" {
			return fmt.Errorf("OVERLAY_SHARED_SECRET must be set when overlay is enabled")
		}
		if cfg.Server.Mode == "release" {
			if err := validateSharedSecret("OVERLAY_SHARED_SECRET", cfg.Overlay.SharedSecret); err != nil {
				return err
			}
		}
	}
	if cfg.Sepiida.Enabled {
		if strings.TrimSpace(cfg.Sepiida.QueryKey) != "" {
			if err := validateAbsoluteServiceURL("SEPIIDA_URL", cfg.Sepiida.ServerURL); err != nil {
				return err
			}
		}
		if cfg.Server.Mode == "release" {
			if strings.TrimSpace(cfg.Sepiida.QueryKey) == "" {
				return fmt.Errorf("SEPIIDA_QUERY_KEY must be set when Sepiida is enabled in release mode")
			}
			if err := validateSharedSecret("SEPIIDA_QUERY_KEY", cfg.Sepiida.QueryKey); err != nil {
				return err
			}
		}
	}
	if cfg.LLM.Enabled {
		if strings.TrimSpace(cfg.LLM.BaseURL) == "" {
			return fmt.Errorf("LLM_BASE_URL must be set when LLM is enabled")
		}
		if strings.TrimSpace(cfg.LLM.APIKey) == "" {
			return fmt.Errorf("LLM_API_KEY must be set when LLM is enabled")
		}
		if cfg.Server.Mode == "release" {
			if err := validateReleaseHTTPSURL("LLM_BASE_URL", cfg.LLM.BaseURL); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateAbsoluteServiceURL(name, rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be a valid absolute URL", name)
	}
	if u.User != nil {
		return fmt.Errorf("%s must not include user info", name)
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("%s must use http or https", name)
	}
	return nil
}

func validateSharedSecret(name, secret string) error {
	if len(strings.TrimSpace(secret)) < 32 {
		return fmt.Errorf("%s must be at least 32 characters in release mode", name)
	}
	lower := strings.ToLower(secret)
	if strings.Contains(lower, "change") || strings.Contains(lower, "secret") || strings.Contains(lower, "default") {
		return fmt.Errorf("%s appears to be a default value; please set a strong random secret", name)
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

func validateReleaseHTTPSURL(name, rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be a valid absolute URL in release mode", name)
	}
	if u.User != nil {
		return fmt.Errorf("%s must not include user info in release mode", name)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("%s must use https in release mode", name)
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return fmt.Errorf("%s host is not allowed in release mode", name)
	}
	return nil
}

func validateReleaseCORSOrigins(origins string) error {
	parts := splitComma(origins)
	if len(parts) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS must be set in release mode")
	}
	for _, origin := range parts {
		if origin == "*" || strings.Contains(origin, "*") {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain wildcards in release mode")
		}
		u, err := url.Parse(origin)
		if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS contains invalid origin %q", origin)
		}
		if !strings.EqualFold(u.Scheme, "https") {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must use https origins in release mode")
		}
	}
	return nil
}
