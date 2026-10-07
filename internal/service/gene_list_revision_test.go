package service

import (
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
	"testing"
)

func TestGeneListStaleRevisionCannotWrite(t *testing.T) {
	_, mock := newAIEvaluatorScopeTest(t)
	s := NewGeneListService(&config.Config{})
	mock.ExpectQuery(`SELECT .*gene_lists`).WillReturnRows(sqlmock.NewRows([]string{"id", "created_by", "revision"}).AddRow("list", 1, 3))
	_, e := s.Update("list", &model.GeneListUpdateRequest{ExpectedRevision: 2}, model.OverlayActor{UserID: 1})
	if !errors.Is(e, ErrResourceConflict) {
		t.Fatalf("stale update error: %v", e)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestGeneListClearDescriptionAndDeduplicate(t *testing.T) {
	_, mock := newAIEvaluatorScopeTest(t)
	s := NewGeneListService(&config.Config{})
	mock.ExpectQuery(`SELECT .*gene_lists`).WillReturnRows(sqlmock.NewRows([]string{"id", "created_by", "revision", "name", "description", "disease_category", "genes_json"}).AddRow("list", 1, 3, "Panel", "old", "old disease", `["OLD"]`))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT count.*gene_lists`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`UPDATE "gene_lists" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	empty := ""
	r, e := s.Update("list", &model.GeneListUpdateRequest{ExpectedRevision: 3, Description: &empty, DiseaseCategory: &empty, Genes: []string{" brca1 ", "BRCA1", "tp53"}}, model.OverlayActor{UserID: 1})
	if e != nil {
		t.Fatal(e)
	}
	if r.Description != "" || r.DiseaseCategory != "" || r.Revision != 4 || len(r.Genes) != 2 {
		t.Fatalf("incorrect update: %+v", r)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
