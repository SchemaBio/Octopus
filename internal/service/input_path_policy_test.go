package service

import (
	"path/filepath"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestValidateActorTaskFileInputsRejectsArbitraryLocalPath(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{LocalDir: filepath.Join(t.TempDir(), "uploads")},
		Task:    config.TaskConfig{TemplateDir: filepath.Join(t.TempDir(), "templates")},
	}
	actor := model.OverlayActor{UserID: 1, Role: string(model.SystemRoleUser)}

	err := validateActorTaskFileInputs(cfg, actor, map[string]interface{}{
		"fastq_r1": filepath.Join(t.TempDir(), "secret.fastq.gz"),
	})
	if err == nil {
		t.Fatal("expected arbitrary local file input to be rejected for non-admin actor")
	}
}

func TestValidateActorTaskFileInputsAllowsUploadDir(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "uploads")
	cfg := &config.Config{
		Storage: config.StorageConfig{LocalDir: uploadDir},
		Task:    config.TaskConfig{TemplateDir: filepath.Join(t.TempDir(), "templates")},
	}
	actor := model.OverlayActor{UserID: 1, Role: string(model.SystemRoleUser)}

	err := validateActorTaskFileInputs(cfg, actor, map[string]interface{}{
		"fastq_r1": filepath.Join(uploadDir, "user", "sample_R1.fastq.gz"),
	})
	if err != nil {
		t.Fatalf("expected upload-dir input to be allowed: %v", err)
	}
}

func TestValidateActorTaskFileInputsAllowsSuperAdminPath(t *testing.T) {
	cfg := &config.Config{}
	actor := model.OverlayActor{UserID: 1, Role: string(model.SystemRoleSuperAdmin)}

	if err := validateActorTaskFileInputs(cfg, actor, map[string]interface{}{"bed_file": filepath.Join(t.TempDir(), "panel.bed")}); err != nil {
		t.Fatalf("expected super admin arbitrary path to be allowed: %v", err)
	}
}

func TestValidateActorTaskFileInputsAllowsCNVBaselineMetadata(t *testing.T) {
	uploadDir := t.TempDir()
	cfg := &config.Config{Storage: config.StorageConfig{LocalDir: uploadDir}}
	actor := model.OverlayActor{UserID: 1, Role: string(model.SystemRoleUser)}
	for _, genome := range []string{"hg19", "hg38"} {
		err := validateActorTaskFileInputs(cfg, actor, map[string]interface{}{
			"CNVBaselineFix.assembly":              genome,
			"reference_genome":                     genome,
			"CNVBaselineFix.prefix":                "8da4d946",
			"CNVBaselineFix.existing_sample_count": 20,
			"CNVBaselineFix.bed":                   filepath.Join(uploadDir, "panel.bed"),
			"CNVBaselineFix.read_1":                []string{filepath.Join(uploadDir, "sample_R1.fastq.gz")},
			"CNVBaselineFix.read_2":                []interface{}{filepath.Join(uploadDir, "sample_R2.fastq.gz")},
		})
		if err != nil {
			t.Fatalf("valid %s baseline inputs rejected: %v", genome, err)
		}
	}
}

func TestValidateActorTaskFileInputsStillRejectsCNVBaselineExternalFiles(t *testing.T) {
	cfg := &config.Config{Storage: config.StorageConfig{LocalDir: t.TempDir()}}
	actor := model.OverlayActor{UserID: 1, Role: string(model.SystemRoleUser)}
	for _, key := range []string{"bed", "read_1", "read_2", "fasta", "existing_reference", "ref_dir"} {
		t.Run(key, func(t *testing.T) {
			value := filepath.Join(t.TempDir(), "outside-upload")
			if err := validateActorTaskFileInputs(cfg, actor, map[string]interface{}{"CNVBaselineFix." + key: value}); err == nil {
				t.Fatal("expected out-of-scope file path to be rejected")
			}
		})
	}
}
