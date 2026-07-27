package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SchemaBio/Octopus/internal/config"
)

type TenantStorageService struct {
	cfg *config.Config
}

func NewTenantStorageService(cfg *config.Config) *TenantStorageService {
	return &TenantStorageService{cfg: cfg}
}

func (s *TenantStorageService) Initialize(ctx context.Context, orgID string) (string, error) {
	if s.cfg == nil || s.cfg.Storage.Provider != "s3" {
		return "", fmt.Errorf("tenant object storage is not enabled")
	}
	orgID, err := normalizeTenantID(orgID)
	if err != nil {
		return "", err
	}
	prefix, err := tenantStoragePrefix(orgID)
	if err != nil {
		return "", err
	}
	store, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return "", err
	}
	manifest, err := json.Marshal(map[string]interface{}{
		"layout_version":  tenantStorageLayoutVersion,
		"organization_id": orgID,
	})
	if err != nil {
		return "", fmt.Errorf("encode tenant storage manifest: %w", err)
	}
	if err := store.put(ctx, prefix+"/.tenant.json", "application/json", append(manifest, '\n')); err != nil {
		return "", err
	}
	markers, err := tenantDirectoryMarkers(orgID)
	if err != nil {
		return "", err
	}
	for _, marker := range markers {
		if err := store.put(ctx, marker, "application/octet-stream", nil); err != nil {
			return "", err
		}
	}
	return prefix, nil
}
