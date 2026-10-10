package config

import (
	"testing"
	"time"
)

func TestResultDownloadDefaultsAndValidation(t *testing.T) {
	for _, name := range []string{"RESULT_DOWNLOAD_LINK_TTL", "RESULT_DOWNLOAD_REFRESH_INTERVAL", "RESULT_DOWNLOAD_MAX_ISSUES", "RESULT_DOWNLOAD_TRAFFIC_LIMIT_BPS", "RESULT_DOWNLOAD_PAUSE_FILE"} {
		t.Setenv(name, "")
	}
	cfg := Load()
	if cfg.Storage.ResultDownloadLinkTTL != 30*time.Minute || cfg.Storage.ResultDownloadRefreshInterval != time.Minute || cfg.Storage.ResultDownloadMaxIssues != 12 || cfg.Storage.ResultDownloadTrafficLimit != 83886080 || cfg.Storage.ResultDownloadPauseFile != "/data/archive/.downloads-paused" {
		t.Fatal("wrong download defaults")
	}
	for _, test := range []struct {
		name    string
		storage StorageConfig
	}{
		{"long TTL", StorageConfig{ResultDownloadLinkTTL: 2 * time.Hour}},
		{"negative TTL", StorageConfig{ResultDownloadLinkTTL: -time.Minute}},
		{"slow refresh", StorageConfig{ResultDownloadLinkTTL: time.Minute, ResultDownloadRefreshInterval: 2 * time.Minute}},
		{"negative count", StorageConfig{ResultDownloadMaxIssues: -1}},
		{"excessive count", StorageConfig{ResultDownloadMaxIssues: 101}},
		{"invalid speed", StorageConfig{ResultDownloadTrafficLimit: 100}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateStartup(&Config{Storage: test.storage}); err == nil {
				t.Fatal("invalid download controls accepted")
			}
		})
	}
}
