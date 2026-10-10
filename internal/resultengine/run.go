package resultengine

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

// Spill records keep CSV bytes verbatim, avoiding repeated JSON expansion of
// every quoted CSV cell. Runs are private, ephemeral files, never public data.
func writeEntry(w io.Writer, v sortEntry) error {
	var row []byte
	var err error
	if v.Row != nil {
		row, err = json.Marshal(v.Row)
		if err != nil {
			return err
		}
	}
	var header [30]byte
	if v.Key.Missing {
		header[0] = 1
	}
	if v.Key.Number != nil {
		header[1] = 1
		binary.LittleEndian.PutUint64(header[10:18], math.Float64bits(*v.Key.Number))
	}
	binary.LittleEndian.PutUint64(header[2:10], uint64(v.Ordinal))
	binary.LittleEndian.PutUint32(header[18:22], uint32(len(v.Key.Text)))
	binary.LittleEndian.PutUint32(header[22:26], uint32(len(v.CSV)))
	binary.LittleEndian.PutUint32(header[26:30], uint32(len(row)))
	if _, err = w.Write(header[:]); err != nil {
		return err
	}
	for _, b := range [][]byte{[]byte(v.Key.Text), []byte(v.CSV), row} {
		if _, err = w.Write(b); err != nil {
			return err
		}
	}
	return nil
}
func readEntry(r io.Reader, v *sortEntry) error {
	var header [30]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	*v = sortEntry{Ordinal: int64(binary.LittleEndian.Uint64(header[2:10])), Key: sortKey{Missing: header[0] != 0}}
	if header[1] != 0 {
		v.Key.Number = numFloat(math.Float64frombits(binary.LittleEndian.Uint64(header[10:18])))
	}
	chunks := make([][]byte, 3)
	for i, offset := range []int{18, 22, 26} {
		size := binary.LittleEndian.Uint32(header[offset : offset+4])
		if size > 32<<20 {
			return fmt.Errorf("invalid spill record size")
		}
		chunks[i] = make([]byte, int(size))
		if _, err := io.ReadFull(r, chunks[i]); err != nil {
			return err
		}
	}
	v.Key.Text, v.CSV = string(chunks[0]), string(chunks[1])
	if len(chunks[2]) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(chunks[2]))
		decoder.UseNumber()
		return decoder.Decode(&v.Row)
	}
	return nil
}
func numFloat(v float64) *float64 { return &v }
