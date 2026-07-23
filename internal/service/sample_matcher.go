package service

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var fastqPairPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(.+?)(?:_S[0-9]+)?(?:_L[0-9]{3})?_R([12])(?:_[0-9]{3})?$`),
	regexp.MustCompile(`(?i)^(.+?)[._-]([12])$`),
}

type SampleMatcher struct {
	samples *repository.SampleRepository
	assets  *repository.DataAssetRepository
}

func NewSampleMatcher() *SampleMatcher {
	return &SampleMatcher{samples: repository.NewSampleRepository(), assets: repository.NewDataAssetRepository()}
}

func (m *SampleMatcher) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		m.run(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.run(ctx)
			}
		}
	}()
}

func (m *SampleMatcher) run(ctx context.Context) {
	samples, err := m.samples.FindAutoMatchable(500)
	if err != nil {
		return
	}
	for i := range samples {
		select {
		case <-ctx.Done():
			return
		default:
		}
		m.matchSample(ctx, &samples[i])
	}
}

func (m *SampleMatcher) matchSample(ctx context.Context, sample *model.Sample) {
	assets, err := m.assets.FindCompletedByScope(sample.ExternalOrgID, sample.CreatedBy)
	if err != nil {
		return
	}
	var read1, read2 []model.DataAsset
	for i := range assets {
		key, readType, ok := parseFASTQPairName(assets[i].FileName)
		if !ok || !strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(sample.InternalID)) {
			continue
		}
		if readType == model.ReadTypeRead1 && assets[i].ReadType == model.ReadTypeRead1 {
			read1 = append(read1, assets[i])
		}
		if readType == model.ReadTypeRead2 && assets[i].ReadType == model.ReadTypeRead2 {
			read2 = append(read2, assets[i])
		}
	}
	switch {
	case len(read1) == 1 && len(read2) == 1:
		_ = m.autoLink(sample.ID, &read1[0], &read2[0])
	case len(read1) > 1 || len(read2) > 1:
		m.updateStatus(sample, model.SampleMatchConflict)
	case len(read1)+len(read2) > 0:
		m.updateStatus(sample, model.SampleMatchPartial)
	case sample.MatchMode == model.SampleMatchModeAutomatic && sample.GetMatchedPair() != nil:
		m.updateStatus(sample, model.SampleMatchMissing)
	default:
		m.updateStatus(sample, model.SampleMatchUnmatched)
	}
}

func (m *SampleMatcher) autoLink(sampleID uint, read1, read2 *model.DataAsset) error {
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		var sample model.Sample
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sample, sampleID).Error; err != nil {
			return err
		}
		if sample.MatchMode == model.SampleMatchModeManual || !sample.AutoMatchEnabled {
			return nil
		}
		now := time.Now()
		link := model.SampleDataLink{
			SampleID: sample.ID, ExternalOrgID: sample.ExternalOrgID,
			Read1AssetID: read1.ID, Read2AssetID: read2.ID,
			MatchMode: model.SampleMatchModeAutomatic, MatchRule: "filename_internal_id_exact",
			MatchedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "sample_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"external_org_id", "read1_asset_id", "read2_asset_id", "match_mode", "match_rule", "matched_by", "matched_at", "updated_at"}),
		}).Create(&link).Error; err != nil {
			return err
		}
		sample.SetMatchedPair(&model.MatchedPair{R1Path: read1.StorageKey, R2Path: read2.StorageKey})
		sample.MatchStatus = model.SampleMatchMatched
		sample.MatchMode = model.SampleMatchModeAutomatic
		return tx.Save(&sample).Error
	})
}

func (m *SampleMatcher) updateStatus(sample *model.Sample, status model.SampleMatchStatus) {
	if sample.MatchMode == model.SampleMatchModeManual || sample.MatchStatus == status {
		return
	}
	database.GetDB().Model(&model.Sample{}).
		Where("id = ? AND (match_mode IS NULL OR match_mode <> ?)", sample.ID, model.SampleMatchModeManual).
		Update("match_status", status)
}

func parseFASTQPairName(name string) (string, model.ReadType, bool) {
	base := filepath.Base(name)
	lower := strings.ToLower(base)
	for _, suffix := range []string{".fastq.gz", ".fq.gz", ".fastq", ".fq"} {
		if strings.HasSuffix(lower, suffix) {
			base = base[:len(base)-len(suffix)]
			break
		}
	}
	for _, pattern := range fastqPairPatterns {
		matches := pattern.FindStringSubmatch(base)
		if len(matches) != 3 {
			continue
		}
		if matches[2] == "1" {
			return matches[1], model.ReadTypeRead1, true
		}
		return matches[1], model.ReadTypeRead2, true
	}
	return "", "", false
}
