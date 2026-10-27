// Package exampledata defines the validated, portable example-data contract.
// It does not provision data or make a template available to users.
package exampledata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"regexp"
	"strings"
	"time"
)

type Role string

const (
	Read1    Role = "read1"
	Read2    Role = "read2"
	BAM      Role = "bam"
	BAMIndex Role = "bam_index"
	Result   Role = "result"
)

// Object paths are relative to the immutable template version, never S3 keys.
type Object struct {
	Path   string `json:"path"`
	Role   Role   `json:"role"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Version         string          `json:"version"`
	SampleID        string          `json:"sample_id"`
	ReferenceGenome string          `json:"reference_genome"`
	Pipeline        string          `json:"pipeline"`
	PipelineVersion string          `json:"pipeline_version"`
	Objects         []Object        `json:"objects"`
	Sample          Sample          `json:"sample"`
	Outputs         json.RawMessage `json:"outputs"`
}

// Sample is deliberately independent of database identities and organization data.
type Sample struct {
	Gender            string          `json:"gender"`
	Age               *int            `json:"age,omitempty"`
	SampleType        string          `json:"sample_type"`
	ClinicalDiagnosis string          `json:"clinical_diagnosis"`
	HPOTerms          json.RawMessage `json:"hpo_terms"`
}

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Validate checks the declared inventory and rejects common archive filenames.
// Publication must additionally inspect file contents before enabling a template.
func (m Manifest) Validate() error {
	if !versionPattern.MatchString(m.Version) || m.Version == "." || m.Version == ".." {
		return errors.New("invalid template version")
	}
	if strings.TrimSpace(m.SampleID) == "" || strings.TrimSpace(m.ReferenceGenome) == "" || strings.TrimSpace(m.Pipeline) == "" || strings.TrimSpace(m.PipelineVersion) == "" {
		return errors.New("sample, reference genome and pipeline identity are required")
	}
	counts := map[Role]int{}
	seen := map[string]bool{}
	basenames := map[string]bool{}
	for _, obj := range m.Objects {
		if err := validateObject(obj); err != nil {
			return err
		}
		if seen[obj.Path] {
			return fmt.Errorf("duplicate object path: %s", obj.Path)
		}
		seen[obj.Path] = true
		base := path.Base(obj.Path)
		if basenames[base] || strings.EqualFold(base, "outputs.resolved.json") {
			return errors.New("duplicate or reserved object basename")
		}
		basenames[base] = true
		counts[obj.Role]++
	}
	if counts[Read1] != 1 || counts[Read2] != 1 || counts[BAM] != 1 || counts[BAMIndex] != 1 || counts[Result] == 0 {
		return errors.New("template requires one read pair, one BAM and index, and result files")
	}
	return nil
}

func validateObject(o Object) error {
	if o.Path == "" || strings.ContainsAny(o.Path, "\\\x00") || strings.HasPrefix(o.Path, "/") || path.Clean(o.Path) != o.Path || o.Path == "." || strings.HasPrefix(o.Path, "../") {
		return errors.New("invalid object path")
	}
	if o.Size <= 0 || o.Size == math.MaxInt64 {
		return errors.New("object size must be positive")
	}
	if len(o.SHA256) != 64 || strings.ToLower(o.SHA256) != o.SHA256 {
		return errors.New("invalid SHA256")
	}
	if _, err := hex.DecodeString(o.SHA256); err != nil {
		return errors.New("invalid SHA256")
	}
	lower := strings.ToLower(o.Path)
	for _, ext := range []string{".zip", ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tar.xz", ".7z", ".rar"} {
		if strings.HasSuffix(lower, ext) {
			return errors.New("nested archives are not permitted")
		}
	}
	switch o.Role {
	case Read1, Read2:
		if !strings.HasSuffix(lower, ".fastq.gz") && !strings.HasSuffix(lower, ".fq.gz") {
			return errors.New("read must be a gzipped FASTQ")
		}
	case BAM:
		if !strings.HasSuffix(lower, ".bam") {
			return errors.New("invalid BAM path")
		}
	case BAMIndex:
		if !strings.HasSuffix(lower, ".bai") {
			return errors.New("invalid BAM index path")
		}
	case Result:
		if strings.HasSuffix(lower, ".bam") || strings.HasSuffix(lower, ".bai") || strings.HasSuffix(lower, ".cram") || strings.HasSuffix(lower, ".crai") {
			return errors.New("alignment files must not be results")
		}
	default:
		return errors.New("invalid object role")
	}
	return nil
}

// VerifyObject checks the actual bytes, not an S3 ETag, against the inventory.
func VerifyObject(o Object, r io.Reader) error {
	if err := validateObject(o); err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, o.Size+1))
	if err != nil {
		return err
	}
	if n != o.Size || hex.EncodeToString(h.Sum(nil)) != o.SHA256 {
		return errors.New("object size or SHA256 mismatch")
	}
	return nil
}

type Deadlines struct{ Data, BAM, Sample, Result time.Time }

// ApprovalDeadlines never bases retention on copy or retry time.
func ApprovalDeadlines(approvedAt time.Time) (Deadlines, error) {
	if approvedAt.IsZero() {
		return Deadlines{}, errors.New("approval time is required")
	}
	t := approvedAt.UTC()
	return Deadlines{Data: t.Add(168 * time.Hour), BAM: t.Add(168 * time.Hour), Sample: t.Add(720 * time.Hour), Result: t.Add(720 * time.Hour)}, nil
}
