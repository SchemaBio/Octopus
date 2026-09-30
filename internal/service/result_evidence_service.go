package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
)

var ErrIGVEvidenceChanged = errors.New("IGV_EVIDENCE_CHANGED")

const maxIGVManifestBytes = 10 << 20

type igvTrackCandidate struct {
	descriptor model.IGVTrackDescriptor
	objectKey  string
	indexKey   string
}

type igvArchive struct {
	storage    *s3Storage
	version    string
	reference  model.IGVReferenceResponse
	candidates []igvTrackCandidate
}

// GetIGVSession returns archive-derived track identities. It does not expose
// object names or signed URLs, so a browser cannot use this endpoint to probe
// arbitrary COS keys.
func (s *ResultService) GetIGVSession(ctx context.Context, task *model.Task) (*model.IGVSessionResponse, error) {
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	attemptID := task.ExecutionAttemptID
	if attemptID == "" {
		attemptID = task.UUID
	}
	archive, err := s.loadIGVArchive(ctx, task)
	if err != nil {
		return &model.IGVSessionResponse{
			TaskUUID: task.UUID, ExecutionAttemptID: attemptID, Available: false,
			Reason: "该执行的归档测序证据当前不可用",
		}, nil
	}
	tracks := make([]model.IGVTrackDescriptor, len(archive.candidates))
	usableTracks := 0
	for i := range archive.candidates {
		tracks[i] = archive.candidates[i].descriptor
		if tracks[i].Available {
			usableTracks++
		}
	}
	available := archive.reference.Available
	reason := archive.reference.Reason
	if usableTracks == 0 {
		available = false
		if reason == "" {
			reason = "归档未提供可读取的 BAM、VCF 或目标 BED 轨迹"
		}
	}
	return &model.IGVSessionResponse{
		TaskUUID: task.UUID, ExecutionAttemptID: attemptID, Version: archive.version,
		Available: available, Reason: reason, Reference: archive.reference, Tracks: tracks,
	}, nil
}

// SignIGVTracks signs only IDs from the current archive manifest. A stale
// browser session receives a conflict sentinel rather than URLs for a changed
// execution result.
func (s *ResultService) SignIGVTracks(ctx context.Context, task *model.Task, request model.IGVURLRequest) (*model.IGVURLResponse, error) {
	archive, err := s.loadIGVArchive(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("IGV evidence unavailable")
	}
	if request.Version != archive.version {
		return nil, ErrIGVEvidenceChanged
	}
	if !archive.reference.Available {
		return nil, fmt.Errorf("IGV reference unavailable: %s", archive.reference.Reason)
	}
	byID := make(map[string]igvTrackCandidate, len(archive.candidates))
	for _, candidate := range archive.candidates {
		byID[candidate.descriptor.ID] = candidate
	}
	expiry := s.cfg.IGV.TrackURLExpiry
	if expiry <= 0 {
		expiry = 10 * time.Minute
	}
	selected := make([]model.IGVTrackURL, 0, len(request.TrackIDs))
	seen := make(map[string]struct{}, len(request.TrackIDs))
	for _, id := range request.TrackIDs {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		candidate, ok := byID[id]
		if !ok || !candidate.descriptor.Available || candidate.objectKey == "" {
			return nil, fmt.Errorf("requested IGV track is unavailable")
		}
		trackURL, err := archive.storage.presignRead(ctx, candidate.objectKey, expiry)
		if err != nil {
			return nil, err
		}
		item := model.IGVTrackURL{ID: id, URL: trackURL}
		if candidate.indexKey != "" {
			indexURL, err := archive.storage.presignRead(ctx, candidate.indexKey, expiry)
			if err != nil {
				return nil, err
			}
			item.IndexURL = indexURL
		}
		selected = append(selected, item)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no IGV tracks selected")
	}
	expiresAt := time.Now().UTC().Add(expiry).Format(time.RFC3339)
	return &model.IGVURLResponse{Tracks: selected, ExpiresAt: expiresAt}, nil
}

func (s *ResultService) loadIGVArchive(ctx context.Context, task *model.Task) (*igvArchive, error) {
	if s.cfg == nil || s.cfg.Storage.Provider != "s3" {
		return nil, fmt.Errorf("COS/S3 archive storage is not configured")
	}
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	if _, err := uuid.Parse(task.UUID); err != nil {
		return nil, fmt.Errorf("invalid task UUID")
	}
	resolvedTask := *task
	if resolvedTask.ExecutionAttemptID == "" {
		resolvedTask.ExecutionAttemptID = resolvedTask.UUID
	}
	if _, err := uuid.Parse(resolvedTask.ExecutionAttemptID); err != nil {
		return nil, fmt.Errorf("invalid execution attempt UUID")
	}
	if _, err := uuid.Parse(task.ExternalOrgID); err != nil {
		return nil, fmt.Errorf("invalid organization UUID")
	}
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	prefix := resultPackagePrefix(&resolvedTask)
	objects, err := storage.list(ctx, prefix+"/")
	if err != nil {
		return nil, err
	}
	manifestKey, objectByBase, version, err := igvArchiveObjectIndex(prefix, objects)
	if err != nil {
		return nil, err
	}
	manifest, err := readIGVManifest(ctx, storage, manifestKey)
	if err != nil {
		return nil, err
	}
	ref := igvReferenceForTask(s.cfg, &resolvedTask)
	return &igvArchive{
		storage: storage, version: version, reference: ref,
		candidates: igvCandidatesFromManifest(manifest, objectByBase),
	}, nil
}

func igvArchiveObjectIndex(prefix string, objects []s3ObjectInfo) (string, map[string]s3ObjectInfo, string, error) {
	manifestKey := ""
	byBase := make(map[string]s3ObjectInfo)
	duplicateBase := make(map[string]bool)
	hash := sha256.New()
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	for _, object := range objects {
		if _, err := safeResultPackageRelativePath(prefix, object.Key); err != nil {
			return "", nil, "", err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00%d\n", object.Key, object.Size, object.LastModified.UnixNano())
		base := strings.ToLower(path.Base(object.Key))
		if base == "outputs.resolved.json" {
			if manifestKey != "" {
				return "", nil, "", fmt.Errorf("archive has multiple result manifests")
			}
			manifestKey = object.Key
		}
		if existing, exists := byBase[base]; exists && existing.Key != object.Key {
			duplicateBase[base] = true
		} else {
			byBase[base] = object
		}
	}
	for base := range duplicateBase {
		delete(byBase, base)
	}
	if manifestKey == "" {
		return "", nil, "", fmt.Errorf("archive is missing outputs.resolved.json")
	}
	return manifestKey, byBase, hex.EncodeToString(hash.Sum(nil)), nil
}

func readIGVManifest(ctx context.Context, storage *s3Storage, key string) (map[string]interface{}, error) {
	reader, err := storage.open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxIGVManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxIGVManifestBytes {
		return nil, fmt.Errorf("result manifest is too large")
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid result manifest")
	}
	return manifest, nil
}

func igvReferenceForTask(cfg *config.Config, task *model.Task) model.IGVReferenceResponse {
	base := resultReferenceForTask(cfg, task)
	response := model.IGVReferenceResponse{ID: base.DeclaredID, Available: base.Available, Reason: base.Reason}
	if !base.Available {
		return response
	}
	var reference config.IGVReferenceConfig
	if base.DeclaredID == "hg19" {
		reference = cfg.IGV.HG19
	} else {
		reference = cfg.IGV.HG38
	}
	response.FASTAURL = reference.FASTAURL
	response.IndexURL = reference.FAIURL
	if cfg.IGV.ReferenceProxyBaseURL != "" {
		base := cfg.IGV.ReferenceProxyBaseURL + "/tasks/" + url.PathEscape(task.UUID) + "/results/igv/reference/"
		attempt := task.ExecutionAttemptID
		if attempt == "" {
			attempt = task.UUID
		}
		query := "?attempt=" + url.QueryEscape(attempt)
		response.FASTAURL = base + "fasta" + query
		response.IndexURL = base + "fai" + query
	}
	response.AliasURL = reference.AliasURL
	response.CytobandURL = reference.CytobandURL
	response.GeneTrackURL = reference.GeneTrackURL
	response.GeneTrackIndexURL = reference.GeneTrackIndexURL
	return response
}

func igvCandidatesFromManifest(manifest map[string]interface{}, objectByBase map[string]s3ObjectInfo) []igvTrackCandidate {
	bams := manifestFileRefs(manifest, "bam")
	bais := manifestFileRefs(manifest, "bai")
	members := manifestStringValues(manifest, "members")
	tracks := make([]igvTrackCandidate, 0, len(bams)+3)
	for index, raw := range bams {
		bam, ok := archiveObjectForRef(raw, objectByBase)
		if !ok {
			continue
		}
		role := manifestMemberRole(members, index, len(bams) > 1)
		candidate := igvTrackCandidate{descriptor: model.IGVTrackDescriptor{
			ID: fmt.Sprintf("bam:%d", index), Name: evidenceTrackName(role, "BAM"), Type: "alignment", Format: "bam",
			MemberID: role, MemberRole: role, Available: true, HasIndex: false,
		}, objectKey: bam.Key}
		if indexKey, exists := matchingIndexForRef(raw, bais, objectByBase); exists {
			candidate.indexKey = indexKey
			candidate.descriptor.HasIndex = true
		} else {
			candidate.descriptor.Available = false
			candidate.descriptor.Reason = "BAM 索引缺失"
		}
		tracks = append(tracks, candidate)
	}
	if raw, ok := firstManifestFileRef(manifest, "vcf_raw"); ok {
		if vcf, exists := archiveObjectForRef(raw, objectByBase); exists {
			candidate := igvTrackCandidate{descriptor: model.IGVTrackDescriptor{
				ID: "vcf:primary", Name: "联合变异 VCF", Type: "variant", Format: "vcf", Available: true,
			}, objectKey: vcf.Key}
			if indexKey, exists := matchingIndexForRef(raw, manifestFileRefs(manifest, "vcf_raw_tbi"), objectByBase); exists {
				candidate.indexKey = indexKey
				candidate.descriptor.HasIndex = true
			} else {
				candidate.descriptor.Available = false
				candidate.descriptor.Reason = "VCF tabix 索引缺失"
			}
			tracks = append(tracks, candidate)
		}
	}
	if raw, ok := firstManifestFileRef(manifest, "bed"); ok {
		if bed, exists := archiveObjectForRef(raw, objectByBase); exists {
			tracks = append(tracks, igvTrackCandidate{descriptor: model.IGVTrackDescriptor{
				ID: "bed:target", Name: "目标区域 BED", Type: "annotation", Format: "bed", Available: true,
			}, objectKey: bed.Key})
		}
	}
	return tracks
}

func evidenceTrackName(role, suffix string) string {
	if role == "" || role == "proband" {
		return "先证者 " + suffix
	}
	switch role {
	case "father":
		return "父亲 " + suffix
	case "mother":
		return "母亲 " + suffix
	default:
		return role + " " + suffix
	}
}

func manifestMemberRole(members []string, index int, trio bool) string {
	if index >= 0 && index < len(members) && strings.TrimSpace(members[index]) != "" {
		return strings.TrimSpace(members[index])
	}
	return memberRoleAt(nil, index, trio)
}

func firstManifestFileRef(manifest map[string]interface{}, key string) (string, bool) {
	refs := manifestFileRefs(manifest, key)
	if len(refs) == 0 {
		return "", false
	}
	return refs[0], true
}

func manifestFileRefs(manifest map[string]interface{}, key string) []string {
	return manifestStringValues(manifest, key)
}

func manifestStringValues(value interface{}, wantedKey string) []string {
	var values []string
	var walk func(interface{})
	walk = func(node interface{}) {
		switch typed := node.(type) {
		case map[string]interface{}:
			for key, child := range typed {
				if key == wantedKey {
					values = append(values, stringsFromManifestValue(child)...)
				}
				walk(child)
			}
		case []interface{}:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return values
}

func stringsFromManifestValue(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{typed}
	case []interface{}:
		values := make([]string, 0, len(typed))
		for _, child := range typed {
			values = append(values, stringsFromManifestValue(child)...)
		}
		return values
	default:
		return nil
	}
}

func archiveObjectForRef(ref string, objectByBase map[string]s3ObjectInfo) (s3ObjectInfo, bool) {
	base := archiveReferenceBaseName(ref)
	if base == "" {
		return s3ObjectInfo{}, false
	}
	object, ok := objectByBase[strings.ToLower(base)]
	return object, ok
}

func archiveReferenceBaseName(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Path != "" {
		trimmed = parsed.Path
	}
	base := path.Base(strings.ReplaceAll(trimmed, `\`, "/"))
	if base == "." || base == "/" || base == "" || base == ".." {
		return ""
	}
	return base
}

func matchingIndexForRef(raw string, indexRefs []string, objectByBase map[string]s3ObjectInfo) (string, bool) {
	dataBase := strings.ToLower(archiveReferenceBaseName(raw))
	for _, ref := range indexRefs {
		index, ok := archiveObjectForRef(ref, objectByBase)
		if !ok {
			continue
		}
		indexBase := strings.ToLower(path.Base(index.Key))
		if indexBase == dataBase+".bai" || indexBase == dataBase+".tbi" {
			return index.Key, true
		}
		if strings.TrimSuffix(dataBase, ".bam")+".bai" == indexBase || strings.TrimSuffix(dataBase, ".vcf.gz")+".vcf.gz.tbi" == indexBase {
			return index.Key, true
		}
	}
	return "", false
}
