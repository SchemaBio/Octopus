package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVCFLifecycleCreateArchiveImportAndContext(t *testing.T) {
	dsn := os.Getenv("EXECUTION_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := "octopus_vcf_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err = root.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	old := database.DB
	database.DB = db
	defer func() {
		database.DB = old
		conn, _ := db.DB()
		conn.Close()
		root.Exec("DROP SCHEMA " + schema + " CASCADE")
		rootConn, _ := root.DB()
		rootConn.Close()
	}()
	if err = db.AutoMigrate(&model.User{}, &model.Sample{}, &model.Pipeline{}, &model.DataAsset{}, &model.Task{}, &model.TaskDataAsset{}, &model.SampleDataLink{}, &model.CNVBaseline{}, &model.WorkflowThresholdConfig{}, &model.QCResult{}, &model.SNVIndel{}, &model.CNVSegment{}, &model.CNVExon{}, &model.CNVAssessment{}, &model.STR{}, &model.MEIVariant{}, &model.MitochondrialVariant{}, &model.UPDRegion{}, &model.ROHRegion{}, &model.ResultImportBatch{}, &model.ResultDataset{}, &model.VariantReviewEvent{}, &model.HistoryScopeRevision{}, &model.HistoryFocus{}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := &config.Config{Edition: "opensource", Storage: config.StorageConfig{Provider: "local", LocalDir: dir}, Task: config.TaskConfig{TemplateDir: dir, OutputDir: filepath.Join(dir, "output"), ArchiveDir: filepath.Join(dir, "archive"), DefaultExecutor: "local"}, ResultQuery: config.ResultQueryConfig{CacheDir: filepath.Join(dir, "cache")}}
	os.WriteFile(filepath.Join(dir, "vcf2anno.wdl"), []byte("version 1.2\n"), 0600)
	source := filepath.Join(dir, "sample.vcf")
	os.WriteFile(source, []byte("##fileformat=VCFv4.2\n#CHROM\tPOS\tID\tREF\tALT\tQUAL\tFILTER\tINFO\tFORMAT\tS1\n1\t10\t.\tA\tC\t.\tLowQual\t.\tGT\t0/1\n"), 0600)
	sample := &model.Sample{UUID: uuid.NewString(), InternalID: "VCF-BIZ", Gender: model.SampleGenderUnknown, CreatedBy: 1}
	if err = db.Select("UUID", "InternalID", "Gender", "CreatedBy").Create(sample).Error; err != nil {
		t.Fatal(err)
	}
	asset := &model.DataAsset{UUID: uuid.NewString(), CreatedBy: 1, StorageKey: source, Provider: model.UploadProviderLocal, FileName: "sample.vcf", FileSize: 100, ReadType: model.ReadTypeVCF, Status: model.FileStatusCompleted}
	if err = db.Create(asset).Error; err != nil {
		t.Fatal(err)
	}
	actor := model.OverlayActor{UserID: 1, SharedInstance: true}
	service := NewTaskService(cfg)
	custom, err := NewPipelineService(cfg).CreatePipeline(context.Background(), &model.PipelineCreateRequest{Name: "Custom VCF", BasePipelineID: model.BuiltinPipelineVCFHG38ID}, actor)
	if err != nil || custom.Template != "vcf2anno" || custom.BEDAssetID != "" || custom.CNVBaselineID != "" {
		t.Fatalf("custom VCF %+v %v", custom, err)
	}
	for _, id := range []string{model.BuiltinPipelineVCFHG19ID, model.BuiltinPipelineVCFHG38ID, custom.ID} {
		t.Run(id, func(t *testing.T) {
			request := &model.TaskCreateRequest{SampleID: sample.UUID, PipelineID: id, VCFAssetID: asset.UUID, VCFSampleName: "S1", DeferStart: true}
			estimate, err := service.EstimateTask(&model.TaskEstimateRequest{SampleID: sample.UUID, PipelineID: id, VCFAssetID: asset.UUID}, actor)
			if err != nil || estimate.EstimatedMinutes != 25 {
				t.Fatalf("estimate %+v %v", estimate, err)
			}
			preview := service.PreviewTaskBatch([]model.TaskBatchInputRow{{RowNumber: 2, SampleIdentifier: sample.UUID, PipelineID: id, VCFAssetID: asset.UUID}}, actor)
			if preview.ValidRows != 1 || preview.TotalEstimatedMinutes != 25 || preview.Rows[0].EstimatedMinutes != 25 {
				t.Fatalf("VCF batch estimate %+v", preview)
			}
			task, err := service.CreateTask(context.Background(), request, actor)
			if err != nil {
				t.Fatal(err)
			}
			if task.Status != model.TaskStatusQueued || task.EstimatedMinutes != 25 {
				t.Fatalf("task %+v", task)
			}
			if ok, reason := service.checkDataReady(task); !ok {
				t.Fatal(reason)
			}
			var used *ResourceInUseError
			err = checkBEDReferences(db, asset)
			if !errors.As(err, &used) {
				t.Fatalf("active VCF reference was not protected: %v", err)
			}
			saved, err := NewWorkflowConfigService().Update("vcf2anno", "hg19", model.WorkflowThresholdUpdateRequest{SexMinSites: 100, FemaleMaxRatio: 0, MaleMinRatio: 0.7}, 1)
			if err != nil || saved.FemaleMaxRatio != 0 {
				t.Fatalf("zero female ratio not preserved: %+v %v", saved, err)
			}
			archive := filepath.Join(cfg.Task.ArchiveDir, task.UUID)
			os.MkdirAll(archive, 0700)
			manifest := map[string]interface{}{"VCF2Anno.snp_indel": "sample.snv_indel.txt"}
			for _, field := range []string{"preprocessing", "sex_inference", "summary"} {
				name := field + ".json"
				manifest["VCF2Anno."+field] = name
				os.WriteFile(filepath.Join(archive, name), []byte(`{"sex":"unknown","x_sum":0}`), 0600)
			}
			raw, _ := json.Marshal(manifest)
			os.WriteFile(filepath.Join(archive, "outputs.resolved.json"), raw, 0600)
			os.WriteFile(filepath.Join(archive, "sample.snv_indel.txt"), []byte("Chromosome\tPosition\tRef\tAlt\tType\tQuality\tFilter\tGenotype\tZygosity\tGene\n1\t10\tA\tC\tSNP\t.\tLowQual\t0/1\tHet\tTEST\n"), 0600)
			result, err := NewImporter(cfg).ImportFromTaskArchive(task, archive)
			if err != nil || !result.Success || result.Counts["snv_indel"] != 1 {
				t.Fatalf("import %+v %v", result, err)
			}
			task.Status = model.TaskStatusCompleted
			task.ResultImportStatus = model.ResultImportStatusSuccess
			db.Save(task)
			context, err := NewResultService(cfg).GetContext(context.Background(), task)
			if err != nil {
				t.Fatal(err)
			}
			if len(context.QC) != 0 || len(context.Members) != 1 || context.Members[0].SampleID != sample.UUID || context.VCFAnnotation == nil {
				t.Fatalf("bad result context %+v", context)
			}
			var rowCount int64
			db.Model(&model.SNVIndel{}).Where("task_id=?", task.UUID).Count(&rowCount)
			if rowCount != 1 {
				t.Fatal("nonempty VCF not imported")
			}
			os.WriteFile(filepath.Join(archive, "sample.snv_indel.txt"), []byte("Chromosome\tPosition\tRef\tAlt\tType\tQuality\tFilter\tGenotype\tZygosity\tGene\n"), 0600)
			empty, err := NewImporter(cfg).ImportFromTaskArchive(task, archive)
			if err != nil || !empty.Success || empty.Counts["snv_indel"] != 0 {
				t.Fatalf("empty VCF %+v %v", empty, err)
			}
			db.Model(&model.SNVIndel{}).Where("task_id=?", task.UUID).Count(&rowCount)
			if rowCount != 0 {
				t.Fatal("re-import retained old variants")
			}
			var after model.Sample
			db.First(&after, sample.ID)
			if after.Gender != model.SampleGenderUnknown {
				t.Fatal("VCF inference overwrote business gender")
			}
		})
	}
}
