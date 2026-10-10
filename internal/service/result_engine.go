package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/resultengine"
)

func (s *ResultService) resultEngine() (*resultengine.Engine, error) {
	root := s.cfg.ResultQuery.CacheDir
	temp := s.cfg.ResultQuery.TempDir
	if temp == "" {
		temp = filepath.Join(root, "tmp")
	}
	if err := os.MkdirAll(temp, 0750); err != nil {
		return nil, err
	}
	s.engineOnce.Do(func() { s.engine = resultengine.New(root, s.cfg.ResultQuery.AssessmentDir, temp) })
	return s.engine, nil
}
func wireRequest(wire interface{}) (resultengine.Request, error) {
	encoded, err := json.Marshal(wire)
	if err != nil {
		return resultengine.Request{}, err
	}
	if len(encoded) > 16<<20 {
		return resultengine.Request{}, fmt.Errorf("request_too_large")
	}
	var q resultengine.Request
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	err = decoder.Decode(&q)
	return q, err
}
func (s *ResultService) queryResultEngine(ctx context.Context, wire parquetQueryWireRequest) (*model.ParquetQueryResponse, error) {
	q, err := wireRequest(wire)
	if err != nil {
		return nil, err
	}
	engine, err := s.resultEngine()
	if err != nil {
		return nil, err
	}
	r, err := engine.Query(ctx, q)
	if err != nil {
		if err.Error() == ErrParquetIncomplete.Error() {
			return nil, ErrParquetIncomplete
		}
		return nil, err
	}
	encoded, encodeErr := json.Marshal(r)
	if encodeErr != nil || len(encoded) > 16<<20 {
		return nil, fmt.Errorf("invalid Parquet query response")
	}
	return &model.ParquetQueryResponse{Items: r.Items, Total: r.Total, RowCount: r.RowCount, Offset: r.Offset, Limit: r.Limit, Columns: r.Columns, ColumnTypes: r.ColumnTypes, FieldProfileVersion: r.FieldProfileVersion}, nil
}
func (s *ResultService) prepareResultEngine(ctx context.Context, wire resultengine.Request) (*parquetPrepareResponse, error) {
	q, err := wireRequest(wire)
	if err != nil {
		return nil, err
	}
	engine, err := s.resultEngine()
	if err != nil {
		return nil, err
	}
	p, err := engine.Prepare(ctx, q)
	if err != nil {
		return nil, err
	}
	return &parquetPrepareResponse{AssessmentFile: p.AssessmentFile, Profile: p.Profile, Rows: p.Rows}, nil
}

type exportFileBody struct {
	*os.File
	path string
}

func (b *exportFileBody) Close() error {
	err := b.File.Close()
	removeErr := os.Remove(b.path)
	if err != nil {
		return err
	}
	return removeErr
}
func (s *ResultService) exportResultEngine(ctx context.Context, wire parquetQueryWireRequest) (*http.Response, error) {
	q, err := wireRequest(wire)
	if err != nil {
		return nil, err
	}
	engine, err := s.resultEngine()
	if err != nil {
		return nil, err
	}
	path, err := engine.Export(ctx, q)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/csv; charset=utf-8"}}, ContentLength: info.Size(), Body: &exportFileBody{File: file, path: path}}, nil
}
