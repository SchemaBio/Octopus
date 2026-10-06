package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

// InspectResultDownloadAuthorization is a controlled read-only deployment
// check. It issues restricted STS credentials and reads at most one byte; it
// never creates a quote, starts a package build or calls the credit service.
func InspectResultDownloadAuthorization(ctx context.Context, cfg *config.Config, task *model.Task, ip string, probe bool) (map[string]interface{}, error) {
	svc := &ResultDownloadService{cfg: cfg}
	catalog, err := svc.Catalog(ctx, task, false)
	if err != nil {
		return nil, err
	}
	if len(catalog.BAMs) == 0 {
		return nil, fmt.Errorf("没有可验证的 BAM")
	}
	file := catalog.BAMs[0]
	expires := time.Now().UTC().Add(3 * time.Hour)
	link, err := ipBoundCOSDownload(ctx, cfg.Storage, file.key, file.Filename, ip, expires)
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{"attempt_id": catalog.AttemptID, "bam_size_bytes": file.SizeBytes, "bam_credits": file.Credits, "sts_three_hour_grant": "ok", "missing_outputs": catalog.Missing, "credits_charged": 0}
	if !probe {
		return result, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, fmt.Errorf("无法创建读取验证请求")
	}
	request.Header.Set("Range", "bytes=0-0")
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return nil, fmt.Errorf("COS 读取验证请求失败")
	}
	if response.StatusCode != 206 {
		var failure struct {
			Code string `xml:"Code"`
		}
		xml.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&failure)
		result["cos_error_code"] = failure.Code
	} else {
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	}
	response.Body.Close()
	result["range_http_status"] = response.StatusCode
	result["content_range"] = response.Header.Get("Content-Range")
	if response.StatusCode != 206 {
		return result, fmt.Errorf("COS 单字节读取返回 HTTP %d", response.StatusCode)
	}
	// A second grant bound to a documentation IP must fail from this host.
	wrongLink, err := ipBoundCOSDownload(ctx, cfg.Storage, file.key, file.Filename, "203.0.113.1", expires)
	if err != nil {
		return result, err
	}
	request, _ = http.NewRequestWithContext(ctx, http.MethodGet, wrongLink, nil)
	request.Header.Set("Range", "bytes=0-0")
	response, err = (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return result, fmt.Errorf("COS 异地 IP 验证请求失败")
	}
	io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	response.Body.Close()
	result["different_ip_http_status"] = response.StatusCode
	if response.StatusCode != 403 {
		return result, fmt.Errorf("COS IP 限制验证未通过")
	}
	return result, nil
}
