package resultengine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/SchemaBio/Octopus/internal/pathsafe"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/reader"
	"github.com/xitongsys/parquet-go/source"
)

type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type Reader struct {
	ctx     context.Context
	file    source.ParquetFile
	reader  *reader.ParquetReader
	columns []Column
	names   map[string]string
	ordinal int64
	closed  bool
}

// Open uses the footer even when there are no rows. Names come from the external
// schema, before parquet-go's generated Go field names are exposed to callers.
func Open(ctx context.Context, root, path string) (r *Reader, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path, err = pathsafe.ResolveExistingWithin(root, path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("parquet source is not a regular file")
	}
	if err = validateFooter(ctx, path, info.Size()); err != nil {
		return nil, err
	}
	file, err := local.NewLocalFileReader(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if panicValue := recover(); panicValue != nil {
			file.Close()
			r = nil
			err = fmt.Errorf("invalid parquet metadata: %v", panicValue)
		}
	}()
	pr, err := reader.NewParquetReader(file, nil, 1)
	if err != nil {
		file.Close()
		return nil, err
	}
	r = &Reader{ctx: ctx, file: file, reader: pr, names: map[string]string{}, columns: []Column{}}
	for i, schema := range pr.SchemaHandler.SchemaElements {
		info := pr.SchemaHandler.Infos[i]
		r.names[info.InName] = info.ExName
		if i == 0 || schema.GetNumChildren() != 0 {
			continue
		}
		kind := "string"
		switch schema.GetType().String() {
		case "BOOLEAN":
			kind = "boolean"
		case "INT32", "INT64":
			kind = "integer"
		case "FLOAT", "DOUBLE":
			kind = "number"
		}
		r.columns = append(r.columns, Column{Name: info.ExName, Type: kind})
	}
	return r, nil
}
func (r *Reader) Columns() []Column { return append([]Column{}, r.columns...) }
func (r *Reader) NumRows() int64    { return r.reader.GetNumRows() }
func (r *Reader) Ordinal() int64    { return r.ordinal }
func (r *Reader) SkipRows(count int64) error {
	for count > 0 {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		step := count
		if step > 1000 {
			step = 1000
		}
		if err := r.fill(int(step)); err != nil {
			return err
		}
		if err := r.reader.SkipRows(step); err != nil {
			return err
		}
		r.ordinal += step
		count -= step
	}
	return nil
}
func (r *Reader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	r.reader.ReadStop()
	return r.file.Close()
}
func (r *Reader) ReadBatch(limit int) (rows []map[string]interface{}, err error) {
	if err = r.ctx.Err(); err != nil {
		return nil, err
	}
	if r.closed {
		return nil, errors.New("reader closed")
	}
	if r.ordinal >= r.NumRows() {
		return nil, io.EOF
	}
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid batch size")
	}
	if left := r.NumRows() - r.ordinal; int64(limit) > left {
		limit = int(left)
	}
	defer func() {
		if panicValue := recover(); panicValue != nil {
			rows = nil
			err = fmt.Errorf("invalid parquet page: %v", panicValue)
		}
	}()
	if err = r.fill(limit); err != nil {
		return nil, err
	}
	raw, err := r.reader.ReadByNumber(limit)
	if err != nil {
		return nil, err
	}
	if len(raw) != limit {
		return nil, fmt.Errorf("parquet row count mismatch")
	}
	rows = make([]map[string]interface{}, len(raw))
	for i, item := range raw {
		if err = r.ctx.Err(); err != nil {
			return nil, err
		}
		row, ok := r.value(reflect.ValueOf(item)).(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid parquet row")
		}
		rows[i] = row
	}
	r.ordinal += int64(len(rows))
	return rows, nil
}

// parquet-go's ReadRows suppresses page errors. Pre-fill through its error-
// returning page API, and allow the final lookahead EOF only after all declared
// values were consumed; otherwise corruption must never become null evidence.
func (r *Reader) fill(limit int) error {
	for _, column := range r.reader.ColumnBuffers {
		for column.DataTableNumRows < int64(limit) {
			if err := r.ctx.Err(); err != nil {
				return err
			}
			remaining := column.ChunkHeader != nil && column.ChunkHeader.MetaData != nil && column.ChunkReadValues < column.ChunkHeader.MetaData.NumValues
			err := column.ReadPage()
			if err != nil {
				if err == io.EOF && !remaining && column.DataTableNumRows >= int64(limit) {
					break
				}
				return fmt.Errorf("invalid parquet page: %w", err)
			}
		}
	}
	return nil
}
func validateFooter(ctx context.Context, path string, size int64) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("invalid parquet footer: %v", v)
		}
	}()
	if size < 12 {
		return fmt.Errorf("invalid parquet footer")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var magic [4]byte
	var tail [8]byte
	if _, err = f.ReadAt(magic[:], 0); err != nil {
		return err
	}
	if _, err = f.ReadAt(tail[:], size-8); err != nil {
		return err
	}
	length := int64(binary.LittleEndian.Uint32(tail[:4]))
	if string(magic[:]) != "PAR1" || string(tail[4:]) != "PAR1" || length < 1 || length > 16<<20 || length > size-12 {
		return fmt.Errorf("invalid parquet footer")
	}
	file, err := local.NewLocalFileReader(path)
	if err != nil {
		return err
	}
	defer file.Close()
	probe := &reader.ParquetReader{PFile: file}
	if err = probe.ReadFooter(); err != nil {
		return err
	}
	if len(probe.Footer.Schema) > 2048 || probe.Footer.NumRows < 0 {
		return fmt.Errorf("invalid parquet schema or row count")
	}
	rows := int64(0)
	for _, group := range probe.Footer.RowGroups {
		if err = ctx.Err(); err != nil {
			return err
		}
		if group.NumRows < 0 {
			return fmt.Errorf("invalid parquet row group")
		}
		rows += group.NumRows
		for _, column := range group.Columns {
			if column.FilePath != nil && *column.FilePath != "" {
				return fmt.Errorf("external parquet column files are not allowed")
			}
			m := column.MetaData
			if m == nil || m.NumValues < 0 || m.TotalCompressedSize < 0 {
				return fmt.Errorf("invalid parquet column metadata")
			}
			offset := m.DataPageOffset
			if m.DictionaryPageOffset != nil {
				offset = *m.DictionaryPageOffset
			}
			if offset < 4 || offset > size-8-length || m.TotalCompressedSize > size-8-length-offset {
				return fmt.Errorf("parquet column exceeds source bounds")
			}
		}
	}
	if rows != probe.Footer.NumRows {
		return fmt.Errorf("parquet row count mismatch")
	}
	return nil
}
func (r *Reader) value(v reflect.Value) interface{} {
	if !v.IsValid() {
		return nil
	}
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Struct:
		row := map[string]interface{}{}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			name := r.names[field.Name]
			if name == "" {
				name = strings.Split(field.Tag.Get("json"), ",")[0]
			}
			if name == "" {
				name = field.Name
			}
			row[name] = r.value(v.Field(i))
		}
		return row
	case reflect.Map:
		row := map[string]interface{}{}
		iter := v.MapRange()
		for iter.Next() {
			row[fmt.Sprint(iter.Key().Interface())] = r.value(iter.Value())
		}
		return row
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return string(v.Bytes())
		}
		a := make([]interface{}, v.Len())
		for i := range a {
			a[i] = r.value(v.Index(i))
		}
		return a
	default:
		return v.Interface()
	}
}
