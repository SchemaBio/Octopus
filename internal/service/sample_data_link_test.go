package service

import (
	"encoding/json"
	"errors"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestResolveSampleDataLinkExactIdentityAndPublicMetadata(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)
	assets := map[string]*model.DataAsset{
		"private/a": {UUID: "r1-id", StorageKey: "private/a", FileName: "same.fastq.gz", Status: model.FileStatusCompleted, ReadType: model.ReadTypeRead1, ExpiresAt: &future},
		"private/b": {UUID: "r2-id", StorageKey: "private/b", FileName: "same.fastq.gz", Status: model.FileStatusCompleted, ReadType: model.ReadTypeRead2},
	}
	result, err := resolveSampleDataLink(&model.MatchedPair{R1Path: "private/a", R2Path: "private/b"}, func(key string) (*model.DataAsset, error) { return assets[key], nil }, now)
	if err != nil || result.Unavailable || result.Read1.ID != "r1-id" || result.Read2.ID != "r2-id" {
		t.Fatalf("unexpected link: %+v %v", result, err)
	}
	payload, _ := json.Marshal(result)
	if strings.Contains(string(payload), "private/") {
		t.Fatal("storage identity leaked")
	}
}

func TestResolveSampleDataLinkMissingExpiredAndErrors(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Second)
	pair := &model.MatchedPair{R1Path: "expired", R2Path: "missing"}
	result, err := resolveSampleDataLink(pair, func(key string) (*model.DataAsset, error) {
		if key == "missing" {
			return nil, gorm.ErrRecordNotFound
		}
		return &model.DataAsset{Status: model.FileStatusCompleted, ReadType: model.ReadTypeRead1, ExpiresAt: &past}, nil
	}, now)
	if err != nil || !result.Unavailable || result.Read1 != nil || result.Read2 != nil {
		t.Fatalf("unavailable files returned: %+v %v", result, err)
	}
	expected := errors.New("database unavailable")
	if _, err = resolveSampleDataLink(pair, func(string) (*model.DataAsset, error) { return nil, expected }, now); !errors.Is(err, expected) {
		t.Fatal("database failure hidden")
	}
	result, err = resolveSampleDataLink(nil, func(string) (*model.DataAsset, error) { t.Fatal("unlinked sample looked up files"); return nil, nil }, now)
	if err != nil || result.Unavailable {
		t.Fatal("unlinked sample marked unavailable")
	}
}
