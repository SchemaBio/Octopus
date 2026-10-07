package service

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
)

type assessmentGenotype struct {
	GT string    `json:"gt"`
	DP *float64  `json:"dp,omitempty"`
	GQ *float64  `json:"gq,omitempty"`
	AD []float64 `json:"ad,omitempty"`
	PS string    `json:"ps,omitempty"`
}

func parseAssessmentGenotype(format, value string) assessmentGenotype {
	out := assessmentGenotype{}
	fields := strings.Split(format, ":")
	values := strings.Split(value, ":")
	for i, key := range fields {
		if i >= len(values) {
			break
		}
		v := values[i]
		switch key {
		case "GT":
			out.GT = v
		case "DP", "GQ":
			if n, err := strconv.ParseFloat(v, 64); err == nil && finiteRange(n, 0, 1e9) {
				if key == "DP" {
					out.DP = &n
				} else {
					out.GQ = &n
				}
			}
		case "AD":
			for _, x := range strings.Split(v, ",") {
				n, err := strconv.ParseFloat(x, 64)
				if err != nil || !finiteRange(n, 0, 1e9) {
					out.AD = nil
					break
				}
				out.AD = append(out.AD, n)
			}
		case "PS":
			if v != "." {
				out.PS = v
			}
		}
	}
	return out
}
func assessmentAlleleKey(chr, pos, ref, alt string) string {
	return strings.TrimPrefix(chr, "chr") + ":" + pos + ":" + ref + ":" + alt
}

type assessmentLimitedReader struct {
	reader    io.Reader
	remaining int64
}

func (r *assessmentLimitedReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, fmt.Errorf("VCF evidence exceeds decoded limit")
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}
func readAssessmentVCF(input io.Reader, members []model.ResultMember) (map[string]map[string]assessmentGenotype, error) {
	scanner := bufio.NewScanner(&assessmentLimitedReader{reader: input, remaining: 128 * 1024 * 1024})
	scanner.Buffer(make([]byte, 65536), 2*1024*1024)
	roles := map[int]string{}
	rows := map[string]map[string]assessmentGenotype{}
	header := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "##") {
			continue
		}
		fields := strings.Split(line, "\t")
		if strings.HasPrefix(line, "#CHROM") {
			if len(fields) < 9 {
				return nil, fmt.Errorf("invalid VCF sample header")
			}
			header = true
			for i, name := range fields[9:] {
				for _, member := range members {
					if name == member.ID || name == member.SampleID {
						if member.Role == "proband" || member.Role == "father" || member.Role == "mother" {
							roles[i+9] = member.Role
						}
					}
				}
			}
			continue
		}
		if !header || len(fields) < 10 || strings.Contains(fields[4], ",") {
			continue
		}
		if len(rows) >= 1000000 {
			return nil, fmt.Errorf("VCF evidence record limit exceeded")
		}
		genotypes := map[string]assessmentGenotype{}
		for i, role := range roles {
			if i < len(fields) {
				genotypes[role] = parseAssessmentGenotype(fields[8], fields[i])
			}
		}
		if len(genotypes) > 0 {
			key := assessmentAlleleKey(fields[0], fields[1], fields[3], fields[4])
			if _, exists := rows[key]; exists {
				return nil, fmt.Errorf("ambiguous duplicate VCF allele")
			}
			rows[key] = genotypes
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("VCF evidence read failed")
	}
	return rows, nil
}

// Controlled I/O-only preparation: supplement original GT/GQ/DP/AD and explicit
// coordinate conventions. No ACMG, filtering, priority, or pin calculation runs
// here. Peddy/phase/LoF proofs are not fabricated from an absent source.
func (s *ResultService) PrepareAssessmentEvidence(ctx context.Context, taskID, attempt string, execute bool) (map[string]interface{}, error) {
	if _, err := uuid.Parse(taskID); err != nil {
		return nil, fmt.Errorf("invalid task")
	}
	if _, err := uuid.Parse(attempt); err != nil {
		return nil, fmt.Errorf("invalid attempt")
	}
	var task model.Task
	if err := database.DB.WithContext(ctx).Where("uuid=? AND execution_attempt_id=?", taskID, attempt).First(&task).Error; err != nil {
		return nil, err
	}
	workspace, err := s.GetContext(ctx, &task)
	if err != nil {
		return nil, err
	}
	if workspace.State != "ready" {
		return nil, ErrParquetIncomplete
	}
	output := map[string]interface{}{"taskId": taskID, "attemptId": attempt, "mode": "inspect", "tables": workspace.Parquet.Tables}
	if !execute {
		return output, nil
	}
	ctx = context.WithValue(ctx, browserLocalAssessmentKey{}, true)
	genotypes := map[string]map[string]assessmentGenotype{}
	archive, err := s.loadIGVArchive(ctx, &task)
	if err != nil {
		return nil, err
	}
	for _, candidate := range archive.candidates {
		if candidate.descriptor.Format != "vcf" || candidate.objectKey == "" {
			continue
		}
		body, err := archive.storage.open(ctx, candidate.objectKey)
		if err != nil {
			return nil, fmt.Errorf("VCF evidence unavailable")
		}
		var reader io.Reader = body
		var gz *gzip.Reader
		if strings.HasSuffix(candidate.objectKey, ".gz") {
			gz, err = gzip.NewReader(body)
			if err != nil {
				body.Close()
				return nil, fmt.Errorf("VCF gzip invalid")
			}
			reader = gz
		}
		parsed, readErr := readAssessmentVCF(reader, workspace.Members)
		if gz != nil {
			gz.Close()
		}
		body.Close()
		if readErr != nil {
			return nil, readErr
		}
		for key, value := range parsed {
			if _, exists := genotypes[key]; exists {
				return nil, fmt.Errorf("multiple VCF sources describe the same allele")
			}
			genotypes[key] = value
		}
	}
	datasets := map[string]string{}
	proofs := map[string]interface{}{}
	matched := 0
	withGQ := 0
	for _, table := range workspace.Parquet.Tables {
		descriptor, err := s.BrowserDataset(ctx, &task, table)
		if err != nil {
			return nil, err
		}
		d := &descriptor.Dataset
		datasets[table] = d.ObjectSHA256
		cache := filepath.Join(s.cfg.ResultQuery.CacheDir, d.ID+"-"+d.ObjectSHA256+".parquet")
		for offset := int64(0); offset < d.Rows; offset += 1000 {
			page, err := NewParquetReader().ReadPage(cache, offset, 1000)
			if err != nil {
				return nil, err
			}
			for i, row := range page.Rows {
				ordinal := offset + int64(i)
				identity := digestAssessment([]byte(fmt.Sprintf("%s/%s/%d", d.ID, d.ObjectSHA256, ordinal)))
				proof := map[string]interface{}{"reference": workspace.Reference.DeclaredID}
				value := func(key string) string {
					v, ok := row[key]
					if !ok || v == nil {
						return ""
					}
					return fmt.Sprint(v)
				}
				if table == "snv-indel" {
					key := assessmentAlleleKey(value("Chromosome"), value("Position"), value("Ref"), value("Alt"))
					if memberGT, ok := genotypes[key]; ok {
						for role, g := range memberGT {
							proof[role] = g
						}
						matched++
						if memberGT["proband"].GQ != nil {
							withGQ++
						}
					}
				}
				// Report formats verified against the workflow scripts: CNV CNR/BED uses
				// 0-based half-open intervals, VCF point reports use 1-based positions.
				start, end := int64(-1), int64(-1)
				chr := value("Chromosome")
				if table == "cnv-segment" || table == "cnv-exon" {
					a, ea := strconv.ParseInt(value("Start"), 10, 64)
					b, eb := strconv.ParseInt(value("End"), 10, 64)
					if ea == nil && eb == nil {
						start = a
						end = b
					}
				}
				if table == "snv-indel" || table == "str" || table == "mt" || table == "mei" {
					if pos, err := strconv.ParseInt(value("Position"), 10, 64); err == nil && pos > 0 {
						start = pos - 1
						end = pos
					}
				}
				if chr != "" && start >= 0 && end > start {
					proof["interval"] = map[string]interface{}{"reference": workspace.Reference.DeclaredID, "chromosome": strings.TrimPrefix(chr, "chr"), "start": start, "end": end}
				}
				proofs[identity] = proof
			}
		}
	}
	raw, err := json.Marshal(map[string]interface{}{"taskId": taskID, "attemptId": attempt, "reference": workspace.Reference.DeclaredID, "datasets": datasets, "proofs": proofs})
	if err != nil || len(raw) > 64*1024*1024 {
		return nil, fmt.Errorf("proof file exceeds limit")
	}
	// Recheck before publication: a concurrent retry must not publish as current.
	var current model.Task
	if err := database.DB.WithContext(ctx).First(&current, "id=?", task.ID).Error; err != nil {
		return nil, err
	}
	if executionAttempt(&current) != attempt {
		return nil, ErrAdjustmentConflict
	}
	dir := filepath.Join(s.cfg.ResultQuery.ReferenceDir, "task-proofs", digestAssessment([]byte(model.TenantIDForTask(&task))), taskID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, "proof-*.part")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, attempt+".json")); err != nil {
		return nil, err
	}
	output["mode"] = "prepared"
	output["rows"] = len(proofs)
	output["vcfMatchedRows"] = matched
	output["probandGQRows"] = withGQ
	output["sha256"] = digestAssessment(raw)
	return output, nil
}
