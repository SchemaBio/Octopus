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

func TestLocalMiniWDLInputsUsesUploadedReads(t *testing.T) {
	stored := map[string]interface{}{
		"reference_genome":           "hg19",
		"fastq_r1":                   "/mnt/e/SchemaBio/uploads/DEMO_R1.fastq.gz",
		"fastq_r2":                   "/mnt/e/SchemaBio/uploads/DEMO_R2.fastq.gz",
		"bed_file":                   "/mnt/data/inputs/panel.bed",
		"cnv_baseline":               "/mnt/data/inputs/reference.cnn",
		"SingleWES.read_1":           "/mnt/data/test/202511_proband_ILMN_1.fq.gz",
		"SingleWES.read_2":           "/mnt/data/test/202511_proband_ILMN_2.fq.gz",
		"SingleWES.prefix":           "662ccaac-72fb-4889-8214-b1018012711c",
		"SingleWES.fasta":            "/mnt/data/database/Homo_sapiens.GRCh37.dna.primary_assembly.fa",
		"SingleWES.cnv_bin_size":     float64(20000),
		"SingleWES.cnvkit_reference": "/mnt/data/database/schema_bundle/hg19_reference.cnvkit.cnn",
	}

	got, err := localMiniWDLInputs("single", stored)
	if err != nil {
		t.Fatal(err)
	}
	if got["SingleWES.read_1"] != stored["fastq_r1"] || got["SingleWES.read_2"] != stored["fastq_r2"] {
		t.Fatalf("uploaded reads were not applied: %#v", got)
	}
	if got["SingleWES.bed"] != stored["bed_file"] || got["SingleWES.cnvkit_reference"] != stored["cnv_baseline"] {
		t.Fatalf("auxiliary inputs were not applied: %#v", got)
	}
	if got["SingleWES.prefix"] != stored["SingleWES.prefix"] || got["SingleWES.cnv_bin_size"] != float64(20000) {
		t.Fatalf("existing workflow inputs were not preserved: %#v", got)
	}
	for _, key := range []string{"fastq_r1", "fastq_r2", "reference_genome", "bed_file", "cnv_baseline"} {
		if _, ok := got[key]; ok {
			t.Fatalf("non-WDL key %s leaked into miniwdl inputs", key)
		}
	}
}

func TestLocalMiniWDLInputsDropsTrioMetadata(t *testing.T) {
	stored := map[string]interface{}{
		"reference_genome":    "hg19",
		"TrioWES.prefix":      "family",
		"TrioWES.ped":         "/mnt/data/output/family.ped",
		"TrioWES.ped_content": "family\tproband\tfather\tmother\t1\t2\n",
		"trio_members":        []interface{}{"proband"},
		"trio_input_bytes":    float64(10),
		"fastq_r1":            "/should/not/appear",
	}

	got, err := localMiniWDLInputs("trio", stored)
	if err != nil {
		t.Fatal(err)
	}
	if got["TrioWES.ped"] != stored["TrioWES.ped"] || got["TrioWES.prefix"] != "family" {
		t.Fatalf("trio workflow inputs were not preserved: %#v", got)
	}
	for _, key := range []string{"fastq_r1", "reference_genome", "trio_members", "trio_input_bytes", "TrioWES.ped_content"} {
		if _, ok := got[key]; ok {
			t.Fatalf("non-WDL key %s leaked into miniwdl inputs", key)
		}
	}
}

func TestLocalMiniWDLInputsRejectsUnknownTemplate(t *testing.T) {
	stored := map[string]interface{}{"reference_genome": "hg19", "fastq_r1": "/uploads/r1.fq.gz", "Custom.input": "keep"}
	got, err := localMiniWDLInputs("custom", stored)
	if err == nil || got != nil {
		t.Fatalf("unsupported template must fail without executable inputs: got=%#v err=%v", got, err)
	}
	if stored["fastq_r1"] != "/uploads/r1.fq.gz" || stored["Custom.input"] != "keep" {
		t.Fatal("failed projection mutated stored inputs")
	}
}

func TestTaskOutputPrefixUsesFullWorkflowUUID(t *testing.T) {
	workflowUUID := "8da4d946-8af2-41e2-a8e8-df51ec4f6ffd"
	if got := taskOutputPrefix(workflowUUID); got != workflowUUID {
		t.Fatalf("taskOutputPrefix() = %q, want full UUID %q", got, workflowUUID)
	}
}

func TestLocalMiniWDLInputsInvalidGenomeDoesNotFallBackToOldReads(t *testing.T) {
	stored := map[string]interface{}{
		"reference_genome": "hg20",
		"fastq_r1":         "/uploads/actual-r1.fq.gz",
		"fastq_r2":         "/uploads/actual-r2.fq.gz",
		"SingleWES.read_1": "/mnt/data/test/old-r1.fq.gz",
		"SingleWES.read_2": "/mnt/data/test/old-r2.fq.gz",
	}
	got, err := localMiniWDLInputs("single", stored)
	if err == nil || got != nil {
		t.Fatalf("invalid genome must fail without executable inputs: got=%#v err=%v", got, err)
	}
	if stored["fastq_r1"] != "/uploads/actual-r1.fq.gz" || stored["reference_genome"] != "hg20" {
		t.Fatal("failed projection mutated stored inputs")
	}
}
