package service

import (
	"path/filepath"
	"testing"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
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
