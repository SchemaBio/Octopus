package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func retentionPrefix(org, task string) (string, error) {
	if _, err := uuid.Parse(task); err != nil {
		return "", fmt.Errorf("invalid cleanup task")
	}
	if org == "" || strings.ContainsAny(org, "/\\\x00") || org == "." || org == ".." {
		return "", fmt.Errorf("invalid cleanup tenant")
	}
	return "organizations/" + org + "/workflows/" + task + "/", nil
}

// Delete all generations and unfinished uploads, not just current object keys.
func (s *s3Storage) purgePrefix(ctx context.Context, prefix string) error {
	if !strings.HasPrefix(prefix, "organizations/") || !strings.HasSuffix(prefix, "/") {
		return fmt.Errorf("invalid destruction prefix")
	}
	for {
		page, err := s.client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), MaxKeys: aws.Int32(1000)})
		if err != nil {
			return fmt.Errorf("object version inventory unavailable: %w", err)
		}
		if len(page.Versions)+len(page.DeleteMarkers) == 0 {
			break
		}
		remove := func(key, version *string) error {
			if !strings.HasPrefix(aws.ToString(key), prefix) {
				return fmt.Errorf("object outside cleanup prefix")
			}
			_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: key, VersionId: version})
			return err
		}
		for _, v := range page.Versions {
			if err := remove(v.Key, v.VersionId); err != nil {
				return err
			}
		}
		for _, v := range page.DeleteMarkers {
			if err := remove(v.Key, v.VersionId); err != nil {
				return err
			}
		}
	}
	for {
		page, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), MaxKeys: aws.Int32(1000)})
		if err != nil {
			return err
		}
		if len(page.Contents) == 0 {
			break
		}
		for _, v := range page.Contents {
			if !strings.HasPrefix(aws.ToString(v.Key), prefix) {
				return fmt.Errorf("object outside cleanup prefix")
			}
			if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: v.Key}); err != nil {
				return err
			}
		}
	}
	for {
		page, err := s.client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), MaxUploads: aws.Int32(1000)})
		if err != nil {
			return err
		}
		if len(page.Uploads) == 0 {
			break
		}
		for _, v := range page.Uploads {
			if !strings.HasPrefix(aws.ToString(v.Key), prefix) {
				return fmt.Errorf("upload outside cleanup prefix")
			}
			if _, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: aws.String(s.bucket), Key: v.Key, UploadId: v.UploadId}); err != nil {
				return err
			}
		}
	}
	// A final inventory catches producers that were still winding down.
	check, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), MaxKeys: aws.Int32(1)})
	if err != nil {
		return err
	}
	if len(check.Contents) > 0 {
		return fmt.Errorf("objects remain after cleanup")
	}
	generations, err := s.client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), MaxKeys: aws.Int32(1)})
	if err != nil {
		return err
	}
	if len(generations.Versions)+len(generations.DeleteMarkers) > 0 {
		return fmt.Errorf("object generations remain after cleanup")
	}
	return nil
}

func removeTaskDirectory(base, task string) error {
	if base == "" {
		return nil
	}
	if _, err := uuid.Parse(task); err != nil {
		return err
	}
	root, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	target := filepath.Join(root, task)
	if filepath.Dir(target) != root {
		return fmt.Errorf("invalid task directory")
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("cleanup directory is a symlink")
	}
	return os.RemoveAll(target)
}

func (s *s3Storage) promoteBaseline(ctx context.Context, source, target string) error {
	identity := sha256.Sum256([]byte(source))
	fingerprint := hex.EncodeToString(identity[:])
	// Retry after the task purge or a DB rollback can use the verified promoted copy.
	existing, existingErr := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(target)})
	if existingErr == nil && existing.Metadata["research-source"] == fingerprint && existing.Metadata["research-bytes"] == strconv.FormatInt(aws.ToInt64(existing.ContentLength), 10) && aws.ToInt64(existing.ContentLength) > 0 {
		return nil
	}
	before, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(source)})
	if err != nil {
		return err
	}
	if aws.ToInt64(before.ContentLength) <= 0 {
		return fmt.Errorf("empty baseline")
	}
	metadata := map[string]string{"research-source": fingerprint, "research-bytes": strconv.FormatInt(aws.ToInt64(before.ContentLength), 10)}
	_, err = s.client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(target), CopySource: aws.String(url.PathEscape(s.bucket + "/" + source)), Metadata: metadata, MetadataDirective: s3types.MetadataDirectiveReplace})
	if err != nil {
		return err
	}
	after, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(target)})
	if err != nil {
		return err
	}
	if aws.ToInt64(after.ContentLength) != aws.ToInt64(before.ContentLength) || after.Metadata["research-source"] != fingerprint {
		return fmt.Errorf("baseline promotion verification failed")
	}
	return nil
}

func (s *s3Storage) purgeExactObject(ctx context.Context, key string) error {
	// Exact matching prevents similarly named resources from being deleted.
	versions := s3.NewListObjectVersionsPaginator(s.client, &s3.ListObjectVersionsInput{Bucket: aws.String(s.bucket), Prefix: aws.String(key)})
	for versions.HasMorePages() {
		page, err := versions.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, v := range page.Versions {
			if aws.ToString(v.Key) == key {
				if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: v.Key, VersionId: v.VersionId}); err != nil {
					return err
				}
			}
		}
		for _, v := range page.DeleteMarkers {
			if aws.ToString(v.Key) == key {
				if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: v.Key, VersionId: v.VersionId}); err != nil {
					return err
				}
			}
		}
	}
	objects, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(key)})
	if err != nil {
		return err
	}
	for _, v := range objects.Contents {
		if aws.ToString(v.Key) == key {
			if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: v.Key}); err != nil {
				return err
			}
		}
	}
	uploads := s3.NewListMultipartUploadsPaginator(s.client, &s3.ListMultipartUploadsInput{Bucket: aws.String(s.bucket), Prefix: aws.String(key)})
	for uploads.HasMorePages() {
		page, err := uploads.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, v := range page.Uploads {
			if aws.ToString(v.Key) == key {
				if _, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: aws.String(s.bucket), Key: v.Key, UploadId: v.UploadId}); err != nil {
					return err
				}
			}
		}
	}
	generations := s3.NewListObjectVersionsPaginator(s.client, &s3.ListObjectVersionsInput{Bucket: aws.String(s.bucket), Prefix: aws.String(key)})
	for generations.HasMorePages() {
		page, err := generations.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, v := range page.Versions {
			if aws.ToString(v.Key) == key {
				return fmt.Errorf("object generations remain after cleanup")
			}
		}
		for _, v := range page.DeleteMarkers {
			if aws.ToString(v.Key) == key {
				return fmt.Errorf("object markers remain after cleanup")
			}
		}
	}
	remaining := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(key)})
	for remaining.HasMorePages() {
		page, err := remaining.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, v := range page.Contents {
			if aws.ToString(v.Key) == key {
				return fmt.Errorf("object remains after cleanup")
			}
		}
	}
	return nil
}
