package service

import (
	"path/filepath"
	"testing"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func TestMiniWDLCommandUsesAbsoluteConfiguredOutputDirectory(t *testing.T) {
	outputDir := t.TempDir()
	templateDir := t.TempDir()
	service := &TaskService{cfg: &config.Config{Task: config.TaskConfig{
		OutputDir:   outputDir,
		TemplateDir: templateDir,
		MiniWDLPath: "miniwdl",
	}}}
	task := &model.Task{
		UUID:       "11111111-1111-4111-8111-111111111111",
		Template:   "single",
		Executor:   model.ExecutorLocal,
		ConfigFile: filepath.Join(templateDir, "conf", "local.cfg"),
	}

	cmd := service.miniWDLCommand(task, filepath.Join(outputDir, "inputs.json"))
	want := filepath.Join(outputDir, task.UUID)
	if !filepath.IsAbs(want) {
		t.Fatalf("test output directory is not absolute: %q", want)
	}
	if got := cmd.Args[len(cmd.Args)-1]; got != want {
		t.Fatalf("miniwdl -d argument = %q, want %q", got, want)
	}
}
