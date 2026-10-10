package resultengine

import (
	"bufio"
	"bytes"
	"context"

	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Overlay struct {
	RowID   string                 `json:"rowId"`
	Payload map[string]interface{} `json:"payload"`
	Version uint64                 `json:"version"`
	raw     map[string]json.RawMessage
}

func (o *Overlay) UnmarshalJSON(data []byte) error {
	type alias Overlay
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*o = Overlay(a)
	var wire struct {
		Payload map[string]json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	o.raw = wire.Payload
	return nil
}

type Request struct {
	Table      string    `json:"table"`
	FilePath   string    `json:"filePath"`
	DatasetID  string    `json:"datasetId"`
	ObjectHash string    `json:"objectSha256"`
	RowID      string    `json:"rowId,omitempty"`
	RowCount   int64     `json:"rowCount"`
	Offset     int64     `json:"offset"`
	Limit      int64     `json:"limit"`
	Search     string    `json:"search"`
	Sort       string    `json:"sort"`
	Direction  string    `json:"direction"`
	Filters    []Filter  `json:"filters"`
	Overlays   []Overlay `json:"overlays"`
}
type Response struct {
	Items               []map[string]interface{} `json:"items"`
	Total               int64                    `json:"total"`
	RowCount            int64                    `json:"rowCount"`
	Offset              int64                    `json:"offset"`
	Limit               int64                    `json:"limit"`
	Columns             []string                 `json:"columns"`
	ColumnTypes         map[string]string        `json:"columnTypes"`
	FieldProfileVersion string                   `json:"fieldProfileVersion"`
}
type Prepared struct {
	AssessmentFile string `json:"assessmentFile"`
	Profile        string `json:"profile"`
	Rows           int64  `json:"rows"`
}
type Engine struct {
	Root, AssessmentRoot, TempRoot string
	slots                          chan struct{}
	prepare                        chan struct{}
	identities                     sync.Map
}

func New(root, assessments, temp string) *Engine {
	return &Engine{Root: root, AssessmentRoot: assessments, TempRoot: temp, slots: make(chan struct{}, 2), prepare: make(chan struct{}, 1)}
}

var fingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (e *Engine) acquire(ctx context.Context) (func(), error) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case e.slots <- struct{}{}:
		return func() { <-e.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("query_capacity")
	}
}
func (e *Engine) Query(ctx context.Context, q Request) (*Response, error) {
	r, _, err := e.run(ctx, q, false)
	return r, err
}

// Export returns a completed CSV file, never a partially written stream. The
// caller owns deletion after transmission; run removes it on every failure.
func (e *Engine) Export(ctx context.Context, q Request) (string, error) {
	_, path, err := e.run(ctx, q, true)
	return path, err
}
func (e *Engine) run(ctx context.Context, q Request, export bool) (response *Response, output string, err error) {
	release, err := e.acquire(ctx)
	if err != nil {
		return nil, "", err
	}
	defer release()
	duration := 30 * time.Second
	if export {
		duration = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	valid := false
	for _, t := range strings.Fields("snv-indel cnv-segment cnv-exon str mei mt upd roh") {
		if t == q.Table {
			valid = true
		}
	}
	if !valid {
		return nil, "", fmt.Errorf("unsupported table")
	}
	if !fingerprint.MatchString(q.ObjectHash) {
		return nil, "", fmt.Errorf("invalid parquet fingerprint")
	}
	if q.RowID != "" && !fingerprint.MatchString(q.RowID) {
		return nil, "", fmt.Errorf("invalid row identity")
	}
	if len(q.Overlays) > 100000 {
		return nil, "", fmt.Errorf("too many row adjustments")
	}
	if len(q.Filters) > 40 {
		return nil, "", fmt.Errorf("too many filters")
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	if q.Limit < 1 {
		q.Limit = 1
	}
	if q.Limit > 200 {
		q.Limit = 200
	}
	reader, err := e.openSource(ctx, q)
	if err != nil {
		return nil, "", err
	}
	defer reader.Close()
	fields := map[string]bool{}
	original := []string{}
	for _, c := range reader.Columns() {
		fields[c.Name] = true
		original = append(original, c.Name)
	}
	for _, f := range q.Filters {
		if err = validateFilter(f, fields); err != nil {
			return nil, "", err
		}
	}
	if q.Sort != "" {
		if err = validateField(q.Sort, fields); err != nil {
			return nil, "", err
		}
	}
	overlays := map[string]Overlay{}
	for _, o := range q.Overlays {
		if _, ok := overlays[o.RowID]; ok {
			return nil, "", fmt.Errorf("duplicate row adjustment")
		}
		overlays[o.RowID] = o
	}
	columns := append([]string{}, original...)
	for _, f := range overlayFields {
		if !fields[f] {
			columns = append(columns, f)
		}
	}
	sort.Strings(columns)
	types := map[string]string{}
	for _, c := range columns {
		types[c] = FieldType(c)
	}
	response = &Response{Items: []map[string]interface{}{}, RowCount: reader.NumRows(), Offset: q.Offset, Limit: q.Limit, Columns: columns, ColumnTypes: types, FieldProfileVersion: FieldProfile}
	// Footer row count is authoritative: caller-supplied rowCount is a validation
	// expectation, not permission to hide a truncated archive.
	if q.RowCount > 0 && q.RowCount != reader.NumRows() {
		return nil, "", fmt.Errorf("PARQUET_SOURCE_ROW_COUNT_MISMATCH")
	}
	if !export && q.Sort == "" && len(q.Filters) == 0 && strings.TrimSpace(q.Search) == "" && q.RowID == "" {
		response.Total = reader.NumRows()
		if q.Offset >= response.Total {
			return response, "", nil
		}
		if err = reader.SkipRows(q.Offset); err != nil {
			return nil, "", err
		}
		rows, readErr := reader.ReadBatch(int(q.Limit))
		if readErr != nil && readErr != io.EOF {
			return nil, "", readErr
		}
		for i, row := range rows {
			id := RowID(q.DatasetID, q.ObjectHash, q.Offset+int64(i))
			o, exists := overlays[id]
			decorate(row, q.Table, id, q.Offset+int64(i), o, exists)
			response.Items = append(response.Items, row)
		}
		return response, "", nil
	}
	sorter, err := newSorter(ctx, e.TempRoot, q.Direction == "desc")
	if err != nil {
		return nil, "", err
	}
	defer sorter.close()
	// Interactive pages use a bounded heap. Large offsets and exports use the
	// spill sorter instead; the heap never grows with dataset row count.
	var top *topRows
	if !export && q.Sort != "" && q.Offset+q.Limit <= 1200 {
		top = &topRows{s: sorter, limit: int(q.Offset + q.Limit)}
	}
	var writer *csvWriter
	var file *os.File
	extra := append([]string{}, overlayFields...)
	sort.Strings(extra)
	if export {
		file, err = os.CreateTemp(e.TempRoot, "octopus-effective-*.csv")
		if err != nil {
			return nil, "", err
		}
		output = file.Name()
		defer func() {
			file.Close()
			if err != nil {
				os.Remove(output)
			}
		}()
		writer = newCSVWriter(file)
		if err = writer.WriteHeader(append(append(append([]string{}, original...), "row_id"), extra...)); err != nil {
			return nil, "", err
		}
	}
	search := strings.ToLower(strings.TrimSpace(q.Search))
	needsAuto := export || search != "" || strings.HasPrefix(q.Sort, "acmg")
	for _, f := range q.Filters {
		if strings.HasPrefix(f.Column, "acmg") {
			needsAuto = true
		}
	}
	ordinal := int64(0)
	for {
		batch, readErr := reader.ReadBatch(1000)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, "", readErr
		}
		for _, row := range batch {
			if err = ctx.Err(); err != nil {
				return nil, "", err
			}
			id := ""
			if len(overlays) > 0 || q.RowID != "" || export {
				id = RowID(q.DatasetID, q.ObjectHash, ordinal)
			}
			index := ordinal
			ordinal++
			if q.RowID != "" && id != q.RowID {
				continue
			}
			o, hasOverlay := overlays[id]
			var automatic map[string]interface{}
			if q.Table == "snv-indel" && needsAuto {
				automatic = AutomaticACMG(row)
			}
			p := projected{raw: row, overlay: o.Payload, automatic: automatic, fields: fields, table: q.Table}
			match := true
			for _, f := range q.Filters {
				if !matches(p.field(f.Column), f) {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			if search != "" {
				found := false
				for _, c := range original {
					if row[c] != nil && strings.Contains(strings.ToLower(text(row[c])), search) {
						found = true
						break
					}
				}
				v := p.field("acmgClassification")
				if v != nil && strings.Contains(strings.ToLower(text(v)), search) {
					found = true
				}
				if !found {
					continue
				}
			}
			response.Total++
			if !export && q.Sort == "" && (response.Total <= q.Offset || int64(len(response.Items)) >= q.Limit) {
				continue
			}
			entry := sortEntry{Ordinal: index, Key: sortKey{}, Row: row}
			if q.Sort != "" {
				entry.Key = makeKey(q.Sort, p.field(q.Sort))
			}
			if export {
				values := make([]csvCell, 0, len(original)+len(extra)+1)
				for _, c := range original {
					values = append(values, csvValue(row[c]))
				}
				values = append(values, csvCell{Text: id})
				for _, c := range extra {
					v := p.field(c)
					if raw, ok := o.raw[c]; ok && c != "acmgEvidence" {
						if len(raw) > 0 && (raw[0] == '{' || raw[0] == '[') {
							var b bytes.Buffer
							json.Compact(&b, raw)
							v = b.String()
						}
					}
					if c == "acmgEvidence" {
						if raw, ok := o.raw[c]; ok {
							var b bytes.Buffer
							json.Compact(&b, raw)
							v = b.String()
						} else if a, ok := automatic["criteria"].([]interface{}); ok && len(a) > 0 {
							criterion := a[0].(map[string]interface{})
							v = fmt.Sprintf(`[{"code":%q,"strength":%q,"source":%q,"value":%s}]`, criterion["code"], criterion["strength"], criterion["source"], text(criterion["value"]))
						}
					}
					values = append(values, csvValue(v))
				}
				entry.CSV = csvLine(values)
				entry.Row = nil
			} else if top == nil {
				if id == "" {
					id = RowID(q.DatasetID, q.ObjectHash, index)
				}
				decorate(row, q.Table, id, index, o, hasOverlay)
			}
			if q.Sort != "" {
				if top != nil {
					top.add(entry)
				} else if err = sorter.add(entry); err != nil {
					return nil, "", err
				}
			} else if export {
				if err = writer.WriteRaw(entry.CSV); err != nil {
					return nil, "", err
				}
			} else if response.Total > q.Offset && int64(len(response.Items)) < q.Limit {
				response.Items = append(response.Items, row)
			}
		}
	}
	if q.Sort != "" {
		seen := int64(0)
		emit := func(entry sortEntry) error {
			if export {
				return writer.WriteRaw(entry.CSV)
			}
			if seen >= q.Offset && int64(len(response.Items)) < q.Limit {
				if top != nil {
					id := RowID(q.DatasetID, q.ObjectHash, entry.Ordinal)
					o, exists := overlays[id]
					decorate(entry.Row, q.Table, id, entry.Ordinal, o, exists)
				}
				response.Items = append(response.Items, entry.Row)
			}
			seen++
			return nil
		}
		if top != nil {
			err = top.each(emit)
		} else {
			err = sorter.each(emit)
		}
		if err != nil {
			return nil, "", err
		}
	}
	if export {
		writer.Flush()
		err = writer.Error()
		if err == nil {
			err = file.Sync()
		}
		if err != nil {
			return nil, "", err
		}
	}
	return response, output, nil
}
func decorate(row map[string]interface{}, table, id string, index int64, o Overlay, exists bool) {
	var automatic map[string]interface{}
	if table == "snv-indel" {
		automatic = AutomaticACMG(row)
	}
	row["file_row_number"], row["__ordinal"], row["__row_id"], row["__acmg"] = index, index, id, automatic
	row["__adjustments"], row["__adjustment_version"] = nil, nil
	if exists {
		row["__adjustments"], row["__adjustment_version"] = o.Payload, o.Version
	}
}
func csvText(v interface{}) string {
	if v == nil {
		return ""
	}
	return text(v)
}
func (e *Engine) Prepare(ctx context.Context, q Request) (*Prepared, error) {
	release, err := e.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case e.prepare <- struct{}{}:
		defer func() { <-e.prepare }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if q.Table != "snv-indel" {
		return nil, fmt.Errorf("automatic ACMG is only supported for SNP/InDel")
	}
	if !fingerprint.MatchString(q.DatasetID) || !fingerprint.MatchString(q.ObjectHash) {
		return nil, fmt.Errorf("invalid dataset identity")
	}
	reader, err := e.openSource(ctx, q)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if q.RowCount > 0 && q.RowCount != reader.NumRows() {
		return nil, fmt.Errorf("PARQUET_SOURCE_ROW_COUNT_MISMATCH")
	}
	if err = os.MkdirAll(e.AssessmentRoot, 0750); err != nil {
		return nil, err
	}
	output := filepath.Join(e.AssessmentRoot, q.DatasetID+"-"+q.ObjectHash+"-"+ACMGProfile+".jsonl")
	if f, openErr := os.Open(output); openErr == nil {
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 65536), 1<<20)
		rows := int64(0)
		for scanner.Scan() {
			if err = ctx.Err(); err != nil {
				break
			}
			rows++
		}
		if err == nil {
			err = scanner.Err()
		}
		f.Close()
		if err != nil {
			return nil, err
		}
		if rows != reader.NumRows() {
			return nil, fmt.Errorf("automatic assessment row count mismatch")
		}
		return &Prepared{output, ACMGProfile, rows}, nil
	}
	file, err := os.CreateTemp(e.AssessmentRoot, ".acmg-*.tmp")
	if err != nil {
		return nil, err
	}
	defer func() { file.Close(); os.Remove(file.Name()) }()
	if err = file.Chmod(0640); err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	ordinal := int64(0)
	for {
		rows, readErr := reader.ReadBatch(1000)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
		for _, row := range rows {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			record := map[string]interface{}{"rowId": RowID(q.DatasetID, q.ObjectHash, ordinal), "profileVersion": ACMGProfile, "assessment": AutomaticACMG(row)}
			if err = encoder.Encode(record); err != nil {
				return nil, err
			}
			ordinal++
		}
	}
	if err = file.Sync(); err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	if err = os.Rename(file.Name(), output); err != nil {
		return nil, err
	}
	return &Prepared{output, ACMGProfile, ordinal}, nil
}
