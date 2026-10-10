package service

import (
	"fmt"
	"os"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func downloadSettings(cfg config.StorageConfig) config.StorageConfig {
	if cfg.ResultDownloadLinkTTL == 0 {
		cfg.ResultDownloadLinkTTL = 30 * time.Minute
	}
	if cfg.ResultDownloadRefreshInterval == 0 {
		cfg.ResultDownloadRefreshInterval = time.Minute
	}
	if cfg.ResultDownloadMaxIssues == 0 {
		cfg.ResultDownloadMaxIssues = 12
	}
	if cfg.ResultDownloadTrafficLimit == 0 {
		cfg.ResultDownloadTrafficLimit = 10 * 1024 * 1024 * 8
	}
	return cfg
}

func downloadSigningAvailable(cfg config.StorageConfig) error {
	if cfg.ResultDownloadPauseFile == "" {
		return nil
	}
	_, err := os.Stat(cfg.ResultDownloadPauseFile)
	if os.IsNotExist(err) {
		return nil
	}
	// Fail closed if the marker cannot be checked.
	return fmt.Errorf("下载链接签发已暂停，请稍后重试同一申请；不会重复扣费")
}

// Called under the grant row lock. The paid deadline is never moved forward.
func downloadIssueDeadline(cfg config.StorageConfig, grant *model.ResultDownload, now time.Time) (time.Time, error) {
	cfg = downloadSettings(cfg)
	if grant.RefundedAt != nil {
		return time.Time{}, fmt.Errorf("下载申请已关闭")
	}
	if grant.LinkExpiresAt == nil || !now.Before(*grant.LinkExpiresAt) {
		return time.Time{}, fmt.Errorf("下载授权已过期，请重新申请")
	}
	if grant.IssueCount >= cfg.ResultDownloadMaxIssues {
		return time.Time{}, fmt.Errorf("本申请链接签发次数已用完；已领取的有效链接仍可使用")
	}
	if grant.LastIssuedAt != nil && now.Before(grant.LastIssuedAt.Add(cfg.ResultDownloadRefreshInterval)) {
		seconds := int(grant.LastIssuedAt.Add(cfg.ResultDownloadRefreshInterval).Sub(now).Seconds()) + 1
		return time.Time{}, fmt.Errorf("请等待 %d 秒后刷新同一申请，不会重复扣费", seconds)
	}
	expires := now.Add(cfg.ResultDownloadLinkTTL)
	if expires.After(*grant.LinkExpiresAt) {
		expires = *grant.LinkExpiresAt
	}
	return expires, nil
}
