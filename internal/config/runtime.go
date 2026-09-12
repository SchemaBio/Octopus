package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// RequireSecureRuntime refuses non-release Gin modes unless the operator
// explicitly opts into insecure development.
func RequireSecureRuntime(mode string) error {
	if mode == "release" {
		return nil
	}
	if os.Getenv("ALLOW_INSECURE_DEV") == "true" {
		return nil
	}
	return fmt.Errorf("GIN_MODE must be release unless ALLOW_INSECURE_DEV=true")
}

// AllowSeed refuses the seed command in release mode unless ALLOW_SEED=true.
func AllowSeed(mode string) error {
	if mode != "release" {
		return nil
	}
	if os.Getenv("ALLOW_SEED") == "true" {
		return nil
	}
	return fmt.Errorf("cmd/seed is disabled in release mode unless ALLOW_SEED=true")
}

func validateDatabaseTLS(dsn string) error {
	host, sslmode := dsnHostAndSSLMode(dsn)
	if host == "" || isLocalDatabaseHost(host) {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(sslmode)) {
	case "require", "verify-ca", "verify-full":
		return nil
	default:
		return fmt.Errorf("DB_DSN for remote host %q must use sslmode=require (or verify-ca/verify-full)", host)
	}
}

func dsnHostAndSSLMode(dsn string) (host, sslmode string) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return "", ""
	}
	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", ""
		}
		return u.Hostname(), u.Query().Get("sslmode")
	}
	for _, field := range strings.Fields(dsn) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "host":
			host = value
		case "sslmode":
			sslmode = value
		}
	}
	return host, sslmode
}

func isLocalDatabaseHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" || h == "localhost" || h == "postgres" {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return !strings.Contains(h, ".")
}
