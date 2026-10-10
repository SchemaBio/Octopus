package resultengine

import (
	"bufio"
	"io"
	"strings"
)

type csvCell struct {
	Text string `json:"text"`
	Null bool   `json:"null,omitempty"`
}
type csvWriter struct {
	w   *bufio.Writer
	err error
}

func newCSVWriter(w io.Writer) *csvWriter { return &csvWriter{w: bufio.NewWriterSize(w, 65536)} }
func (w *csvWriter) WriteHeader(values []string) error {
	cells := make([]csvCell, len(values))
	for i, v := range values {
		cells[i] = csvCell{Text: v}
	}
	return w.Write(cells)
}
func (w *csvWriter) Write(values []csvCell) error {
	return w.WriteRaw(csvLine(values))
}
func csvLine(values []csvCell) string {
	var builder strings.Builder
	for i, v := range values {
		if i > 0 {
			builder.WriteByte(',')
		}
		if v.Null {
			continue
		}
		s := v.Text
		if s == "" || strings.ContainsAny(s, ",\r\n\"") {
			builder.WriteByte('"')
			builder.WriteString(strings.ReplaceAll(s, `"`, `""`))
			builder.WriteByte('"')
		} else {
			builder.WriteString(s)
		}
	}
	builder.WriteByte('\n')
	return builder.String()
}
func (w *csvWriter) WriteRaw(line string) error { _, w.err = w.w.WriteString(line); return w.err }
func (w *csvWriter) Flush()                     { w.err = w.w.Flush() }
func (w *csvWriter) Error() error               { return w.err }
func csvValue(v interface{}) csvCell {
	if v == nil {
		return csvCell{Null: true}
	}
	s := text(v)
	if _, ok := v.(float64); ok && !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return csvCell{Text: s}
}
