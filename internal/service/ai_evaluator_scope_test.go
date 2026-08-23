package service

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newAIEvaluatorScopeTest(t *testing.T) (*AIEvaluator, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	previousDB := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previousDB })
	return NewAIEvaluator(&config.Config{}), mock
}

func TestAIEvaluatorResolvesOnlyOwnedGeneList(t *testing.T) {
	evaluator, mock := newAIEvaluatorScopeTest(t)
	query := `SELECT * FROM "gene_lists" WHERE id = $1 AND created_by = $2 ORDER BY "gene_lists"."id" LIMIT $3`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("list-1", uint(42), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_by", "genes_json"}).
			AddRow("list-1", 42, `["BRCA1", "BRCA2"]`))

	genes, err := evaluator.resolveGeneSet(AIFilter{GeneListID: "list-1", Genes: []string{" TP53 ", ""}}, model.OverlayActor{UserID: 42})
	if err != nil {
		t.Fatalf("resolveGeneSet: %v", err)
	}
	for _, gene := range []string{"BRCA1", "BRCA2", "TP53"} {
		if !genes[gene] {
			t.Fatalf("resolved genes missing %s: %#v", gene, genes)
		}
	}
	if len(genes) != 3 {
		t.Fatalf("resolved genes = %#v, want exactly 3 entries", genes)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestAIEvaluatorRejectsAnotherUsersGeneList(t *testing.T) {
	evaluator, mock := newAIEvaluatorScopeTest(t)
	query := `SELECT * FROM "gene_lists" WHERE id = $1 AND created_by = $2 ORDER BY "gene_lists"."id" LIMIT $3`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("other-list", uint(42), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	genes, err := evaluator.resolveGeneSet(AIFilter{GeneListID: "other-list"}, model.OverlayActor{UserID: 42})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("resolveGeneSet error = %v, want record not found", err)
	}
	if genes != nil {
		t.Fatalf("resolved genes = %#v, want nil", genes)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestAIEvaluatorRejectsMalformedGeneListData(t *testing.T) {
	evaluator, mock := newAIEvaluatorScopeTest(t)
	query := `SELECT * FROM "gene_lists" WHERE id = $1 AND created_by = $2 ORDER BY "gene_lists"."id" LIMIT $3`
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("broken-list", uint(42), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_by", "genes_json"}).
			AddRow("broken-list", 42, `{not-json`))

	genes, err := evaluator.resolveGeneSet(AIFilter{GeneListID: "broken-list"}, model.OverlayActor{UserID: 42})
	if err == nil || !strings.Contains(err.Error(), "invalid gene list data") {
		t.Fatalf("resolveGeneSet error = %v, want invalid data error", err)
	}
	if genes != nil {
		t.Fatalf("resolved genes = %#v, want nil", genes)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestFilterVariantsEmptySelectedGeneListMatchesNothing(t *testing.T) {
	variants := []model.SNVIndel{{Gene: "BRCA1", ACMGClassification: model.ACMGPathogenic}}
	filtered := filterVariants(variants, AIFilter{GeneListID: "empty-list"}, map[string]bool{})
	if len(filtered) != 0 {
		t.Fatalf("filterVariants returned %#v for an empty selected gene list", filtered)
	}
}
