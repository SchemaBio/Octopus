package service

import (
	"context"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/exampledata"
	"github.com/SchemaBio/Octopus/internal/model"
	"strings"
	"testing"
)

func TestExampleDataIsSaaSOnly(t *testing.T) {
	cfg := &config.Config{Edition: "opensource"}
	if exampleAvailable(cfg) {
		t.Fatal("self-hosted example enabled")
	}
	// All these calls must return before touching a database or storage.
	if err := MigrateExampleData(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := NewExampleDataService(cfg).Enqueue(context.Background(), model.ExampleDataGrant{}); err == nil {
		t.Fatal("self-hosted grant accepted")
	}
	if err := PublishExampleTemplate(context.Background(), cfg, "", exampledata.Manifest{}, true); err == nil {
		t.Fatal("self-hosted publish accepted")
	}
	if _, err := ExportExampleTemplate(context.Background(), cfg, "", "", "", ""); err == nil {
		t.Fatal("self-hosted export accepted")
	}
	NewExampleDataService(cfg).Start(context.Background())
}
func TestExampleObjectsCannotShareAnotherOrganizationOrTask(t *testing.T) {
	g := &model.ExampleDataGrant{ID: "grant", OrgID: "org-a", TaskUUID: "task-a", AttemptUUID: "attempt-a"}
	read := exampleObjectKey(g, exampledata.Object{Path: "data/read.fastq.gz", Role: exampledata.Read1})
	bam := exampleObjectKey(g, exampledata.Object{Path: "results/read.bam", Role: exampledata.BAM})
	if !strings.HasPrefix(read, "organizations/org-a/uploads/fastq/paired/grant/") || !strings.HasPrefix(bam, "organizations/org-a/workflows/task-a/attempts/attempt-a/") {
		t.Fatal("unexpected scopes")
	}
	other := *g
	other.OrgID = "org-b"
	if read == exampleObjectKey(&other, exampledata.Object{Path: "data/read.fastq.gz", Role: exampledata.Read1}) {
		t.Fatal("shared object path")
	}
}
