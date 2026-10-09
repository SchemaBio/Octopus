package service

import (
	"bytes"
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"
)

func TestSnapshotValidation(t *testing.T) {
	for _, invalid := range []string{"chr1:0-10", "chr1:20-10", "../../other:1-2", "chr1:1-6000000", "chr23:1-10"} {
		if _, err := normalizeSnapshotLocus(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	locus, err := normalizeSnapshotLocus("MT:0001-20")
	if err != nil || locus != "chrM:1-20" {
		t.Fatalf("normalization: %s %v", locus, err)
	}
	var valid bytes.Buffer
	if err := png.Encode(&valid, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	if err := validateSnapshotPNG(valid.Bytes()); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{nil, []byte("<svg/>"), valid.Bytes()[:40], make([]byte, MaxIGVSnapshotBytes+1)} {
		if validateSnapshotPNG(invalid) == nil {
			t.Fatal("accepted invalid image")
		}
	}
}

func TestSnapshotIdentitySeparatesTasksAndAttempts(t *testing.T) {
	task := model.Task{UUID: "task-a", ExternalOrgID: "org-a", ExecutionAttemptID: "attempt-a"}
	original := snapshotIdentity(&task, "hg19", "chr1:1-10")
	other := task
	other.ExecutionAttemptID = "attempt-b"
	if original == snapshotIdentity(&other, "hg19", "chr1:1-10") {
		t.Fatal("attempts collided")
	}
	other = task
	other.UUID = "task-b"
	if original == snapshotIdentity(&other, "hg19", "chr1:1-10") {
		t.Fatal("tasks collided")
	}
	if original == snapshotIdentity(&task, "hg38", "chr1:1-10") || original == snapshotIdentity(&task, "hg19", "chr1:2-11") {
		t.Fatal("references or intervals collided")
	}
}

func TestSnapshotsDoNotChangeEvidenceVersion(t *testing.T) {
	objects := []s3ObjectInfo{{Key: "prefix/outputs.resolved.json", Size: 10, LastModified: time.Unix(1, 0)}, {Key: "prefix/reads.bam", Size: 100, LastModified: time.Unix(1, 0)}}
	_, _, before, err := igvArchiveObjectIndex("prefix", objects)
	if err != nil {
		t.Fatal(err)
	}
	objects = append(objects, s3ObjectInfo{Key: "prefix/_igv-snapshots/image.png", Size: 50, LastModified: time.Now()})
	_, indexed, after, err := igvArchiveObjectIndex("prefix", objects)
	if err != nil || before != after {
		t.Fatalf("screenshot invalidated evidence: %v", err)
	}
	if _, ok := indexed["image.png"]; ok {
		t.Fatal("screenshot indexed as raw evidence")
	}
}

func TestSnapshotReadDoesNotRequireBAMOrManifest(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	previous := database.DB
	database.DB = db
	defer func() { database.DB = previous }()
	task := &model.Task{UUID: "11111111-1111-4111-8111-111111111111", ExternalOrgID: "22222222-2222-4222-8222-222222222222", ExecutionAttemptID: "33333333-3333-4333-8333-333333333333", InputJSON: `{"reference_genome":"hg19"}`}
	svc := NewResultService(&config.Config{Storage: config.StorageConfig{Provider: "s3", S3Endpoint: "https://storage.invalid", S3Region: "test", S3UsePathStyle: true, S3AccessKey: "test", S3SecretKey: "test", S3Bucket: "evidence"}})
	locus := "chr1:1-110"
	key := resultPackagePrefix(task) + "/_igv-snapshots/saved.png"
	mock.ExpectQuery(`SELECT .*igv_snapshots`).WithArgs(snapshotIdentity(task, "hg19", locus), model.TenantIDForTask(task), task.UUID, task.ExecutionAttemptID, 1).WillReturnRows(sqlmock.NewRows([]string{"id", "locus", "reference", "object_key", "created_at"}).AddRow("saved", locus, "hg19", key, time.Now()))
	result, err := svc.GetIGVSnapshot(context.Background(), task, locus)
	if err != nil || !result.Available || !strings.Contains(result.URL, "saved.png") {
		t.Fatalf("snapshot unavailable without BAM: %+v %v", result, err)
	}
	mock.ExpectQuery(`SELECT .*igv_snapshots`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	result, err = svc.GetIGVSnapshot(context.Background(), task, "chr1:2-111")
	if err != nil || result.Available {
		t.Fatalf("missing screenshot result: %+v %v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
