package service

import (
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"testing"
)

func TestDeletedBEDCannotRegisterNewReference(t *testing.T) {
	_, m := newAIEvaluatorScopeTest(t)
	db := database.GetDB()
	m.ExpectQuery(`SELECT .*data_assets.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "read_type", "status", "validation_status"}).AddRow(7, "bed", "deleted", "valid"))
	if e := lockTaskAssets(db, []model.TaskDataAsset{{AssetID: 7}}); e == nil {
		t.Fatal("deleted BED accepted")
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestBEDReferencesBlockDelete(t *testing.T) {
	_, m := newAIEvaluatorScopeTest(t)
	db := database.GetDB()
	m.ExpectQuery(`SELECT .*pipelines`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("pipeline-1", "Panel"))
	m.ExpectQuery(`SELECT .*cnv_baselines`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	m.ExpectQuery(`SELECT .*tasks`).WillReturnRows(sqlmock.NewRows([]string{"uuid"}))
	e := checkBEDReferences(db, &model.DataAsset{ID: 7, ReadType: model.ReadTypeBed})
	var conflict *ResourceInUseError
	if !errors.As(e, &conflict) || len(conflict.References) != 1 || conflict.References[0].ID != "pipeline-1" {
		t.Fatalf("missing references: %v", e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
