package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SchemaBio/Octopus/internal/model"
)

func archiveTransport(archive *igvArchive) string {
	if archive.storage == nil {
		return "api"
	}
	return ""
}

func localIGVReferenceID(task *model.Task) string {
	if id := declaredReferenceID(task.InputJSON); id != "" {
		return id
	}
	// WDL input snapshots use qualified keys such as SingleWES.fasta.
	var inputs map[string]interface{}
	_ = json.Unmarshal([]byte(task.InputJSON), &inputs)
	for key, value := range inputs {
		if strings.HasSuffix(key, ".fasta") {
			if name, ok := value.(string); ok {
				return normalizeReferenceID(name)
			}
		}
	}
	return ""
}

// Serve only administrator-configured reference files, never a user-supplied
// task input path. The input snapshot selects the assembly, not a filesystem path.
func (s *ResultService) localIGVReferencePath(task *model.Task) (string, error) {
	var filename string
	switch localIGVReferenceID(task) {
	case "hg19":
		filename = s.cfg.IGV.HG19.FASTAPath
	case "hg38":
		filename = s.cfg.IGV.HG38.FASTAPath
	}
	if !filepath.IsAbs(filename) {
		return "", fmt.Errorf("local IGV reference is not configured")
	}
	for _, name := range []string{filename, filename + ".fai"} {
		info, err := os.Stat(name)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return "", fmt.Errorf("local IGV reference unavailable")
		}
	}
	return filename, nil
}

func (s *ResultService) localIGVReference(task *model.Task) model.IGVReferenceResponse {
	ref := model.IGVReferenceResponse{ID: localIGVReferenceID(task)}
	if _, err := s.localIGVReferencePath(task); err != nil {
		ref.Reason = "该执行的本地参考 FASTA 与 FAI 尚未配置或不可读"
		return ref
	}
	base := "/v1/tasks/" + url.PathEscape(task.UUID) + "/results/igv/reference/"
	query := "?attempt=" + url.QueryEscape(executionAttempt(task))
	ref.Available, ref.FASTAURL, ref.IndexURL = true, base+"fasta"+query, base+"fai"+query
	return ref
}

func (s *ResultService) loadLocalIGVArchive(task *model.Task) (*igvArchive, error) {
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	dir, err := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
	if err != nil {
		return nil, err
	}
	raw, err := (&Archiver{cfg: s.cfg}).readArchiveJSONFile(dir, "outputs.resolved.json")
	if err != nil {
		return nil, err
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	objects := make([]s3ObjectInfo, 0, len(entries))
	for _, entry := range entries {
		filename, err := resolveArchiveRegularFile(dir, filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		info, err := os.Stat(filename)
		if err != nil {
			continue
		}
		objects = append(objects, s3ObjectInfo{Key: "local-igv/" + entry.Name(), Size: info.Size(), LastModified: info.ModTime()})
	}
	_, index, version, err := igvArchiveObjectIndex("local-igv", objects)
	if err != nil {
		return nil, err
	}
	for base, object := range index {
		object.Key = filepath.ToSlash(filepath.Join(dir, filepath.Base(object.Key)))
		index[base] = object
	}
	version = fmt.Sprintf("%x", sha256.Sum256(append(raw, []byte(executionAttempt(task)+version)...)))
	return &igvArchive{version: version, reference: s.localIGVReference(task), candidates: igvCandidatesFromManifest(manifest, index)}, nil
}

func (s *ResultService) OpenLocalIGVTrack(task *model.Task, id, asset, attempt, version, requestedRange string) (*IGVReferenceStream, error) {
	if !s.localBrowserStorage() {
		return nil, fmt.Errorf("local IGV unavailable")
	}
	if attempt != executionAttempt(task) {
		return nil, ErrIGVEvidenceChanged
	}
	archive, err := s.loadLocalIGVArchive(task)
	if err != nil {
		return nil, err
	}
	if version != archive.version {
		return nil, ErrIGVEvidenceChanged
	}
	for _, candidate := range archive.candidates {
		if candidate.descriptor.ID != id || !candidate.descriptor.Available {
			continue
		}
		filename := candidate.objectKey
		if asset == "index" {
			filename = candidate.indexKey
		} else if asset != "data" {
			return nil, fmt.Errorf("unknown IGV asset")
		}
		if filename == "" {
			break
		}
		// Recheck containment when opening, including symlinks changed since discovery.
		dir, err := taskArchiveDir(s.cfg.Task.ArchiveDir, task)
		if err != nil {
			return nil, err
		}
		filename, err = resolveArchiveRegularFile(dir, filename)
		if err != nil {
			return nil, err
		}
		return openLocalIGVFile(filename, requestedRange, asset == "data" && candidate.descriptor.Format == "bam", 32<<20)
	}
	return nil, fmt.Errorf("IGV track unavailable")
}

type localIGVReader struct {
	io.Reader
	io.Closer
}

func openLocalIGVFile(filename, requestedRange string, requireRange bool, limit int64) (*IGVReferenceStream, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*IGVReferenceStream, error) { f.Close(); return nil, err }
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return fail(fmt.Errorf("invalid IGV file"))
	}
	size := info.Size()
	start, end := int64(0), size-1
	if requestedRange == "" {
		if requireRange || size > limit {
			return fail(ErrIGVReferenceRange)
		}
	} else {
		if !strings.HasPrefix(requestedRange, "bytes=") || strings.Contains(requestedRange, ",") {
			return fail(ErrIGVReferenceRange)
		}
		parts := strings.Split(strings.TrimPrefix(requestedRange, "bytes="), "-")
		if len(parts) != 2 || parts[0] == "" {
			return fail(ErrIGVReferenceRange)
		}
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || start < 0 || start >= size {
			return fail(ErrIGVReferenceRange)
		}
		if parts[1] != "" {
			end, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil || end < start {
				return fail(ErrIGVReferenceRange)
			}
		}
		if end >= size {
			end = size - 1
		}
		if end-start+1 > limit {
			return fail(ErrIGVReferenceRange)
		}
	}
	contentRange := ""
	if requestedRange != "" {
		contentRange = fmt.Sprintf("bytes %d-%d/%d", start, end, size)
	}
	return &IGVReferenceStream{Body: localIGVReader{Reader: io.NewSectionReader(f, start, end-start+1), Closer: f}, Length: end - start + 1, ContentRange: contentRange}, nil
}
