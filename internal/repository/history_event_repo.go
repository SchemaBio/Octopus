package repository

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

// latestReviewEvents is the event-backed read model for /history. It keeps
// one latest state per task/attempt/variant fingerprint, so result row UUIDs
// can safely change during re-import.
func (r *HistoryRepository) latestReviewEvents(query *model.HistoryListQuery, variantType string) ([]model.VariantReviewEvent, error) {
	db := r.db.Model(&model.VariantReviewEvent{}).Where("variant_type = ?", variantType)
	if query != nil && !query.IncludeAll {
		tenantID := model.TenantIDForIdentity(query.ExternalOrgID, query.CreatedBy)
		db = db.Where("tenant_id = ?", tenantID)
	}
	var rows []model.VariantReviewEvent
	if err := db.Order("recorded_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(rows))
	out := make([]model.VariantReviewEvent, 0, len(rows))
	for _, row := range rows {
		key := strings.Join([]string{row.TenantID, row.TaskUUID, row.ExecutionAttemptID, row.VariantType, row.VariantFingerprint}, "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		if row.Action == model.VariantReviewActionRevoked && (query == nil || !query.IncludeRevoked) {
			continue
		}
		if query != nil && query.Search != "" {
			needle := strings.ToLower(query.Search)
			if !strings.Contains(strings.ToLower(row.HistoryGroupKey), needle) && !strings.Contains(strings.ToLower(row.VariantSnapshotJSON), needle) && !strings.Contains(strings.ToLower(row.TaskName), needle) {
				continue
			}
		}
		out = append(out, row)
	}
	return out, nil
}

type eventGroup struct {
	key         string
	rows        []model.VariantReviewEvent
	active      int
	first, last *time.Time
	unknown     bool
}

func groupEvents(rows []model.VariantReviewEvent) []*eventGroup {
	groups := map[string]*eventGroup{}
	for _, row := range rows {
		g := groups[row.HistoryGroupKey]
		if g == nil {
			g = &eventGroup{key: row.HistoryGroupKey}
			groups[row.HistoryGroupKey] = g
		}
		g.rows = append(g.rows, row)
		if row.Action == model.VariantReviewActionReviewed {
			g.active++
			if row.OccurredAt == nil || !row.TimestampKnown {
				g.unknown = true
			} else {
				t := *row.OccurredAt
				if g.first == nil || t.Before(*g.first) {
					g.first = &t
				}
				if g.last == nil || t.After(*g.last) {
					g.last = &t
				}
			}
		}
	}
	out := make([]*eventGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].active != out[j].active {
			return out[i].active > out[j].active
		}
		return out[i].key < out[j].key
	})
	return out
}

func paginateEventGroups(groups []*eventGroup, query *model.HistoryListQuery) ([]*eventGroup, int64) {
	total := int64(len(groups))
	page, size := 1, 20
	if query != nil {
		if query.Page > 0 {
			page = query.Page
		}
		if query.PageSize > 0 {
			size = query.PageSize
		}
	}
	start := (page - 1) * size
	if start >= len(groups) {
		return []*eventGroup{}, total
	}
	end := start + size
	if end > len(groups) {
		end = len(groups)
	}
	return groups[start:end], total
}

func eventRecord(row model.VariantReviewEvent) model.DetectionRecord {
	value := ""
	if row.OccurredAt != nil && row.TimestampKnown {
		value = row.OccurredAt.UTC().Format(time.RFC3339)
	}
	return model.DetectionRecord{RecordID: row.ID, TaskID: row.TaskUUID, TaskName: row.TaskName, Pipeline: row.Pipeline, PipelineVersion: row.PipelineVersion, SampleID: row.SampleID, InternalID: row.InternalID, ReviewedAt: value, ReviewedBy: row.ActorEmail, ExecutionAttemptID: row.ExecutionAttemptID, VariantFingerprint: row.VariantFingerprint, ReferenceGenome: row.ReferenceGenome, Action: row.Action, TimestampKnown: row.TimestampKnown}
}

func snapshot(row model.VariantReviewEvent) map[string]interface{} {
	var v map[string]interface{}
	_ = json.Unmarshal([]byte(row.VariantSnapshotJSON), &v)
	return v
}
func ss(v map[string]interface{}, key string) string {
	if value, ok := v[key].(string); ok {
		return value
	}
	return ""
}
func ii(v map[string]interface{}, key string) int64 {
	switch value := v[key].(type) {
	case float64:
		return int64(value)
	case json.Number:
		n, _ := strconv.ParseInt(string(value), 10, 64)
		return n
	case int64:
		return value
	}
	return 0
}
func ff(v map[string]interface{}, key string) float64 {
	switch value := v[key].(type) {
	case float64:
		return value
	case json.Number:
		n, _ := strconv.ParseFloat(string(value), 64)
		return n
	}
	return 0
}
func stringArray(v map[string]interface{}, key string) []string {
	raw := ss(v, key)
	if raw == "" {
		if a, ok := v[key].([]interface{}); ok {
			out := make([]string, 0, len(a))
			for _, x := range a {
				out = append(out, fmt.Sprint(x))
			}
			return out
		}
		return []string{}
	}
	var a []string
	if json.Unmarshal([]byte(raw), &a) == nil {
		return a
	}
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '|' })
}
func times(g *eventGroup) (string, string) {
	var first, last string
	if g.first != nil {
		first = g.first.UTC().Format(time.RFC3339)
	}
	if g.last != nil {
		last = g.last.UTC().Format(time.RFC3339)
	}
	return first, last
}

func (r *HistoryRepository) eventGroups(query *model.HistoryListQuery, variantType string) ([]*eventGroup, int64, error) {
	rows, err := r.latestReviewEvents(query, variantType)
	if err != nil {
		return nil, 0, err
	}
	groups := groupEvents(rows)
	page, total := paginateEventGroups(groups, query)
	return page, total, nil
}

func (r *HistoryRepository) GetGroupedSNVIndelsFromEvents(q *model.HistoryListQuery) ([]model.GroupedSNVIndel, int64, error) {
	gs, total, err := r.eventGroups(q, "snv-indel")
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.GroupedSNVIndel, 0, len(gs))
	for _, g := range gs {
		row := snapshot(g.rows[0])
		first, last := times(g)
		item := model.GroupedSNVIndel{GroupID: g.key, Gene: ss(row, "gene"), HGVSc: ss(row, "hgvsc"), HGVSp: ss(row, "hgvsp"), Transcript: ss(row, "transcript"), ACMGClassification: model.ACMGClassification(ss(row, "acmgClassification")), Consequence: ss(row, "consequence"), RsID: ss(row, "rsId"), ClinvarID: ss(row, "clinvarDn"), DetectionCount: g.active, FirstDetectedAt: first, LastDetectedAt: last, ReferenceGenome: g.rows[0].ReferenceGenome, HasUnknownReviewTime: g.unknown}
		for _, e := range g.rows {
			item.Records = append(item.Records, eventRecord(e))
		}
		out = append(out, item)
	}
	return out, total, nil
}

func (r *HistoryRepository) GetGroupedCNVSegmentsFromEvents(q *model.HistoryListQuery) ([]model.GroupedCNVSegment, int64, error) {
	gs, total, err := r.eventGroups(q, "cnv-segment")
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.GroupedCNVSegment, 0, len(gs))
	for _, g := range gs {
		row := snapshot(g.rows[0])
		first, last := times(g)
		ratio := ff(row, "copyRatio")
		item := model.GroupedCNVSegment{GroupID: g.key, Chromosome: ss(row, "chromosome"), StartPosition: ii(row, "startPosition"), EndPosition: ii(row, "endPosition"), Length: ii(row, "endPosition") - ii(row, "startPosition"), Type: ss(row, "type"), CopyNumber: int(ratio * 2), Genes: stringArray(row, "dosageGenes"), Confidence: ff(row, "weight"), DetectionCount: g.active, FirstDetectedAt: first, LastDetectedAt: last, ReferenceGenome: g.rows[0].ReferenceGenome, HasUnknownReviewTime: g.unknown}
		for _, e := range g.rows {
			item.Records = append(item.Records, eventRecord(e))
		}
		out = append(out, item)
	}
	return out, total, nil
}

func (r *HistoryRepository) GetGroupedCNVExonsFromEvents(q *model.HistoryListQuery) ([]model.GroupedCNVExon, int64, error) {
	gs, total, err := r.eventGroups(q, "cnv-exon")
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.GroupedCNVExon, 0, len(gs))
	for _, g := range gs {
		row := snapshot(g.rows[0])
		first, last := times(g)
		item := model.GroupedCNVExon{GroupID: g.key, Gene: ss(row, "gene"), Transcript: ss(row, "transcript"), Exon: strconv.FormatInt(ii(row, "exonCount"), 10), Chromosome: ss(row, "chromosome"), StartPosition: ii(row, "startPosition"), EndPosition: ii(row, "endPosition"), Type: ss(row, "type"), CopyNumber: int(ff(row, "copyRatio") * 2), Ratio: ff(row, "copyRatio"), Confidence: ff(row, "weight"), DetectionCount: g.active, FirstDetectedAt: first, LastDetectedAt: last, ReferenceGenome: g.rows[0].ReferenceGenome, HasUnknownReviewTime: g.unknown}
		for _, e := range g.rows {
			item.Records = append(item.Records, eventRecord(e))
		}
		out = append(out, item)
	}
	return out, total, nil
}

func (r *HistoryRepository) GetGroupedSTRsFromEvents(q *model.HistoryListQuery) ([]model.GroupedSTR, int64, error) {
	gs, total, err := r.eventGroups(q, "str")
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.GroupedSTR, 0, len(gs))
	for _, g := range gs {
		row := snapshot(g.rows[0])
		first, last := times(g)
		item := model.GroupedSTR{GroupID: g.key, Gene: ss(row, "gene"), Locus: ss(row, "chromosome") + ":" + strconv.FormatInt(ii(row, "position"), 10), RepeatUnit: ss(row, "repeatUnit"), NormalRangeMax: int(ii(row, "normalRangeMax")), Status: ss(row, "status"), MinRepeatCount: int(ii(row, "refRepeats")), MaxRepeatCount: int(ii(row, "refRepeats")), DetectionCount: g.active, FirstDetectedAt: first, LastDetectedAt: last, ReferenceGenome: g.rows[0].ReferenceGenome, HasUnknownReviewTime: g.unknown}
		for _, e := range g.rows {
			item.Records = append(item.Records, eventRecord(e))
		}
		out = append(out, item)
	}
	return out, total, nil
}

func (r *HistoryRepository) GetGroupedMEIsFromEvents(q *model.HistoryListQuery) ([]model.GroupedMEI, int64, error) {
	gs, total, err := r.eventGroups(q, "mei")
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.GroupedMEI, 0, len(gs))
	for _, g := range gs {
		row := snapshot(g.rows[0])
		first, last := times(g)
		item := model.GroupedMEI{GroupID: g.key, Chromosome: ss(row, "chromosome"), Position: ii(row, "position"), Gene: ss(row, "gene"), TEType: ss(row, "teType"), Direction: ss(row, "direction"), Length: int64(ff(row, "avgSoftClipLength")), Impact: ss(row, "impact"), DetectionCount: g.active, FirstDetectedAt: first, LastDetectedAt: last, ReferenceGenome: g.rows[0].ReferenceGenome, HasUnknownReviewTime: g.unknown}
		for _, e := range g.rows {
			item.Records = append(item.Records, eventRecord(e))
		}
		out = append(out, item)
	}
	return out, total, nil
}

func (r *HistoryRepository) GetGroupedMTVariantsFromEvents(q *model.HistoryListQuery) ([]model.GroupedMTVariant, int64, error) {
	gs, total, err := r.eventGroups(q, "mt")
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.GroupedMTVariant, 0, len(gs))
	for _, g := range gs {
		row := snapshot(g.rows[0])
		first, last := times(g)
		item := model.GroupedMTVariant{GroupID: g.key, Position: ii(row, "position"), Ref: ss(row, "ref"), Alt: ss(row, "alt"), Gene: ss(row, "gene"), Pathogenicity: ss(row, "clinvarSig"), AssociatedDisease: ss(row, "mitophenPhenotypes"), MinHeteroplasmy: ff(row, "heteroplasmy"), MaxHeteroplasmy: ff(row, "heteroplasmy"), DetectionCount: g.active, FirstDetectedAt: first, LastDetectedAt: last, ReferenceGenome: g.rows[0].ReferenceGenome, HasUnknownReviewTime: g.unknown}
		for _, e := range g.rows {
			item.Records = append(item.Records, eventRecord(e))
		}
		out = append(out, item)
	}
	return out, total, nil
}

func (r *HistoryRepository) GetGroupedUPDRegionsFromEvents(q *model.HistoryListQuery) ([]model.GroupedUPDRegion, int64, error) {
	gs, total, err := r.eventGroups(q, "upd")
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.GroupedUPDRegion, 0, len(gs))
	for _, g := range gs {
		row := snapshot(g.rows[0])
		first, last := times(g)
		item := model.GroupedUPDRegion{GroupID: g.key, Chromosome: ss(row, "chromosome"), StartPosition: ii(row, "startPosition"), EndPosition: ii(row, "endPosition"), Length: ii(row, "length"), Type: model.UPDType(ss(row, "type")), Genes: stringArray(row, "genes"), ParentOfOrigin: model.ParentOfOrigin(ss(row, "parentOfOrigin")), DetectionCount: g.active, FirstDetectedAt: first, LastDetectedAt: last, ReferenceGenome: g.rows[0].ReferenceGenome, HasUnknownReviewTime: g.unknown}
		for _, e := range g.rows {
			item.Records = append(item.Records, eventRecord(e))
		}
		out = append(out, item)
	}
	return out, total, nil
}
