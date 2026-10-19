package service

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVCFGenomeResourcesAndThresholds(t *testing.T) {
	for _, genome := range []string{"hg19", "hg38"} {
		id := model.BuiltinPipelineVCFHG19ID
		if genome == "hg38" {
			id = model.BuiltinPipelineVCFHG38ID
		}
		req := &model.TaskCreateRequest{PipelineID: id, Inputs: map[string]interface{}{"reference_genome": "incorrect"}}
		if _, err := (&TaskService{}).resolveAnalysisPipeline(req, model.OverlayActor{}); err != nil {
			t.Fatal(err)
		}
		if req.Template != "vcf2anno" || req.Inputs["reference_genome"] != genome {
			t.Fatal(req)
		}
		inputs, err := buildCVMWDLInputs(req.Template, genome, map[string]interface{}{"VCF2Anno.vcf": "selected.vcf.gz", "VCF2Anno.sample_name": "S2"})
		if err != nil {
			t.Fatal(err)
		}
		if inputs["VCF2Anno.vcf"] != "selected.vcf.gz" || inputs["VCF2Anno.sample_name"] != "S2" {
			t.Fatal(inputs)
		}
		assembly := "GRCh37"
		if genome == "hg38" {
			assembly = "GRCh38"
		}
		if inputs["VCF2Anno.assembly"] != assembly || !strings.Contains(inputs["VCF2Anno.fasta"].(string), assembly) {
			t.Fatal(inputs)
		}
		thresholds, err := defaultThresholdConfig(req.Template, genome)
		if err != nil {
			t.Fatal(err)
		}
		applyWorkflowThresholds(inputs, thresholds)
		if thresholds.SexMinSites != 100 || thresholds.FemaleMaxRatio != 0.5 || thresholds.MaleMinRatio != 0.7 {
			t.Fatal(thresholds)
		}
		for key := range inputs {
			if strings.Contains(key, "bed") || strings.Contains(key, "cnv") || strings.Contains(key, "read_") {
				t.Fatal(key)
			}
		}
	}
}
func TestVCFAssetSelectionRejectsCrossTenantExpiredAndIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name, org, status, kind string
		expired                 bool
		ok                      bool
	}{
		{"valid", "tenant", "completed", "vcf", false, true}, {"foreign", "other", "completed", "vcf", false, false}, {"expired", "tenant", "completed", "vcf", true, false}, {"uploading", "tenant", "uploading", "vcf", false, false}, {"FASTQ", "tenant", "completed", "read1", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newUploadTransactionTestDB(t)
			prior := database.DB
			database.DB = db
			defer func() { database.DB = prior }()
			expires := time.Now().Add(time.Hour)
			if tc.expired {
				expires = time.Now().Add(-time.Second)
			}
			mock.ExpectQuery(`SELECT .*data_assets.*uuid`).WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "external_org_id", "status", "read_type", "file_size", "expires_at"}).AddRow(1, "asset", tc.org, tc.status, tc.kind, 100, expires))
			service := NewTaskService(&config.Config{})
			_, err := service.resolveVCFAsset(&model.TaskCreateRequest{SampleID: "sample", VCFAssetID: "asset"}, model.OverlayActor{OrgID: "tenant", UserID: 1})
			if (err == nil) != tc.ok {
				t.Fatalf("selection error: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestVCFDownloadRoleWithoutFASTQ(t *testing.T) {
	storage := &fakeCVMInputStorage{sizes: map[string]int64{"organizations/org/variants": 100}}
	downloads, err := prepareCVMInputDownloads(context.Background(), storage, []*model.DataAsset{{ID: 1, UUID: "asset", ReadType: model.ReadTypeVCF, StorageKey: "organizations/org/variants", FileName: "sample.vcf.gz", FileSize: 100}}, nil, time.Hour)
	if err != nil || len(downloads) != 1 || downloads[0].ValidationRole != "vcf" || downloads[0].PairKey != "" {
		t.Fatalf("%+v %v", downloads, err)
	}
	if err := validateCVMDownloadPairs(append(downloads, downloads[0])); err == nil {
		t.Fatal("duplicate VCF accepted")
	}
	inputs := map[string]interface{}{}
	applyTaskAssetPath(inputs, model.TaskDataAsset{InputRole: model.TaskAssetRoleVCF}, "localized.vcf.gz")
	if inputs["VCF2Anno.vcf"] != "localized.vcf.gz" {
		t.Fatal(inputs)
	}
}
func TestLocalVCFImportPlainCompressedAndHeaderOnly(t *testing.T) {
	valid := "##fileformat=VCFv4.2\n#CHROM\tPOS\tID\tREF\tALT\tQUAL\tFILTER\tINFO\tFORMAT\tS1\n"
	for _, ext := range []string{".vcf", ".vcf.gz", ".vcf.bgz"} {
		t.Run(ext, func(t *testing.T) {
			root := t.TempDir()
			name := filepath.Join(root, "sample"+ext)
			if ext == ".vcf" {
				if err := os.WriteFile(name, []byte(valid), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				f, err := os.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				z := gzip.NewWriter(f)
				z.Write([]byte(valid))
				z.Close()
				f.Close()
			}
			assets, err := validateLocalVCF(context.Background(), root, name)
			if err != nil {
				t.Fatal(err)
			}
			if len(assets) != 1 || assets[0].ReadType != model.ReadTypeVCF || assets[0].ValidationSHA256 == "" {
				t.Fatal(assets)
			}
		})
	}
	root := t.TempDir()
	name := filepath.Join(root, "invalid.vcf")
	os.WriteFile(name, []byte(valid+"1\t2\t.\tA\tC\t.\tPASS\t.\tDP\t10\n"), 0600)
	if _, err := validateLocalVCF(context.Background(), root, name); err == nil {
		t.Fatal("missing GT accepted")
	}
	if _, err := validateLocalVCF(context.Background(), t.TempDir(), name); err == nil {
		t.Fatal("path outside storage accepted")
	}
}
func TestVCFArchiveMetadataDownloadsAndIGVWithoutQC(t *testing.T) {
	root := t.TempDir()
	task := &model.Task{UUID: "vcf-task", Template: "vcf2anno", Executor: model.ExecutorLocal, Status: model.TaskStatusCompleted, ResultImportStatus: model.ResultImportStatusSuccess}
	dir := filepath.Join(root, task.UUID)
	os.MkdirAll(dir, 0700)
	manifest := map[string]interface{}{}
	for _, field := range []string{"normalized_vcf", "normalized_vcf_tbi", "annotated_vcf", "annotated_vcf_tbi", "snp_indel", "preprocessing", "sex_inference", "sex_explanation", "summary"} {
		name := field + ".json"
		data := []byte(`{}`)
		switch field {
		case "normalized_vcf":
			name = "sample.normalized.vcf.gz"
		case "normalized_vcf_tbi":
			name = "sample.normalized.vcf.gz.tbi"
		case "annotated_vcf":
			name = "sample.vep.vcf.gz"
		case "annotated_vcf_tbi":
			name = "sample.vep.vcf.gz.tbi"
		case "snp_indel":
			name = "sample.snv_indel.txt"
			data = []byte("Chr\tPosition\tRef\tAlt\n")
		case "sex_explanation":
			name = "sample.sex_inference.txt"
			data = []byte("Sample: S1\nInferred sex: unknown\n")
		case "sex_inference":
			data = []byte(`{"x_sum":0,"sex":"unknown","ratio":null}`)
		}
		os.WriteFile(filepath.Join(dir, name), data, 0600)
		manifest["VCF2Anno."+field] = "/worker/output/" + name
	}
	raw, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "outputs.resolved.json"), raw, 0600)
	cfg := &config.Config{Edition: "opensource", Task: config.TaskConfig{ArchiveDir: root}, Storage: config.StorageConfig{Provider: "local"}, ResultQuery: config.ResultQueryConfig{CacheDir: t.TempDir()}}
	info, err := loadVCFAnnotationInfo(cfg, task)
	if err != nil {
		t.Fatal(err)
	}
	if info["sex_inference"].(map[string]interface{})["sex"] != "unknown" {
		t.Fatal(info)
	}
	archive, err := (&ResultDownloadService{cfg: cfg}).collectLocalDownloads(task)
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.catalog.Files) != 9 || len(archive.catalog.BAMs) != 0 || len(archive.catalog.Missing) != 0 || len(archive.qc) != 0 {
		t.Fatalf("bad VCF catalog %+v", archive.catalog)
	}
	objects := map[string]s3ObjectInfo{"sample.normalized.vcf.gz": {Key: "sample.normalized.vcf.gz"}, "sample.normalized.vcf.gz.tbi": {Key: "sample.normalized.vcf.gz.tbi"}}
	tracks := igvCandidatesFromManifest(manifest, objects)
	if len(tracks) != 1 || !tracks[0].descriptor.Available || !tracks[0].descriptor.HasIndex {
		t.Fatalf("VCF track %+v", tracks)
	}
	// Match the real Agent: JSON is inlined and TXT references become Parquet,
	// while original TXT files remain available alongside their conversions.
	for _, field := range []string{"preprocessing", "sex_inference", "summary"} {
		manifest["VCF2Anno."+field] = info[field]
		os.Remove(filepath.Join(dir, field+".json"))
	}
	manifest["VCF2Anno.snp_indel"] = "sample.snv_indel.parquet"
	manifest["VCF2Anno.sex_explanation"] = "sample.sex_inference.parquet"
	raw, _ = json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "outputs.resolved.json"), raw, 0600)
	names, err := vcfArchiveMetadataNames(raw)
	if err != nil || len(names) != 0 {
		t.Fatalf("inline staging %+v %v", names, err)
	}
	embedded, err := loadVCFAnnotationInfo(cfg, task)
	if err != nil || embedded["sex_inference"].(map[string]interface{})["sex"] != "unknown" {
		t.Fatalf("inline metadata %+v %v", embedded, err)
	}
	inline, err := (&ResultDownloadService{cfg: cfg}).collectLocalDownloads(task)
	if err != nil {
		t.Fatal(err)
	}
	if len(inline.catalog.Files) != 9 || len(inline.catalog.Missing) != 0 {
		t.Fatalf("inline files %+v", inline.catalog)
	}
	if err := (&ResultDownloadService{cfg: cfg}).writeLocalDownloadZIP(context.Background(), task, inline); err != nil {
		t.Fatal(err)
	}
	zipReader, err := zip.OpenReader(inline.zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zipReader.Close()
	if len(zipReader.File) != 9 {
		t.Fatalf("ZIP includes %d files", len(zipReader.File))
	}
	for _, source := range inline.sources {
		if _, err := resolveLocalDownloadSource(dir, cfg.ResultQuery.CacheDir, source); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range inline.catalog.Files {
		if strings.HasSuffix(file.Filename, ".parquet") {
			t.Fatalf("report text was not retained: %s", file.Filename)
		}
	}
	if len(task.ToDetailResponse().ResultCapabilities) != 3 {
		t.Fatal("missing trusted capabilities")
	}
}
