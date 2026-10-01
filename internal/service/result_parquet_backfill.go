package service

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/xitongsys/parquet-go/writer"
)

const parquetCatalogName = "results.parquet.catalog.json"

// A separate catalogue records controlled conversions without rewriting original outputs.
func (s *ResultService) BackfillArchivedParquet(ctx context.Context, task *model.Task, repairTable string) (map[string]interface{}, error) {
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.cfg.ResultQuery.CacheDir, 0750); err != nil {
		return nil, err
	}
	prefix := resultPackagePrefix(task)
	objects, err := storage.list(ctx, prefix+"/")
	if err != nil {
		return nil, err
	}
	manifestKey, _, _, err := igvArchiveObjectIndex(prefix, objects)
	if err != nil {
		return nil, err
	}
	manifest, err := readIGVManifest(ctx, storage, manifestKey)
	if err != nil {
		return nil, err
	}
	byKey := map[string]s3ObjectInfo{}
	for _, o := range objects {
		byKey[o.Key] = o
	}
	if repairTable != "" && !validParquetTable(repairTable) {
		return nil, fmt.Errorf("invalid repair table")
	}
	references := map[string]string{}
	sources := map[string]interface{}{}
	for _, ref := range manifestParquetRefs(manifest) {
		key, err := archiveParquetRefKey(storage.bucket, prefix, ref)
		if err != nil {
			return nil, err
		}
		if object, ok := byKey[key]; ok && object.Size > 0 {
			for _, table := range []string{"snv-indel", "cnv-segment", "cnv-exon", "str", "mei", "mt", "roh", "upd"} {
				if parquetTableMatch(table, path.Base(key)) {
					if references[table] != "" && references[table] != key {
						return nil, fmt.Errorf("ambiguous Parquet table")
					}
					references[table] = key
				}
			}
		}
	}
	originalReferences := map[string]string{}
	for table, key := range references {
		originalReferences[table] = key
	}
	catalogKey := path.Join(prefix, parquetCatalogName)
	var previous map[string]interface{}
	if _, exists := byKey[catalogKey]; exists {
		previous, err = readIGVManifest(ctx, storage, catalogKey)
		if err != nil {
			return nil, err
		}
		if previous["task_uuid"] != task.UUID || previous["attempt_id"] != executionAttempt(task) {
			return nil, fmt.Errorf("catalogue execution mismatch")
		}
		if datasets, ok := previous["datasets"].(map[string]interface{}); ok {
			for table, raw := range datasets {
				key, validErr := archiveParquetRefKey(storage.bucket, prefix, fmt.Sprint(raw))
				if validErr != nil {
					return nil, validErr
				}
				if object, exists := byKey[key]; exists && object.Size > 0 && validParquetTable(table) {
					references[table] = key
				}
			}
		}
		if old, ok := previous["conversion_sources"].(map[string]interface{}); ok {
			for table, record := range old {
				sources[table] = record
			}
		}
	}
	if repairTable != "" {
		delete(references, repairTable)
	}
	converted := map[string]int64{}
	for _, table := range []string{"snv-indel", "cnv-segment", "cnv-exon", "str", "mei", "mt", "roh", "upd"} {
		if references[table] != "" {
			continue
		}
		candidates := []string{}
		for _, ref := range manifestTextRefs(manifest) {
			if !parquetTableMatch(table, archiveReferenceBaseName(ref)) {
				continue
			}
			key, err := archiveParquetRefKey(storage.bucket, prefix, ref)
			if err != nil {
				return nil, err
			}
			if object, ok := byKey[key]; ok && object.Size > 0 && parquetTableMatch(table, path.Base(key)) {
				candidates = append(candidates, key)
			}
		}
		if len(candidates) == 0 && table == repairTable {
			originalKey := originalReferences[table]
			counterpart := strings.TrimSuffix(originalKey, ".parquet") + ".txt"
			if object, exists := byKey[counterpart]; exists && object.Size > 0 && originalKey != "" {
				candidates = append(candidates, counterpart)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		if len(candidates) != 1 {
			return nil, fmt.Errorf("ambiguous archived text table %s", table)
		}
		key := candidates[0]
		object := byKey[key]
		if object.Size > 256<<20 {
			return nil, fmt.Errorf("archived text exceeds conversion limit")
		}
		input, err := storage.open(ctx, key)
		if err != nil {
			return nil, err
		}
		tmp, err := os.CreateTemp(s.cfg.ResultQuery.CacheDir, ".backfill-input-*")
		if err != nil {
			input.Close()
			return nil, err
		}
		hash := sha256.New()
		size, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(input, 256<<20+1))
		input.Close()
		tmp.Close()
		if copyErr != nil || size != object.Size {
			os.Remove(tmp.Name())
			return nil, fmt.Errorf("archived text size verification failed")
		}
		fingerprint := hex.EncodeToString(hash.Sum(nil))
		output, rows, headers, err := convertArchivedTextParquet(tmp.Name())
		os.Remove(tmp.Name())
		if err != nil {
			return nil, err
		}
		if legacyRawIdentityColumn(legacyIdentityFields(table)[0], headers) == "" || legacyRawIdentityColumn(legacyIdentityFields(table)[1], headers) == "" {
			os.Remove(output)
			return nil, fmt.Errorf("archived table identity columns missing")
		}
		resultKey := path.Join(prefix, "results-parquet-v1", table+"-"+fingerprint+".parquet")
		file, err := os.Open(output)
		if err != nil {
			os.Remove(output)
			return nil, err
		}
		info, _ := file.Stat()
		err = storage.putReader(ctx, resultKey, "application/vnd.apache.parquet", file, info.Size())
		file.Close()
		os.Remove(output)
		if err != nil {
			return nil, err
		}
		references[table] = resultKey
		converted[table] = rows
		sources[table] = map[string]interface{}{"source": key, "source_sha256": fingerprint, "source_bytes": size, "rows": rows, "columns": headers, "converter": "archived-tsv-strings-v1"}
	}
	catalog := map[string]interface{}{"version": "parquet-catalog-v1", "task_uuid": task.UUID, "attempt_id": executionAttempt(task), "datasets": references, "conversion_sources": sources}
	data, _ := json.Marshal(catalog)
	priorData, _ := json.Marshal(previous)
	if string(data) != string(priorData) {
		if err := storage.put(ctx, catalogKey, "application/json", data); err != nil {
			return nil, err
		}
	}
	return map[string]interface{}{"converted_rows": converted, "tables": len(references), "catalog_published": true}, nil
}

func manifestTextRefs(manifest map[string]interface{}) []string {
	seen := map[string]bool{}
	var walk func(interface{})
	walk = func(value interface{}) {
		switch item := value.(type) {
		case map[string]interface{}:
			for _, v := range item {
				walk(v)
			}
		case []interface{}:
			for _, v := range item {
				walk(v)
			}
		case string:
			ext := strings.ToLower(path.Ext(item))
			if ext == ".txt" || ext == ".tsv" || ext == ".csv" {
				seen[item] = true
			}
		}
	}
	walk(manifest)
	result := []string{}
	for key := range seen {
		result = append(result, key)
	}
	return result
}

func convertArchivedTextParquet(input string) (string, int64, []string, error) {
	file, err := os.Open(input)
	if err != nil {
		return "", 0, nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	original, err := reader.Read()
	if err != nil {
		return "", 0, nil, fmt.Errorf("archived table has no readable header")
	}
	names := make([]string, len(original))
	metadata := make([]string, len(original))
	seen := map[string]bool{}
	for i, name := range original {
		name = strings.TrimPrefix(strings.TrimSpace(name), "\ufeff")
		name = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				return r
			}
			return '_'
		}, name)
		if name == "" {
			name = fmt.Sprintf("column_%d", i+1)
		}
		candidate := name
		for index := 2; seen[candidate]; index++ {
			candidate = fmt.Sprintf("%s_%d", name, index)
		}
		seen[candidate] = true
		names[i] = candidate
		metadata[i] = "name=" + candidate + ", type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"
	}
	output, err := os.CreateTemp("", "octopus-backfill-*.parquet")
	if err != nil {
		return "", 0, nil, err
	}
	failed := true
	defer func() {
		output.Close()
		if failed {
			os.Remove(output.Name())
		}
	}()
	parquet, err := writer.NewCSVWriterFromWriter(metadata, output, 1)
	if err != nil {
		return "", 0, nil, err
	}
	rows := int64(0)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) != len(names) {
			return "", 0, nil, fmt.Errorf("archived table row structure invalid at row %d", rows+1)
		}
		values := make([]*string, len(record))
		for i := range record {
			values[i] = &record[i]
		}
		if err := parquet.WriteString(values); err != nil {
			return "", 0, nil, fmt.Errorf("Parquet conversion failed")
		}
		rows++
	}
	if err := parquet.WriteStop(); err != nil {
		return "", 0, nil, err
	}
	if err := output.Close(); err != nil {
		return "", 0, nil, err
	}
	failed = false
	return output.Name(), rows, names, nil
}

func readParquetResultManifest(ctx context.Context, storage *s3Storage, prefix string, objects []s3ObjectInfo) (map[string]interface{}, string, error) {
	key, _, version, err := igvArchiveObjectIndex(prefix, objects)
	if err != nil {
		return nil, "", err
	}
	for _, object := range objects {
		if object.Key == path.Join(prefix, parquetCatalogName) {
			key = object.Key
			break
		}
	}
	manifest, err := readIGVManifest(ctx, storage, key)
	if err != nil {
		return nil, "", err
	}
	if path.Base(key) == parquetCatalogName {
		parts := strings.Split(prefix, "/")
		if len(parts) != 6 || manifest["task_uuid"] != parts[3] || manifest["attempt_id"] != parts[5] {
			return nil, "", fmt.Errorf("invalid Parquet catalogue identity")
		}
	}
	return manifest, version, nil
}

func (s *ResultService) archivedTextSourceRows(ctx context.Context, storage *s3Storage, prefix string, manifest map[string]interface{}, objects map[string]s3ObjectInfo, parquetKey, table string) (*int64, error) {
	sourceKey := ""
	if records, ok := manifest["conversion_sources"].(map[string]interface{}); ok {
		if record, ok := records[table].(map[string]interface{}); ok {
			sourceKey, _ = record["source"].(string)
		}
	}
	if sourceKey == "" {
		for _, suffix := range []string{".txt", ".tsv", ".csv"} {
			candidate := strings.TrimSuffix(parquetKey, ".parquet") + suffix
			if _, exists := objects[candidate]; exists {
				if sourceKey != "" {
					return nil, fmt.Errorf("ambiguous original report")
				}
				sourceKey = candidate
			}
		}
	}
	if sourceKey == "" {
		return nil, nil
	}
	sourceKey, err := archiveParquetRefKey(storage.bucket, prefix, sourceKey)
	if err != nil {
		return nil, err
	}
	object, exists := objects[sourceKey]
	if !exists || object.Size <= 0 || object.Size > 256<<20 {
		return nil, fmt.Errorf("original report is unavailable")
	}
	source, err := storage.open(ctx, sourceKey)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	csvReader := csv.NewReader(io.LimitReader(source, 256<<20+1))
	csvReader.Comma = '\t'
	if path.Ext(sourceKey) == ".csv" {
		csvReader.Comma = ','
	}
	csvReader.LazyQuotes = true
	csvReader.FieldsPerRecord = -1
	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("original report header invalid")
	}
	count := int64(0)
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) != len(header) {
			return nil, fmt.Errorf("original report row structure invalid")
		}
		count++
	}
	return &count, nil
}
