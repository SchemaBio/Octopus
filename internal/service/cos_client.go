package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/tencentyun/cos-go-sdk-v5"
)

type COSClient struct {
	cfg    *config.StorageConfig
	client *cos.Client
}

func NewCOSClient(cfg *config.StorageConfig) (*COSClient, error) {
	bucketURL, err := url.Parse(cfg.COSEndpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid COS endpoint: %w", err)
	}

	baseURL := &cos.BaseURL{BucketURL: bucketURL}
	client := cos.NewClient(baseURL, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  cfg.COSSecretID,
			SecretKey: cfg.COSSecretKey,
		},
	})

	return &COSClient{
		cfg:    cfg,
		client: client,
	}, nil
}

func (c *COSClient) GeneratePresignedPutURL(ctx context.Context, key string) (string, error) {
	ttl := time.Duration(c.cfg.PresignTTL) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}

	presignedURL, err := c.client.Object.GetPresignedURL(
		ctx,
		http.MethodPut,
		key,
		c.cfg.COSSecretID,
		c.cfg.COSSecretKey,
		ttl,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return presignedURL.String(), nil
}

func (c *COSClient) GeneratePresignedGetURL(ctx context.Context, key string) (string, error) {
	ttl := time.Duration(c.cfg.PresignTTL) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}

	presignedURL, err := c.client.Object.GetPresignedURL(
		ctx,
		http.MethodGet,
		key,
		c.cfg.COSSecretID,
		c.cfg.COSSecretKey,
		ttl,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return presignedURL.String(), nil
}

func (c *COSClient) DeleteObject(ctx context.Context, key string) error {
	_, err := c.client.Object.Delete(ctx, key)
	return err
}

func (c *COSClient) ObjectExists(ctx context.Context, key string) (bool, error) {
	resp, err := c.client.Object.Head(ctx, key, nil)
	if err != nil {
		return false, nil
	}
	return resp.StatusCode == http.StatusOK, nil
}
