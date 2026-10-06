package service

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResultDownloadService struct {
	cfg     *config.Config
	overlay *OverlayClient
}
type ResultDownloadFile struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
	Credits   int    `json:"credits"`
	Role      string `json:"role,omitempty"`
	key       string
}
type ResultDownloadCatalog struct {
	AttemptID string               `json:"attempt_id"`
	ZIPStatus string               `json:"zip_status"`
	ZIPError  string               `json:"zip_error,omitempty"`
	ZIP       *ResultDownloadFile  `json:"zip,omitempty"`
	BAMs      []ResultDownloadFile `json:"bams"`
	Missing   []string             `json:"missing"`
}

func NewResultDownloadService(cfg *config.Config) *ResultDownloadService {
	svc := &ResultDownloadService{cfg: cfg, overlay: NewOverlayClient(cfg.Overlay)}
	go svc.reconcileAbandonedDownloads()
	return svc
}
func downloadID(key string) string { s := sha256.Sum256([]byte(key)); return hex.EncodeToString(s[:]) }
func bamCredits(size int64) int {
	if size <= 0 {
		return 0
	}
	return int((size-1)/1_000_000_000 + 1)
}

// An unresolved cross-service payment cannot strand a charge indefinitely.
// Issued downloads are excluded; only expired grants whose local completion
// was never confirmed are refunded. Squid makes this refund idempotent.
func (s *ResultDownloadService) reconcileAbandonedDownloads() {
	if s.overlay == nil {
		return
	}
	timer := time.NewTicker(5 * time.Minute)
	defer timer.Stop()
	for range timer.C {
		var rows []model.ResultDownload
		if database.DB.Where("link_expires_at < ? AND charged_at IS NULL AND refunded_at IS NULL", time.Now().Add(-5*time.Minute)).Limit(100).Find(&rows).Error != nil {
			continue
		}
		for _, row := range rows {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var locked model.ResultDownload
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", row.ID).Error; err != nil {
					return err
				}
				if locked.ChargedAt != nil || locked.RefundedAt != nil {
					return nil
				}
				if err := s.overlay.RefundCredits(ctx, model.OverlayCreditRefundRequest{Actor: model.OverlayActor{UserID: locked.UserID, OrgID: locked.OrgID}, OrgID: locked.OrgID, ReferenceID: "download:" + locked.ID}); err != nil {
					return err
				}
				return tx.Model(&locked).Update("refunded_at", time.Now()).Error
			})
			cancel()
		}
	}
}

func (s *ResultDownloadService) Active(ctx context.Context, task *model.Task, actor model.OverlayActor, ip string) ([]model.ResultDownload, error) {
	rows := []model.ResultDownload{}
	err := database.DB.WithContext(ctx).Where("task_uuid = ? AND attempt_id = ? AND user_id = ? AND org_id = ? AND client_ip = ? AND link_expires_at > ? AND refunded_at IS NULL", task.UUID, task.ExecutionAttemptID, actor.UserID, actor.OrgID, ip, time.Now()).Order("created_at DESC").Limit(10).Find(&rows).Error
	return rows, err
}

// Catalog reads only this execution's final output manifest. It never accepts
// object paths supplied by the browser, nor searches old attempts.
func (s *ResultDownloadService) Catalog(ctx context.Context, task *model.Task, prepare bool) (*ResultDownloadCatalog, error) {
	if err := validateResultPackageTask(s.cfg, task); err != nil {
		return nil, err
	}
	if task.Status != model.TaskStatusCompleted {
		return nil, fmt.Errorf("分析尚未完成，暂不可下载原始结果")
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	prefix := resultPackagePrefix(task)
	objects, err := storage.list(ctx, prefix+"/")
	if err != nil {
		return nil, fmt.Errorf("无法读取结果归档")
	}
	manifestKey, byBase, _, err := igvArchiveObjectIndex(prefix, objects)
	if err != nil {
		return nil, err
	}
	manifest, err := readIGVManifest(ctx, storage, manifestKey)
	if err != nil {
		return nil, err
	}
	var embeddedQC []interface{}
	var findQC func(interface{})
	findQC = func(value interface{}) {
		switch value := value.(type) {
		case map[string]interface{}:
			for field, child := range value {
				if field == "qc_result" {
					if _, ok := child.(map[string]interface{}); ok {
						embeddedQC = append(embeddedQC, child)
					}
					if rows, ok := child.([]interface{}); ok {
						for _, row := range rows {
							if _, ok := row.(map[string]interface{}); ok {
								embeddedQC = append(embeddedQC, row)
							}
						}
					}
				} else {
					findQC(child)
				}
			}
		case []interface{}:
			for _, child := range value {
				findQC(child)
			}
		}
	}
	findQC(manifest)
	qcJSON, _ := json.Marshal(embeddedQC)
	catalog := &ResultDownloadCatalog{AttemptID: task.ExecutionAttemptID, ZIPStatus: "pending", BAMs: []ResultDownloadFile{}, Missing: []string{}}
	members := manifestStringValues(manifest, "members")
	for i, ref := range manifestFileRefs(manifest, "bam") {
		object, ok := archiveObjectForRef(ref, byBase)
		if !ok {
			catalog.Missing = append(catalog.Missing, fmt.Sprintf("BAM %d 未归档或名称不唯一", i+1))
			continue
		}
		size, err := storage.stat(ctx, object.Key)
		if err != nil || size <= 0 {
			return nil, fmt.Errorf("BAM 文件大小读取失败")
		}
		catalog.BAMs = append(catalog.BAMs, ResultDownloadFile{ID: downloadID(object.Key), Filename: path.Base(object.Key), SizeBytes: size, Credits: bamCredits(size), Role: manifestMemberRole(members, i, len(manifestFileRefs(manifest, "bam")) > 1), key: object.Key})
	}
	// Explicit final outputs only; intermediate BAMs, reads and databases are
	// excluded. Existing workflows without an MT VCF cannot manufacture one.
	fields := []string{"vcf_raw", "vcf_raw_tbi", "mt_vcf", "mt_vcf_tbi", "snp_indel", "mt", "cnv_region", "cnv_gene", "mei", "upd", "roh", "qc_result", "str"}
	sources := map[string]s3ObjectInfo{}
	for _, field := range fields {
		refs := manifestFileRefs(manifest, field)
		if len(refs) == 0 {
			if field == "qc_result" && len(embeddedQC) > 0 {
				continue
			}
			if field != "vcf_raw_tbi" && field != "mt_vcf_tbi" {
				catalog.Missing = append(catalog.Missing, field+" 未产生或未声明")
			}
			continue
		}
		for _, ref := range refs {
			object, ok := archiveObjectForRef(ref, byBase)
			if !ok {
				return nil, fmt.Errorf("原始结果 %s 未归档或名称不唯一", field)
			}
			if object.Size <= 0 {
				return nil, fmt.Errorf("原始结果 %s 为空", field)
			}
			sources[object.Key] = object
		}
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("未找到可打包的原始结果")
	}
	keys := make([]string, 0, len(sources))
	for key := range sources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	var total int64
	for _, key := range keys {
		o := sources[key]
		total += o.Size
		fmt.Fprintf(hash, "%s\x00%d\x00%d\n", key, o.Size, o.LastModified.UnixNano())
	}
	// Leave sufficient disk headroom and reject unexpectedly large reports.
	maxBytes := int64(s.cfg.Report.PackageMaxSizeMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 512 << 20
	}
	if total > maxBytes {
		return nil, fmt.Errorf("原始结果包超过配置的大小限制")
	}
	fingerprint := hex.EncodeToString(hash.Sum(nil))
	if len(embeddedQC) > 0 {
		hash.Write(qcJSON)
		fingerprint = hex.EncodeToString(hash.Sum(nil))
	}
	key := path.Join(prefix, "_raw-downloads", fingerprint+".zip")
	if size, err := storage.stat(ctx, key); err == nil && size > 0 {
		catalog.ZIPStatus = "ready"
		catalog.ZIP = &ResultDownloadFile{ID: downloadID(key), Filename: "task-" + task.UUID + "-raw-results.zip", SizeBytes: size, Credits: 1, key: key}
		return catalog, nil
	}
	var pkg model.RawResultPackage
	if err := database.DB.Where("id = ?", downloadID(key)).First(&pkg).Error; err == nil {
		catalog.ZIPStatus = pkg.Status
		catalog.ZIPError = pkg.Error
	}
	if !prepare {
		return catalog, nil
	}
	claimed := false
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		row := model.RawResultPackage{ID: downloadID(key), ObjectKey: key, Status: "pending"}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", row.ID).Error; err != nil {
			return err
		}
		if row.Status == "building" && time.Since(row.UpdatedAt) < 20*time.Minute {
			return nil
		}
		row.Status = "building"
		row.Error = ""
		claimed = true
		return tx.Save(&row).Error
	})
	if err != nil {
		return nil, err
	}
	if claimed {
		go s.buildRawZIP(storage, key, prefix, keys, sources, catalog.Missing, qcJSON)
	}
	catalog.ZIPStatus = "building"
	catalog.ZIPError = ""
	return catalog, nil
}

func (s *ResultDownloadService) buildRawZIP(storage *s3Storage, key, prefix string, keys []string, sources map[string]s3ObjectInfo, missing []string, qcJSON []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	err := func() error {
		file, err := os.CreateTemp("", "octopus-raw-*.zip")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		defer file.Close()
		zw := zip.NewWriter(file)
		defer zw.Close()
		included := []string{}
		for _, source := range keys {
			rel, err := safeResultPackageRelativePath(prefix, source)
			if err != nil {
				return err
			}
			reader, err := storage.open(ctx, source)
			if err != nil {
				return err
			}
			writer, err := zw.CreateHeader(&zip.FileHeader{Name: rel, Method: zip.Store})
			var n int64
			if err == nil {
				n, err = io.Copy(writer, io.LimitReader(reader, sources[source].Size+1))
			}
			reader.Close()
			if err != nil {
				return err
			}
			if n != sources[source].Size {
				return fmt.Errorf("source size changed")
			}
			included = append(included, rel)
		}
		if string(qcJSON) != "null" && string(qcJSON) != "[]" {
			writer, err := zw.Create("reports/QC.json")
			if err != nil {
				return err
			}
			if _, err = writer.Write(qcJSON); err != nil {
				return err
			}
			included = append(included, "reports/QC.json")
		}
		writer, err := zw.Create("download-manifest.json")
		if err != nil {
			return err
		}
		if err = json.NewEncoder(writer).Encode(map[string]interface{}{"included": included, "unavailable": missing, "manual_adjustments_included": false}); err != nil {
			return err
		}
		if err = zw.Close(); err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if _, err = file.Seek(0, 0); err != nil {
			return err
		}
		return storage.putReader(ctx, key, "application/zip", file, info.Size())
	}()
	state, message := "ready", ""
	if err != nil {
		state = "failed"
		message = "原始结果打包失败，请重试；未扣除积分"
	}
	database.DB.Model(&model.RawResultPackage{}).Where("id = ?", downloadID(key)).Updates(map[string]interface{}{"status": state, "error": message, "updated_at": time.Now()})
}

func (s *ResultDownloadService) Quote(ctx context.Context, task *model.Task, actor model.OverlayActor, ip, kind, fileID string) (*model.ResultDownload, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil || !parsedIP.IsGlobalUnicast() || parsedIP.IsPrivate() || actor.OrgID == "" || actor.OrgID != task.ExternalOrgID {
		return nil, fmt.Errorf("下载身份或来源 IP 无效")
	}
	catalog, err := s.Catalog(ctx, task, false)
	if err != nil {
		return nil, err
	}
	var chosen *ResultDownloadFile
	if kind == "zip" && catalog.ZIP != nil {
		chosen = catalog.ZIP
	}
	if kind == "bam" {
		for i := range catalog.BAMs {
			if catalog.BAMs[i].ID == fileID {
				chosen = &catalog.BAMs[i]
				break
			}
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("下载文件尚未就绪")
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	meta, err := storage.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(storage.bucket), Key: aws.String(chosen.key)})
	if err != nil || aws.ToInt64(meta.ContentLength) <= 0 {
		return nil, fmt.Errorf("无法确认下载文件大小")
	}
	size := aws.ToInt64(meta.ContentLength)
	credits := 1
	if kind == "bam" {
		credits = bamCredits(size)
	}
	if credits > 1_000_000 {
		return nil, fmt.Errorf("下载文件大小超限")
	}
	quote := &model.ResultDownload{ID: uuid.NewString(), TaskUUID: task.UUID, AttemptID: task.ExecutionAttemptID, OrgID: actor.OrgID, UserID: actor.UserID, ClientIP: ip, Kind: kind, ObjectKey: chosen.key, Filename: chosen.Filename, SizeBytes: size, ETag: aws.ToString(meta.ETag), Credits: credits, QuoteExpiresAt: time.Now().Add(10 * time.Minute)}
	if err = database.DB.Create(quote).Error; err != nil {
		return nil, err
	}
	return quote, nil
}

// Issue serializes one quote, prepares authorization before billing and uses
// the quote UUID as the ledger key. A transport retry never creates a new fee.
func (s *ResultDownloadService) Issue(ctx context.Context, task *model.Task, actor model.OverlayActor, ip, id string) (map[string]interface{}, error) {
	if s.overlay == nil {
		return nil, fmt.Errorf("积分服务未配置，无法申请下载")
	}
	var link string
	var quote model.ResultDownload
	// Commit the validity window before crossing the billing service boundary.
	// If its response or the final DB commit is lost, retries retain the original
	// deadline and ledger reference instead of extending the grant or recharging.
	reserveErr := database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_uuid = ? AND attempt_id = ? AND user_id = ? AND org_id = ?", id, task.UUID, task.ExecutionAttemptID, actor.UserID, actor.OrgID).First(&quote).Error; err != nil {
			return fmt.Errorf("下载申请不存在")
		}
		if quote.ClientIP != ip {
			return fmt.Errorf("网络 IP 已改变，请重新申请下载")
		}
		if quote.RefundedAt != nil {
			return fmt.Errorf("下载申请已关闭")
		}
		now := time.Now().UTC()
		if quote.LinkExpiresAt == nil {
			if now.After(quote.QuoteExpiresAt) {
				return fmt.Errorf("下载报价已过期，请重新申请")
			}
			expires := now.Add(3 * time.Hour)
			quote.LinkExpiresAt = &expires
			return tx.Save(&quote).Error
		}
		return nil
	})
	if reserveErr != nil {
		return nil, reserveErr
	}
	err := database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_uuid = ? AND attempt_id = ? AND user_id = ? AND org_id = ?", id, task.UUID, task.ExecutionAttemptID, actor.UserID, actor.OrgID).First(&quote).Error; err != nil {
			return fmt.Errorf("下载申请不存在")
		}
		if quote.ClientIP != ip {
			return fmt.Errorf("网络 IP 已改变，请重新申请下载")
		}
		now := time.Now().UTC()
		if quote.LinkExpiresAt == nil && now.After(quote.QuoteExpiresAt) {
			return fmt.Errorf("下载报价已过期，请重新申请")
		}
		if quote.LinkExpiresAt == nil {
			expires := now.Add(3 * time.Hour)
			quote.LinkExpiresAt = &expires
		}
		if !now.Before(*quote.LinkExpiresAt) {
			return fmt.Errorf("下载链接已过期，请重新申请")
		}
		storage, err := newS3Storage(ctx, s.cfg.Storage)
		if err != nil {
			return err
		}
		meta, err := storage.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(storage.bucket), Key: aws.String(quote.ObjectKey)})
		if err != nil || aws.ToInt64(meta.ContentLength) != quote.SizeBytes || aws.ToString(meta.ETag) != quote.ETag {
			return fmt.Errorf("文件发生变化或不可用，请重新申请；本次未扣费")
		}
		link, err = ipBoundCOSDownload(ctx, s.cfg.Storage, quote.ObjectKey, quote.Filename, ip, *quote.LinkExpiresAt)
		if err != nil {
			return err
		}
		if quote.ChargedAt == nil {
			code := model.BillingCodeResultZIP
			if quote.Kind == "bam" {
				code = model.BillingCodeResultBAM
			}
			charge, err := s.overlay.ChargeCredits(ctx, model.OverlayCreditChargeRequest{Actor: actor, OrgID: actor.OrgID, ReferenceID: "download:" + quote.ID, BillingCode: code, Quantity: quote.Credits, Description: fmt.Sprintf("原始结果 %s 下载：任务 %s，%d bytes", quote.Kind, task.UUID, quote.SizeBytes)})
			if err != nil {
				return err
			}
			if !charge.Allowed || charge.CreditsCharged != quote.Credits {
				return fmt.Errorf("下载扣费确认失败")
			}
			quote.ChargedAt = &now
		}
		return tx.Save(&quote).Error
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": quote.ID, "url": link, "filename": quote.Filename, "size_bytes": quote.SizeBytes, "credits_charged": quote.Credits, "expires_at": quote.LinkExpiresAt, "ip_bound": true}, nil
}
