package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3Storage struct {
	bucket  string
	client  *s3.Client
	presign *s3.PresignClient
	expiry  time.Duration
}

type s3ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

func newS3Storage(ctx context.Context, cfg config.StorageConfig) (*s3Storage, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.S3Region)}
	if cfg.S3AccessKey != "" {
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3SessionToken,
		)))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.S3UsePathStyle
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}
	})
	presignClient := client
	if cfg.S3PublicEndpoint != "" {
		presignClient = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.UsePathStyle = cfg.S3UsePathStyle
			o.BaseEndpoint = aws.String(cfg.S3PublicEndpoint)
		})
	}
	expiry := cfg.PresignExpiry
	if expiry <= 0 {
		expiry = 15 * time.Minute
	}
	return &s3Storage{bucket: cfg.S3Bucket, client: client, presign: s3.NewPresignClient(presignClient), expiry: expiry}, nil
}

func (s *s3Storage) presignUpload(ctx context.Context, key string) (string, error) {
	request, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String("application/octet-stream"),
	}, func(o *s3.PresignOptions) { o.Expires = s.expiry })
	if err != nil {
		return "", fmt.Errorf("presign S3 upload: %w", err)
	}
	return request.URL, nil
}

func (s *s3Storage) presignDownload(ctx context.Context, key, filename string) (string, error) {
	disposition := fmt.Sprintf("attachment; filename=%q", filename)
	request, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ResponseContentDisposition: aws.String(disposition),
	}, func(o *s3.PresignOptions) { o.Expires = s.expiry })
	if err != nil {
		return "", fmt.Errorf("presign S3 download: %w", err)
	}
	return request.URL, nil
}

func (s *s3Storage) stat(ctx context.Context, key string) (int64, error) {
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return 0, fmt.Errorf("stat S3 object: %w", err)
	}
	return aws.ToInt64(output.ContentLength), nil
}

func (s *s3Storage) delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}

func (s *s3Storage) open(ctx context.Context, key string) (io.ReadCloser, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

func (s *s3Storage) list(ctx context.Context, prefix string) ([]s3ObjectInfo, error) {
	var items []s3ObjectInfo
	var token *string
	for {
		output, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("list S3 objects: %w", err)
		}
		for _, object := range output.Contents {
			if object.Key == nil || strings.HasSuffix(*object.Key, "/") {
				continue
			}
			item := s3ObjectInfo{Key: *object.Key, Size: aws.ToInt64(object.Size)}
			if object.LastModified != nil {
				item.LastModified = *object.LastModified
			}
			items = append(items, item)
		}
		if !aws.ToBool(output.IsTruncated) || output.NextContinuationToken == nil {
			break
		}
		token = output.NextContinuationToken
	}
	return items, nil
}
