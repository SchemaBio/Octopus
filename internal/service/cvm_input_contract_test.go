package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

type fakeCVMInputStorage struct {
	sizes        map[string]int64
	statErrors   map[string]error
	statCalls    int
	presignCalls int
}

func (f *fakeCVMInputStorage) stat(_ context.Context, key string) (int64, error) {
	f.statCalls++
	if err := f.statErrors[key]; err != nil {
		return 0, err
	}
	return f.sizes[key], nil
}

func (f *fakeCVMInputStorage) presignDownloadWithExpiry(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	f.presignCalls++
	return "https://objects.example/" + key + "?signed=redacted", nil
}

func TestPrepareCVMInputDownloadsAssignsV2RolesAndPairs(t *testing.T) {
	tests := []struct {
		name      string
		assets    []*model.DataAsset
		links     map[uint][]model.TaskDataAsset
		wantRoles []string
		wantPairs []string
	}{
		{
			name: "single",
			assets: []*model.DataAsset{
				{ID: 1, UUID: "r1", FileName: "r1.fq.gz", StorageKey: "organizations/o/r1", FileSize: 10, ReadType: model.ReadTypeRead1},
				{ID: 2, UUID: "r2", FileName: "r2.fq.gz", StorageKey: "organizations/o/r2", FileSize: 11, ReadType: model.ReadTypeRead2},
			},
			wantRoles: []string{"fastq_r1", "fastq_r2"}, wantPairs: []string{"single:0", "single:0"},
		},
		{
			name: "trio members",
			assets: []*model.DataAsset{
				{ID: 1, UUID: "r1", FileName: "r1.fq.gz", StorageKey: "organizations/o/r1", FileSize: 10, ReadType: model.ReadTypeRead1},
				{ID: 2, UUID: "r2", FileName: "r2.fq.gz", StorageKey: "organizations/o/r2", FileSize: 11, ReadType: model.ReadTypeRead2},
			},
			links: map[uint][]model.TaskDataAsset{
				1: {{InputRole: model.TaskAssetRoleTrioRead1, InputIndex: 1}},
				2: {{InputRole: model.TaskAssetRoleTrioRead2, InputIndex: 1}},
			},
			wantRoles: []string{"fastq_r1", "fastq_r2"}, wantPairs: []string{"trio:1", "trio:1"},
		},
		{
			name: "baseline pair and bed",
			assets: []*model.DataAsset{
				{ID: 1, UUID: "r1", FileName: "r1.fq.gz", StorageKey: "organizations/o/r1", FileSize: 10, ReadType: model.ReadTypeRead1},
				{ID: 2, UUID: "r2", FileName: "r2.fq.gz", StorageKey: "organizations/o/r2", FileSize: 11, ReadType: model.ReadTypeRead2},
				{ID: 3, UUID: "bed", FileName: "panel.bed", StorageKey: "organizations/o/bed", FileSize: 12, ReadType: model.ReadTypeBed},
			},
			links: map[uint][]model.TaskDataAsset{
				1: {{InputRole: model.TaskAssetRoleCNVRead1, InputIndex: 0}},
				2: {{InputRole: model.TaskAssetRoleCNVRead2, InputIndex: 0}},
				3: {{InputRole: model.TaskAssetRoleCNVBED}},
			},
			wantRoles: []string{"fastq_r1", "fastq_r2", "bed"}, wantPairs: []string{"cnv_baseline:0", "cnv_baseline:0", ""},
		},
		{
			name: "custom analysis bed",
			assets: []*model.DataAsset{
				{ID: 1, UUID: "r1", FileName: "r1.fq.gz", StorageKey: "organizations/o/r1", FileSize: 10, ReadType: model.ReadTypeRead1},
				{ID: 2, UUID: "r2", FileName: "r2.fq.gz", StorageKey: "organizations/o/r2", FileSize: 11, ReadType: model.ReadTypeRead2},
				{ID: 3, UUID: "bed", FileName: "panel.bed", StorageKey: "organizations/o/bed", FileSize: 12, ReadType: model.ReadTypeBed},
			},
			links:     map[uint][]model.TaskDataAsset{3: {{InputRole: model.TaskAssetRoleAnalysisBED}}},
			wantRoles: []string{"fastq_r1", "fastq_r2", "bed"}, wantPairs: []string{"single:0", "single:0", ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			storage := &fakeCVMInputStorage{sizes: map[string]int64{}}
			for _, asset := range tc.assets {
				storage.sizes[asset.StorageKey] = asset.FileSize
			}
			downloads, err := prepareCVMInputDownloads(context.Background(), storage, tc.assets, tc.links, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if len(downloads) != len(tc.assets) {
				t.Fatalf("got %d downloads, want %d", len(downloads), len(tc.assets))
			}
			for i := range downloads {
				if downloads[i].ValidationRole != tc.wantRoles[i] || downloads[i].PairKey != tc.wantPairs[i] {
					t.Errorf("download %d role/pair = %q/%q, want %q/%q", i, downloads[i].ValidationRole, downloads[i].PairKey, tc.wantRoles[i], tc.wantPairs[i])
				}
				if downloads[i].ExpectedSizeBytes != tc.assets[i].FileSize || downloads[i].URL == "" {
					t.Errorf("download %d is missing confirmed size or signed URL", i)
				}
			}
			if storage.statCalls != len(tc.assets) || storage.presignCalls != len(tc.assets) {
				t.Fatalf("HEAD/presign calls = %d/%d, want %d/%d", storage.statCalls, storage.presignCalls, len(tc.assets), len(tc.assets))
			}
		})
	}
}

func TestPrepareCVMInputDownloadsFailsBeforePresignOnHeadProblems(t *testing.T) {
	assets := []*model.DataAsset{
		{ID: 1, UUID: "r1", FileName: "r1.fq.gz", StorageKey: "organizations/o/r1", FileSize: 10, ReadType: model.ReadTypeRead1},
		{ID: 2, UUID: "r2", FileName: "r2.fq.gz", StorageKey: "organizations/o/r2", FileSize: 11, ReadType: model.ReadTypeRead2},
	}
	for _, tc := range []struct {
		name       string
		storage    *fakeCVMInputStorage
		wantReason string
	}{
		{"head unavailable", &fakeCVMInputStorage{sizes: map[string]int64{"organizations/o/r1": 10, "organizations/o/r2": 11}, statErrors: map[string]error{"organizations/o/r2": errors.New("not found")}}, cvmReasonInputObjectUnavailable},
		{"size changed", &fakeCVMInputStorage{sizes: map[string]int64{"organizations/o/r1": 10, "organizations/o/r2": 9}}, cvmReasonInputObjectSizeMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := prepareCVMInputDownloads(context.Background(), tc.storage, assets, nil, time.Hour)
			var objectError *CVMInputObjectError
			if !errors.As(err, &objectError) || objectError.ReasonCode != tc.wantReason {
				t.Fatalf("got error %v, want platform reason %s", err, tc.wantReason)
			}
			if tc.storage.presignCalls != 0 {
				t.Fatalf("presigned %d URLs after failed HEAD", tc.storage.presignCalls)
			}
		})
	}
}

func TestV1RetryRefreshPreservesLegacyShapeWhileRecheckingObject(t *testing.T) {
	asset := &model.DataAsset{ID: 1, UUID: "legacy-r1", FileName: "read.fq.gz", StorageKey: "organizations/o/read", FileSize: 8, ReadType: model.ReadTypeRead1}
	storage := &fakeCVMInputStorage{sizes: map[string]int64{asset.StorageKey: asset.FileSize}}
	downloads, err := prepareCVMInputDownloadsForContract(context.Background(), storage, []*model.DataAsset{asset}, nil, time.Hour, "germline-v1")
	if err != nil {
		t.Fatalf("legacy single mate must remain refreshable while v1 is accepted: %v", err)
	}
	if storage.statCalls != 1 || storage.presignCalls != 1 {
		t.Fatalf("legacy refresh did not repeat HEAD and URL refresh: HEAD/presign=%d/%d", storage.statCalls, storage.presignCalls)
	}
	if downloads[0].ExpectedSizeBytes != 0 || downloads[0].ValidationRole != "" || downloads[0].PairKey != "" {
		t.Fatalf("v1 refresh unexpectedly changed contract shape: %#v", downloads[0])
	}
}

func TestCVMDispatchFailureReasonPreservesOnlyTypedPlatformPreflightCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&CVMInputObjectError{ReasonCode: cvmReasonInputObjectUnavailable, err: errors.New("head failed")}, cvmReasonInputObjectUnavailable},
		{&CVMInputObjectError{ReasonCode: cvmReasonInputObjectSizeMismatch, err: errors.New("size changed")}, cvmReasonInputObjectSizeMismatch},
		{errors.New("generic dispatch failure"), "DISPATCH_FAILED"},
	} {
		if got := cvmDispatchFailureReason(tc.err); got != tc.want {
			t.Errorf("reason = %q, want %q", got, tc.want)
		}
	}
}

func TestValidateCVMDownloadPairsRejectsIncompleteAndDuplicateMates(t *testing.T) {
	for _, tc := range []struct {
		name      string
		downloads []model.CVMInputDownload
	}{
		{"incomplete", []model.CVMInputDownload{contractDownload("fastq_r1")}},
		{"duplicate mate", []model.CVMInputDownload{contractDownload("fastq_r1"), contractDownload("fastq_r1"), contractDownload("fastq_r2")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateCVMDownloadPairs(tc.downloads); err == nil {
				t.Fatal("expected malformed FASTQ pair to be rejected")
			}
		})
	}
}

func contractDownload(role string) model.CVMInputDownload {
	return model.CVMInputDownload{ExpectedSizeBytes: 1, PairKey: "single:0", ValidationRole: role}
}
