package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/gin-gonic/gin"
)

func bindQueryForTest(target any, rawQuery string) error {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "/?"+rawQuery, nil)
	return context.ShouldBindQuery(target)
}

func TestListQueriesAllowOmittedPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	factories := []struct {
		name string
		new  func() any
	}{
		{"users", func() any { return &model.UserListQuery{} }},
		{"samples", func() any { return &model.SampleListQuery{} }},
		{"tasks", func() any { return &model.TaskListQuery{} }},
		{"pipelines", func() any { return &model.PipelineListQuery{} }},
		{"projects", func() any { return &model.ProjectListQuery{} }},
		{"pedigrees", func() any { return &model.PedigreeListQuery{} }},
		{"gene lists", func() any { return &model.GeneListListQuery{} }},
		{"upload jobs", func() any { return &model.UploadJobListQuery{} }},
		{"upload files", func() any { return &model.UploadFileListQuery{} }},
		{"result imports", func() any { return &model.ResultImportBatchListQuery{} }},
		{"snv", func() any { return &model.SNVIndelListQuery{} }},
		{"cnv segment", func() any { return &model.CNVSegmentListQuery{} }},
		{"cnv exon", func() any { return &model.CNVExonListQuery{} }},
		{"str", func() any { return &model.STRListQuery{} }},
		{"mei", func() any { return &model.MEIListQuery{} }},
		{"mt", func() any { return &model.MTListQuery{} }},
		{"upd", func() any { return &model.UPDListQuery{} }},
		{"roh", func() any { return &model.ROHListQuery{} }},
	}

	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			if err := bindQueryForTest(factory.new(), ""); err != nil {
				t.Fatalf("omitted pagination should use handler defaults: %v", err)
			}
		})
	}
}

func TestListQueriesStillRejectInvalidPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, rawQuery := range []string{"page=-1", "page_size=101"} {
		if err := bindQueryForTest(&model.UploadFileListQuery{}, rawQuery); err == nil {
			t.Fatalf("expected invalid pagination %q to fail validation", rawQuery)
		}
	}
}
