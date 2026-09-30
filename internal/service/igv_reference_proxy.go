package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var ErrIGVReferenceRange = errors.New("reference range is missing, invalid or too large")

const maxIGVReferenceRead = int64(8 << 20)
const maxIGVReferenceIndex = int64(1 << 20)

type IGVReferenceStream struct {
	Body         io.ReadCloser
	Length       int64
	ContentRange string
}

// Only the fixed reference FASTA/FAI for this execution can be read. Large
// FASTA downloads, arbitrary object paths and stale execution URLs are refused.
func (s *ResultService) OpenIGVReference(ctx context.Context, task *model.Task, asset, attempt, requestedRange string) (*IGVReferenceStream, error) {
	currentAttempt := task.ExecutionAttemptID
	if currentAttempt == "" {
		currentAttempt = task.UUID
	}
	if attempt != currentAttempt {
		return nil, ErrIGVEvidenceChanged
	}
	keys, err := cvmReferenceObjectKeys(declaredReferenceID(task.InputJSON), false)
	if err != nil {
		return nil, err
	}
	var key string
	switch asset {
	case "fasta":
		key = keys[0]
	case "fai":
		key = keys[1]
	default:
		return nil, fmt.Errorf("unknown reference resource")
	}
	cfg, err := cvmReferenceStorageConfig(s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	cfg.S3Bucket = strings.TrimSpace(cfg.CVMReferenceBucket)
	if cfg.S3Bucket == "" {
		cfg.S3Bucket = defaultCVMReferenceBucket
	}
	storage, err := newS3Storage(ctx, cfg)
	if err != nil {
		return nil, err
	}
	size, err := storage.stat(ctx, key)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("reference object unavailable")
	}
	start, end, err := igvReferenceRange(requestedRange, size, asset == "fasta")
	if err != nil {
		return nil, err
	}
	input := &s3.GetObjectInput{Bucket: aws.String(storage.bucket), Key: aws.String(key)}
	expectedRange := ""
	if requestedRange != "" {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", start, end))
		expectedRange = fmt.Sprintf("bytes %d-%d/%d", start, end, size)
	}
	output, err := storage.client.GetObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("reference read unavailable")
	}
	if aws.ToInt64(output.ContentLength) != end-start+1 || aws.ToString(output.ContentRange) != expectedRange {
		output.Body.Close()
		return nil, fmt.Errorf("reference storage did not honor bounded range")
	}
	return &IGVReferenceStream{Body: output.Body, Length: end - start + 1, ContentRange: expectedRange}, nil
}

func igvReferenceRange(value string, size int64, fasta bool) (int64, int64, error) {
	if size <= 0 {
		return 0, 0, ErrIGVReferenceRange
	}
	if value == "" {
		if fasta || size > maxIGVReferenceIndex {
			return 0, 0, ErrIGVReferenceRange
		}
		return 0, size - 1, nil
	}
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, ErrIGVReferenceRange
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 || parts[0] == "" {
		return 0, 0, ErrIGVReferenceRange
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, ErrIGVReferenceRange
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, ErrIGVReferenceRange
		}
		if end >= size {
			end = size - 1
		}
	}
	limit := maxIGVReferenceRead
	if !fasta {
		limit = maxIGVReferenceIndex
	}
	if end-start+1 > limit {
		return 0, 0, ErrIGVReferenceRange
	}
	return start, end, nil
}
