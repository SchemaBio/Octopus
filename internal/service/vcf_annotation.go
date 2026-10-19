package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func isVCFFilename(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".vcf") || strings.HasSuffix(name, ".vcf.gz") || strings.HasSuffix(name, ".vcf.bgz")
}
func (s *TaskService) resolveVCFAsset(req *model.TaskCreateRequest, actor model.OverlayActor) (*model.DataAsset, error) {
	if strings.TrimSpace(req.SampleID) == "" || req.PedigreeID != "" {
		return nil, fmt.Errorf("VCF annotation requires one business sample")
	}
	if strings.ContainsAny(req.VCFSampleName, "\r\n\t\"`$\\") || len(req.VCFSampleName) > 255 {
		return nil, fmt.Errorf("invalid VCF sample name")
	}
	asset, err := s.assetRepo.FindScopedByUUID(strings.TrimSpace(req.VCFAssetID), actor)
	if err != nil || asset.Status != model.FileStatusCompleted || asset.ReadType != model.ReadTypeVCF || asset.FileSize <= 0 || !taskDataAssetUseAllowed(asset, actor) || (asset.ExpiresAt != nil && !asset.ExpiresAt.After(time.Now())) {
		return nil, fmt.Errorf("completed, unexpired VCF data asset not found")
	}
	return asset, nil
}

// Read only declared files inside the active attempt archive; never follow paths
// from the original worker or infer files by a permissive archive scan.
func loadVCFAnnotationInfo(cfg *config.Config, task *model.Task) (map[string]interface{}, error) {
	dir, err := taskArchiveDir(cfg.Task.ArchiveDir, task)
	if err != nil {
		return nil, err
	}
	archiver := &Archiver{cfg: cfg}
	raw, err := archiver.readArchiveJSONFile(dir, "outputs.resolved.json")
	if err != nil {
		return nil, err
	}
	var manifest map[string]interface{}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	info := map[string]interface{}{}
	for _, field := range []string{"preprocessing", "sex_inference", "summary"} {
		node := vcfManifestValue(manifest, field)
		if inline, ok := node.(map[string]interface{}); ok {
			info[field] = inline
			continue
		}
		ref, ok := node.(string)
		if !ok || ref == "" {
			return nil, fmt.Errorf("VCF archive is missing %s", field)
		}
		raw, err = archiver.readArchiveJSONFile(dir, archiveReferenceBaseName(ref))
		if err != nil {
			return nil, err
		}
		var value map[string]interface{}
		if err = json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		info[field] = value
	}
	return info, nil
}

// The Agent expands JSON outputs inline. Also accept file-valued manifests
// produced by other archive implementations without changing the WDL contract.
func vcfManifestValue(manifest map[string]interface{}, field string) interface{} {
	if value, ok := manifest["VCF2Anno."+field]; ok {
		return value
	}
	if field != "summary" {
		if summary, ok := manifest["VCF2Anno.summary"].(map[string]interface{}); ok {
			return summary[field]
		}
	}
	return nil
}

func vcfArchiveMetadataNames(raw []byte) (map[string]bool, error) {
	var manifest map[string]interface{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, field := range []string{"preprocessing", "sex_inference", "summary"} {
		value := vcfManifestValue(manifest, field)
		if _, inline := value.(map[string]interface{}); inline {
			continue
		}
		ref, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("VCF archive is missing %s", field)
		}
		base := archiveReferenceBaseName(ref)
		if base == "" || !strings.HasSuffix(base, ".json") {
			return nil, fmt.Errorf("VCF metadata must be JSON")
		}
		names[base] = true
	}
	return names, nil
}

func vcfDownloadManifest(manifest map[string]interface{}, persist func(string, []byte) (string, error)) (map[string]interface{}, error) {
	result := make(map[string]interface{}, len(manifest))
	for key, value := range manifest {
		result[key] = value
	}
	for _, field := range []string{"preprocessing", "sex_inference", "summary"} {
		value := vcfManifestValue(manifest, field)
		if _, inline := value.(map[string]interface{}); !inline {
			continue
		}
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, err
		}
		ref, err := persist("vcf2anno."+field+".json", raw)
		if err != nil {
			return nil, err
		}
		result["VCF2Anno."+field] = ref
	}
	return result, nil
}

func cacheVCFMetadata(base string, task *model.Task, name string, raw []byte) (string, error) {
	if base == "" {
		return "", fmt.Errorf("result cache directory is required")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(append([]byte(task.UUID+"/"+executionAttempt(task)), raw...)))
	dir := filepath.Join(base, "vcf-metadata", digest)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	filename := filepath.Join(dir, name)
	if existing, err := os.ReadFile(filename); err == nil {
		if string(existing) != string(raw) {
			return "", fmt.Errorf("cached VCF metadata changed")
		}
		return filename, nil
	}
	temporary, err := os.CreateTemp(dir, ".metadata-*")
	if err != nil {
		return "", err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	_, writeErr := temporary.Write(raw)
	closeErr := temporary.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	// The content hash and task identity make concurrently generated values identical.
	if err = os.Rename(temporaryName, filename); err != nil {
		if existing, readErr := os.ReadFile(filename); readErr != nil || string(existing) != string(raw) {
			return "", err
		}
	}
	return filename, nil
}
