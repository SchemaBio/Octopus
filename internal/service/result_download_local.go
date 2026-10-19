package service

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

type localDownloadSource struct {
	generated bool
	file      ResultDownloadFile
	filename  string
	modified  time.Time
	zipped    bool
}
type localDownloadArchive struct {
	catalog *ResultDownloadCatalog
	sources []localDownloadSource
	zipPath string
	qc      []byte
}
type localDownloadBuild struct {
	mu      sync.Mutex
	status  string
	message string
}

var localDownloadBuilds sync.Map

func (s *ResultDownloadService) localDownloadStorage() bool {
	return s.cfg != nil && !s.cfg.IsSaaS() && s.cfg.Storage.Provider == "local"
}

func (s *ResultDownloadService) collectLocalDownloads(task *model.Task) (*localDownloadArchive, error) {
	if task == nil || task.Status != model.TaskStatusCompleted {
		return nil, fmt.Errorf("分析尚未完成，暂不可下载归档结果")
	}
	if task.ResultImportStatus == model.ResultImportStatusPending {
		return nil, fmt.Errorf("结果正在归档，请等待归档完成后刷新")
	}
	dir, err := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
	if err != nil {
		return nil, err
	}
	raw, err := (&Archiver{cfg: s.cfg}).readArchiveJSONFile(dir, "outputs.resolved.json")
	if err != nil {
		return nil, fmt.Errorf("本地结果归档尚未就绪")
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("本地结果清单无效")
	}
	metadataFiles := map[string]string{}
	if task.Template == "vcf2anno" {
		manifest, err = vcfDownloadManifest(manifest, func(name string, data []byte) (string, error) {
			filename, e := cacheVCFMetadata(s.cfg.ResultQuery.CacheDir, task, name, data)
			if e != nil {
				return "", e
			}
			metadataFiles[name] = filename
			return name, nil
		})
		if err != nil {
			return nil, err
		}
	}
	catalog := &ResultDownloadCatalog{Transport: "local", AttemptID: executionAttempt(task), ZIPStatus: "pending", BAMStatus: "available", BAMs: []ResultDownloadFile{}, Files: []ResultDownloadFile{}, Missing: []string{}}
	archive := &localDownloadArchive{catalog: catalog}
	seen := map[string]bool{}
	fields := []string{"vcf_raw", "vcf_raw_tbi", "mt_vcf", "mt_vcf_tbi", "snp_indel", "snv", "mt", "cnv_region", "cnv_gene", "mei", "upd", "roh", "qc_result", "str", "bam", "bai"}
	if task.Template == "vcf2anno" {
		fields = []string{"normalized_vcf", "normalized_vcf_tbi", "annotated_vcf", "annotated_vcf_tbi", "snp_indel", "preprocessing", "sex_inference", "sex_explanation", "summary"}
		catalog.BAMStatus = "not_provided"
	}
	for _, field := range fields {
		refs := manifestFileRefs(manifest, field)
		for _, ref := range refs {
			base := archiveReferenceBaseName(ref)
			if task.Template == "vcf2anno" && (field == "snp_indel" || field == "sex_explanation") && strings.HasSuffix(strings.ToLower(base), ".parquet") {
				base = strings.TrimSuffix(base, filepath.Ext(base)) + ".txt"
			}
			if base == "" || seen[base] {
				continue
			}
			lower := strings.ToLower(base)
			if field == "bam" && !strings.HasSuffix(lower, ".bam") || field == "bai" && !strings.HasSuffix(lower, ".bai") {
				continue
			}
			if field != "bam" && field != "bai" {
				allowed := false
				for _, suffix := range []string{".vcf", ".vcf.gz", ".tbi", ".txt", ".tsv", ".csv", ".json", ".parquet"} {
					if strings.HasSuffix(lower, suffix) {
						allowed = true
					}
				}
				if !allowed {
					catalog.Missing = append(catalog.Missing, field+" 不是受支持的结果文件")
					continue
				}
			}
			filename, err := resolveArchiveRegularFile(dir, filepath.Join(dir, base))
			if generated, ok := metadataFiles[base]; ok {
				filename, err = generated, nil
			}
			if err != nil {
				catalog.Missing = append(catalog.Missing, field+" 未归档或不可读取")
				continue
			}
			info, err := os.Stat(filename)
			if err != nil || info.Size() == 0 {
				catalog.Missing = append(catalog.Missing, field+" 文件为空或不可读取")
				continue
			}
			seen[base] = true
			file := ResultDownloadFile{ID: downloadID(task.UUID + "/" + executionAttempt(task) + "/" + base), Filename: base, SizeBytes: info.Size()}
			archive.sources = append(archive.sources, localDownloadSource{generated: metadataFiles[base] != "", file: file, filename: filename, modified: info.ModTime(), zipped: field != "bam" && field != "bai"})
			catalog.Files = append(catalog.Files, file)
			if field == "bam" {
				catalog.BAMs = append(catalog.BAMs, file)
			}
		}
	}
	// QC is often embedded in the manifest rather than archived as a separate file.
	var qc []interface{}
	var findQC func(interface{})
	findQC = func(value interface{}) {
		switch node := value.(type) {
		case map[string]interface{}:
			for key, child := range node {
				if key == "qc_result" {
					switch item := child.(type) {
					case map[string]interface{}:
						qc = append(qc, item)
					case []interface{}:
						for _, item := range item {
							if _, ok := item.(map[string]interface{}); ok {
								qc = append(qc, item)
							}
						}
					}
				} else {
					findQC(child)
				}
			}
		case []interface{}:
			for _, child := range node {
				findQC(child)
			}
		}
	}
	findQC(manifest)
	if len(qc) > 0 && !seen["qc-result.json"] {
		archive.qc, _ = json.MarshalIndent(qc, "", "  ")
	}
	if len(archive.sources) == 0 && len(qc) == 0 {
		return nil, fmt.Errorf("未找到可下载的归档结果")
	}
	sort.Slice(archive.sources, func(i, j int) bool { return archive.sources[i].file.Filename < archive.sources[j].file.Filename })
	sort.Slice(catalog.Files, func(i, j int) bool { return catalog.Files[i].Filename < catalog.Files[j].Filename })
	hash := sha256.New()
	hash.Write([]byte(task.UUID + "/" + executionAttempt(task)))
	hash.Write(raw)
	for _, source := range archive.sources {
		fmt.Fprintf(hash, "%s\x00%d\x00%d\n", source.file.Filename, source.file.SizeBytes, source.modified.UnixNano())
	}
	catalog.Version = fmt.Sprintf("%x", hash.Sum(nil))
	if s.cfg.ResultQuery.CacheDir == "" {
		return nil, fmt.Errorf("本地结果缓存目录未配置")
	}
	archive.zipPath = filepath.Join(s.cfg.ResultQuery.CacheDir, "downloads", catalog.Version+".zip")
	return archive, nil
}

func (s *ResultDownloadService) localDownloadCatalog(task *model.Task, prepare bool) (*ResultDownloadCatalog, error) {
	archive, err := s.collectLocalDownloads(task)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(archive.zipPath); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		archive.catalog.ZIPStatus = "ready"
		archive.catalog.ZIP = &ResultDownloadFile{ID: downloadID(archive.zipPath), Filename: "task-" + task.UUID + "-results.zip", SizeBytes: info.Size()}
		return archive.catalog, nil
	}
	state := &localDownloadBuild{status: "building"}
	value, exists := localDownloadBuilds.Load(archive.zipPath)
	if !exists && prepare {
		value, exists = localDownloadBuilds.LoadOrStore(archive.zipPath, state)
		if !exists {
			value = state
			copied := *task
			go s.buildLocalDownloadZIP(&copied, archive, state)
		}
	}
	if value != nil {
		current := value.(*localDownloadBuild)
		current.mu.Lock()
		status, message := current.status, current.message
		current.mu.Unlock()
		if status == "failed" && prepare && localDownloadBuilds.CompareAndSwap(archive.zipPath, current, state) {
			copied := *task
			go s.buildLocalDownloadZIP(&copied, archive, state)
			status, message = "building", ""
		}
		archive.catalog.ZIPStatus, archive.catalog.ZIPError = status, message
	}
	if info, err := os.Stat(archive.zipPath); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		archive.catalog.ZIPStatus, archive.catalog.ZIPError = "ready", ""
		archive.catalog.ZIP = &ResultDownloadFile{ID: downloadID(archive.zipPath), Filename: "task-" + task.UUID + "-results.zip", SizeBytes: info.Size()}
	}
	return archive.catalog, nil
}

func (s *ResultDownloadService) buildLocalDownloadZIP(task *model.Task, archive *localDownloadArchive, state *localDownloadBuild) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	err := s.writeLocalDownloadZIP(ctx, task, archive)
	state.mu.Lock()
	defer state.mu.Unlock()
	if err != nil {
		state.status, state.message = "failed", "本地结果打包失败，请确认文件可读、未被修改且缓存空间充足后重试"
	} else {
		state.status = "ready"
		localDownloadBuilds.Delete(archive.zipPath)
	}
}

func (s *ResultDownloadService) writeLocalDownloadZIP(ctx context.Context, task *model.Task, archive *localDownloadArchive) error {
	maxBytes := int64(s.cfg.Report.PackageMaxSizeMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 512 << 20
	}
	total := int64(len(archive.qc))
	for _, source := range archive.sources {
		if source.zipped {
			if source.file.SizeBytes > maxBytes-total {
				return fmt.Errorf("local archive size limit exceeded")
			}
			total += source.file.SizeBytes
		}
	}
	if total > maxBytes {
		return fmt.Errorf("local archive size limit exceeded")
	}
	if err := os.MkdirAll(filepath.Dir(archive.zipPath), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(archive.zipPath), ".results-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	dir, err := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
	if err != nil {
		return err
	}
	for _, source := range archive.sources {
		if !source.zipped {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		filename, err := resolveLocalDownloadSource(dir, s.cfg.ResultQuery.CacheDir, source)
		if err != nil {
			return err
		}
		reader, err := os.Open(filename)
		if err != nil {
			return err
		}
		method := uint16(zip.Deflate)
		if strings.HasSuffix(source.file.Filename, ".gz") || strings.HasSuffix(source.file.Filename, ".parquet") {
			method = zip.Store
		}
		writer, err := zw.CreateHeader(&zip.FileHeader{Name: source.file.Filename, Method: method})
		var n int64
		if err == nil {
			n, err = io.Copy(writer, localDownloadReader{ctx: ctx, reader: io.LimitReader(reader, source.file.SizeBytes+1)})
		}
		info, statErr := reader.Stat()
		reader.Close()
		if err != nil {
			return err
		}
		if statErr != nil || n != source.file.SizeBytes || info.Size() != source.file.SizeBytes || !info.ModTime().Equal(source.modified) {
			return fmt.Errorf("source changed during packaging")
		}
	}
	if len(archive.qc) > 0 {
		writer, err := zw.Create("qc-result.json")
		if err != nil {
			return err
		}
		if _, err := writer.Write(archive.qc); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	latest, err := s.collectLocalDownloads(task)
	if err != nil || latest.catalog.Version != archive.catalog.Version {
		return fmt.Errorf("archive changed during packaging")
	}
	return os.Rename(f.Name(), archive.zipPath)
}

type localDownloadReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r localDownloadReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// File IDs are resolved again from the current manifest, never as client paths.
func (s *ResultDownloadService) SelfHostedDownloadFile(ctx context.Context, task *model.Task, id, attempt, version string) (filename, name, redirect string, err error) {
	if s.cfg == nil || s.cfg.IsSaaS() {
		return "", "", "", ErrResultPackageUnsupported
	}
	if attempt != executionAttempt(task) {
		return "", "", "", ErrIGVEvidenceChanged
	}
	if !s.localDownloadStorage() {
		catalog, e := s.Catalog(ctx, task, false)
		if e != nil {
			return "", "", "", e
		}
		files := append([]ResultDownloadFile{}, catalog.BAMs...)
		if catalog.ZIP != nil {
			files = append(files, *catalog.ZIP)
		}
		for _, file := range files {
			if file.ID == id {
				store, e := newS3Storage(ctx, s.cfg.Storage)
				if e != nil {
					return "", "", "", e
				}
				link, e := store.presignDownload(ctx, file.key, file.Filename)
				return "", file.Filename, link, e
			}
		}
		return "", "", "", fmt.Errorf("归档文件不可用")
	}
	archive, e := s.collectLocalDownloads(task)
	if e != nil {
		return "", "", "", e
	}
	if version != archive.catalog.Version {
		return "", "", "", ErrIGVEvidenceChanged
	}
	if id == downloadID(archive.zipPath) {
		file, e := resolveArchiveRegularFile(filepath.Dir(archive.zipPath), archive.zipPath)
		return file, "task-" + task.UUID + "-results.zip", "", e
	}
	dir, e := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
	if e != nil {
		return "", "", "", e
	}
	for _, source := range archive.sources {
		if source.file.ID == id {
			file, e := resolveLocalDownloadSource(dir, s.cfg.ResultQuery.CacheDir, source)
			return file, source.file.Filename, "", e
		}
	}
	return "", "", "", fmt.Errorf("归档文件不可用")
}

func resolveLocalDownloadSource(archiveDir, cacheDir string, source localDownloadSource) (string, error) {
	root := archiveDir
	if source.generated {
		root = filepath.Join(cacheDir, "vcf-metadata")
	}
	return resolveArchiveRegularFile(root, source.filename)
}
