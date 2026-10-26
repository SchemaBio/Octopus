package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SchemaBio/Octopus/internal/model"
)

const maxKinshipReportBytes = 1 << 20

// Read only the active, tenant-scoped archive. Failure is explicit QC
// unavailability and must not prevent the variant workspace from opening.
func (s *ResultService) archivedKinship(ctx context.Context, task *model.Task) *model.ResultKinship {
	if task.Template != "trio" {
		return nil
	}
	result := &model.ResultKinship{Status: "unavailable", Source: "Peddy", Pairs: []model.KinshipPair{}}
	if task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusWaitingData {
		result.Status = "pending"
		return result
	}
	var body io.ReadCloser
	var err error
	if s.localBrowserStorage() {
		dir, readErr := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
		if readErr != nil {
			return result
		}
		// Use the manifest rather than searching neighboring executions.
		manifest, readErr := (&Archiver{cfg: s.cfg}).readArchiveJSONFile(dir, "outputs.resolved.json")
		if readErr != nil {
			return result
		}
		refs, readErr := kinshipLocalRefs(manifest)
		if readErr != nil || len(refs) != 1 {
			return result
		}
		file := refs[0]
		if !filepath.IsAbs(file) {
			file = filepath.Join(dir, file)
		}
		file, err = resolveArchiveRegularFile(dir, file)
		if err != nil {
			return result
		}
		info, statErr := os.Stat(file)
		if statErr != nil || info.Size() > maxKinshipReportBytes {
			return result
		}
		body, err = os.Open(file)
	} else {
		if s.cfg == nil || !supportsParquetObjectStorage(s.cfg.Storage.Provider) {
			return result
		}
		storage, storageErr := newS3Storage(ctx, s.cfg.Storage)
		if storageErr != nil {
			return result
		}
		current := *task
		current.ExecutionAttemptID = executionAttempt(task)
		prefix := resultPackagePrefix(&current)
		objects, listErr := storage.list(ctx, prefix+"/")
		if listErr != nil {
			return result
		}
		candidates := []s3ObjectInfo{}
		for _, object := range objects {
			if strings.HasSuffix(strings.ToLower(path.Base(object.Key)), ".ped_check.csv") {
				if _, keyErr := safeResultPackageRelativePath(prefix, object.Key); keyErr != nil || object.Size <= 0 || object.Size > maxKinshipReportBytes {
					return result
				}
				candidates = append(candidates, object)
			}
		}
		selected, selectErr := uniqueArchivedParquetObject(candidates, func(key string) (io.ReadCloser, error) { return storage.open(ctx, key) })
		if selectErr != nil {
			return result
		}
		body, err = storage.open(ctx, selected.Key)
	}
	if err != nil {
		return result
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxKinshipReportBytes+1))
	if err != nil || len(data) > maxKinshipReportBytes {
		result.Status = "invalid"
		return result
	}
	pairs, err := parseKinshipReport(string(data))
	if err != nil {
		result.Status = "invalid"
		return result
	}
	result.Pairs = pairs
	result.Status = "ready"
	if len(pairs) == 0 {
		result.Status = "empty"
	}
	return result
}

func parseKinshipReport(raw string) ([]model.KinshipPair, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("invalid kinship header")
	}
	columns := map[string]int{}
	for i, key := range header {
		key = strings.TrimSpace(strings.TrimPrefix(key, "\ufeff"))
		if _, ok := columns[key]; ok {
			return nil, fmt.Errorf("duplicate kinship column")
		}
		columns[key] = i
	}
	required := []string{"sample_a", "sample_b", "rel", "pedigree_relatedness", "ibs0", "ibs2", "n", "pedigree_parents", "predicted_parents", "parent_error", "sample_duplication_error"}
	for _, key := range required {
		if _, ok := columns[key]; !ok {
			return nil, fmt.Errorf("missing kinship column")
		}
	}
	pairs := []model.KinshipPair{}
	seen := map[string]bool{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(row) != len(header) || len(pairs) >= 1000 {
			return nil, fmt.Errorf("invalid kinship row")
		}
		cell := func(key string) string { return strings.TrimSpace(row[columns[key]]) }
		a, b := cell("sample_a"), cell("sample_b")
		if a == "" || b == "" || a == b || len(a) > 256 || len(b) > 256 {
			return nil, fmt.Errorf("invalid kinship sample")
		}
		key := a + "\x00" + b
		if a > b {
			key = b + "\x00" + a
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate kinship pair")
		}
		seen[key] = true
		rel, e1 := strconv.ParseFloat(cell("rel"), 64)
		expected, e2 := strconv.ParseFloat(cell("pedigree_relatedness"), 64)
		ibs0, e3 := strconv.ParseInt(cell("ibs0"), 10, 64)
		ibs2, e4 := strconv.ParseInt(cell("ibs2"), 10, 64)
		n, e5 := strconv.ParseInt(cell("n"), 10, 64)
		pedigree, e6 := strconv.ParseBool(cell("pedigree_parents"))
		predicted, e7 := strconv.ParseBool(cell("predicted_parents"))
		parentError, e8 := strconv.ParseBool(cell("parent_error"))
		dup, e9 := strconv.ParseBool(cell("sample_duplication_error"))
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || e7 != nil || e8 != nil || e9 != nil || math.IsNaN(rel) || math.IsInf(rel, 0) || math.IsNaN(expected) || math.IsInf(expected, 0) || n <= 0 || ibs0 < 0 || ibs2 < 0 || ibs0 > n || ibs2 > n || ibs0+ibs2 > n {
			return nil, fmt.Errorf("invalid kinship metric")
		}
		pairs = append(pairs, model.KinshipPair{SampleA: a, SampleB: b, Relatedness: rel, PedigreeRelatedness: expected, IBS0: ibs0, IBS2: ibs2, Sites: n, PedigreeParents: pedigree, PredictedParents: predicted, ParentError: parentError, SampleDuplicationError: dup})
	}
	return pairs, nil
}

func kinshipLocalRefs(raw []byte) ([]string, error) {
	var manifest map[string]interface{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	refs := []string{}
	for _, ref := range manifestTextRefs(manifest) {
		if strings.HasSuffix(strings.ToLower(filepath.Base(ref)), ".ped_check.csv") {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}
