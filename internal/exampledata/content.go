package exampledata

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/reader"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// RewriteOutputs rewrites only explicit portable references, never substrings.
// Non-reference strings may not contain transport URLs or absolute file paths.
func RewriteOutputs(raw []byte, objects map[string]string) ([]byte, error) {
	var value interface{}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	var extra interface{}
	if d.Decode(&extra) != io.EOF {
		return nil, errors.New("trailing outputs data")
	}
	var walk func(interface{}) (interface{}, error)
	walk = func(v interface{}) (interface{}, error) {
		switch x := v.(type) {
		case string:
			if strings.HasPrefix(x, "example://objects/") {
				target, ok := objects[strings.TrimPrefix(x, "example://objects/")]
				if !ok {
					return nil, errors.New("unknown output object")
				}
				return target, nil
			}
			if strings.Contains(x, "://") || strings.HasPrefix(x, "/") || strings.Contains(x, `\`) || strings.Contains(x, "organizations/") {
				return nil, errors.New("nonportable output reference")
			}
			return x, nil
		case []interface{}:
			for i, y := range x {
				z, err := walk(y)
				if err != nil {
					return nil, err
				}
				x[i] = z
			}
			return x, nil
		case map[string]interface{}:
			for k, y := range x {
				z, err := walk(y)
				if err != nil {
					return nil, err
				}
				x[k] = z
			}
			return x, nil
		default:
			return v, nil
		}
	}
	value, err := walk(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// ValidateFiles inspects the exact local package bytes. FASTQs must be paired;
// BAM/index and result format signatures are checked before publication.
func (m Manifest) ValidateFiles(root string) error {
	if err := m.Validate(); err != nil {
		return err
	}
	var r1, r2, bam, index string
	refs := map[string]string{}
	for _, o := range m.Objects {
		file := filepath.Join(root, filepath.FromSlash(o.Path))
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("template object is not a regular file")
		}
		resolved, err := filepath.EvalSymlinks(file)
		if err != nil {
			return err
		}
		base, err := filepath.EvalSymlinks(root)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("template object escapes package")
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		err = VerifyObject(o, f)
		f.Close()
		if err != nil {
			return err
		}
		refs[o.Path] = "checked/" + o.Path
		switch o.Role {
		case Read1:
			r1 = file
		case Read2:
			r2 = file
		case BAM:
			bam = file
		case BAMIndex:
			index = file
		case Result:
			if err := validateResult(file); err != nil {
				return err
			}
		}
	}
	if index != bam+".bai" && index != strings.TrimSuffix(bam, ".bam")+".bai" {
		return errors.New("BAM index name does not match BAM")
	}
	if err := checkMagic(bam, true, []byte{'B', 'A', 'M', 1}); err != nil {
		return err
	}
	if err := checkMagic(index, false, []byte{'B', 'A', 'I', 1}); err != nil {
		return err
	}
	if err := checkBAMIndexReferences(bam, index); err != nil {
		return err
	}
	if err := validatePair(r1, r2); err != nil {
		return err
	}
	if len(m.Outputs) == 0 {
		return errors.New("outputs are required")
	}
	if err := m.validateOutputRoles(); err != nil {
		return err
	}
	_, err := RewriteOutputs(m.Outputs, refs)
	return err
}

// A BAM must be declared only in the BAM field, or result ZIP builders could
// accidentally preserve it as another report. Every BAM/index must be declared
// so the existing exact-object retention worker can discover it.
func (m Manifest) validateOutputRoles() error {
	roles := map[string]Role{}
	for _, o := range m.Objects {
		roles[o.Path] = o.Role
	}
	var parsed interface{}
	if err := json.Unmarshal(m.Outputs, &parsed); err != nil {
		return err
	}
	bam, index, parquet, qc := false, false, false, false
	var walk func(interface{}, string) error
	walk = func(v interface{}, field string) error {
		switch x := v.(type) {
		case string:
			if !strings.HasPrefix(x, "example://objects/") {
				return nil
			}
			name := strings.TrimPrefix(x, "example://objects/")
			role, ok := roles[name]
			if !ok {
				return errors.New("unknown output object")
			}
			if role == Read1 || role == Read2 {
				return errors.New("FASTQ must not be in result outputs")
			}
			if (role == BAM && field != "bam") || (role == BAMIndex && field != "bai") || (field == "bam" && role != BAM) || (field == "bai" && role != BAMIndex) {
				return errors.New("output role mismatch")
			}
			bam = bam || role == BAM
			index = index || role == BAMIndex
			parquet = parquet || (role == Result && strings.HasSuffix(strings.ToLower(name), ".parquet"))
		case []interface{}:
			for _, y := range x {
				if err := walk(y, field); err != nil {
					return err
				}
			}
		case map[string]interface{}:
			for key, y := range x {
				if key == "qc_result" {
					if _, ok := y.(map[string]interface{}); ok {
						qc = true
					}
				}
				if err := walk(y, key); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(parsed, ""); err != nil {
		return err
	}
	if !bam || !index || !parquet || !qc {
		return errors.New("outputs require declared BAM, index, Parquet and embedded QC")
	}
	return nil
}

func checkMagic(file string, compressed bool, want []byte) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	var r io.Reader = f
	if compressed {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	}
	b := make([]byte, len(want))
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	if !bytes.Equal(b, want) {
		return errors.New("invalid file signature")
	}
	return nil
}

func validateResult(file string) error {
	lower := strings.ToLower(file)
	if strings.HasSuffix(lower, ".parquet") {
		f, err := local.NewLocalFileReader(file)
		if err != nil {
			return err
		}
		defer f.Close()
		p, err := reader.NewParquetReader(f, nil, 1)
		if err != nil {
			return err
		}
		p.ReadStop()
		return nil
	}
	if strings.HasSuffix(lower, ".vcf.gz") {
		return checkMagic(file, true, []byte("##fileformat=VCF"))
	}
	if strings.HasSuffix(lower, ".tbi") {
		return checkMagic(file, true, []byte{'T', 'B', 'I', 1})
	}
	if strings.HasSuffix(lower, ".png") {
		return checkMagic(file, false, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	}
	// Deliberately closed list: compressed/opaque result containers are not published.
	if !strings.HasSuffix(lower, ".json") && !strings.HasSuffix(lower, ".tsv") && !strings.HasSuffix(lower, ".txt") && !strings.HasSuffix(lower, ".bed") && !strings.HasSuffix(lower, ".cnr") && !strings.HasSuffix(lower, ".cns") {
		return errors.New("unsupported example result format")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 64*1024)
	for {
		n, e := f.Read(buf)
		for _, b := range buf[:n] {
			if b < 32 && b != '\t' && b != '\r' && b != '\n' {
				return errors.New("binary data in textual result")
			}
		}
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
	}
}

func checkBAMIndexReferences(bam, index string) error {
	b, err := os.Open(bam)
	if err != nil {
		return err
	}
	defer b.Close()
	g, err := gzip.NewReader(b)
	if err != nil {
		return err
	}
	defer g.Close()
	var header [8]byte
	if _, err := io.ReadFull(g, header[:]); err != nil {
		return err
	}
	textLength := int64(binary.LittleEndian.Uint32(header[4:]))
	if textLength > 16<<20 {
		return errors.New("BAM header too large")
	}
	if _, err := io.CopyN(io.Discard, g, textLength); err != nil {
		return err
	}
	var count [4]byte
	if _, err := io.ReadFull(g, count[:]); err != nil {
		return err
	}
	references := binary.LittleEndian.Uint32(count[:])
	i, err := os.Open(index)
	if err != nil {
		return err
	}
	defer i.Close()
	if _, err := io.ReadFull(i, header[:]); err != nil {
		return err
	}
	if references == 0 || references != binary.LittleEndian.Uint32(header[4:]) {
		return errors.New("BAM and index reference counts differ")
	}
	return nil
}

func validatePair(left, right string) error {
	open := func(p string) (*os.File, *gzip.Reader, *bufio.Scanner, error) {
		f, e := os.Open(p)
		if e != nil {
			return nil, nil, nil, e
		}
		g, e := gzip.NewReader(f)
		if e != nil {
			f.Close()
			return nil, nil, nil, e
		}
		s := bufio.NewScanner(g)
		s.Buffer(make([]byte, 64*1024), 4<<20)
		return f, g, s, nil
	}
	f1, g1, s1, err := open(left)
	if err != nil {
		return err
	}
	defer f1.Close()
	defer g1.Close()
	f2, g2, s2, err := open(right)
	if err != nil {
		return err
	}
	defer f2.Close()
	defer g2.Close()
	count := 0
	for {
		a, b := s1.Scan(), s2.Scan()
		if a != b {
			return errors.New("FASTQ record counts differ")
		}
		if !a {
			break
		}
		h1, h2 := s1.Text(), s2.Text()
		leftFields, rightFields := strings.Fields(h1), strings.Fields(h2)
		if (len(leftFields) > 0 && strings.HasSuffix(leftFields[0], "/2")) || (len(rightFields) > 0 && strings.HasSuffix(rightFields[0], "/1")) || (len(leftFields) > 1 && strings.HasPrefix(leftFields[1], "2:")) || (len(rightFields) > 1 && strings.HasPrefix(rightFields[1], "1:")) {
			return errors.New("FASTQ read directions are reversed")
		}
		id := func(s string) string {
			v := strings.Fields(s)
			if len(v) == 0 {
				return ""
			}
			return strings.TrimSuffix(strings.TrimSuffix(v[0], "/1"), "/2")
		}
		if !strings.HasPrefix(h1, "@") || !strings.HasPrefix(h2, "@") || id(h1) != id(h2) {
			return errors.New("FASTQ read identities differ")
		}
		var lengths [2]int
		for line := 0; line < 3; line++ {
			if !s1.Scan() || !s2.Scan() {
				return errors.New("truncated FASTQ")
			}
			x, y := s1.Text(), s2.Text()
			if line == 0 {
				lengths = [2]int{len(x), len(y)}
				if lengths[0] == 0 || lengths[1] == 0 {
					return errors.New("empty FASTQ read")
				}
			}
			if line == 1 && (!strings.HasPrefix(x, "+") || !strings.HasPrefix(y, "+")) {
				return errors.New("invalid FASTQ separator")
			}
			if line == 2 && (len(x) != lengths[0] || len(y) != lengths[1]) {
				return errors.New("FASTQ quality length mismatch")
			}
		}
		count++
	}
	if s1.Err() != nil {
		return s1.Err()
	}
	if s2.Err() != nil {
		return s2.Err()
	}
	if count == 0 {
		return fmt.Errorf("empty FASTQ pair")
	}
	return nil
}
