package service

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
)

func TestCanMutateDataAsset(t *testing.T) {
	tests := []struct {
		name  string
		asset *model.DataAsset
		actor model.OverlayActor
		want  bool
	}{
		{
			name:  "organization member may mutate shared asset",
			asset: &model.DataAsset{ExternalOrgID: "org-a", CreatedBy: 10},
			actor: model.OverlayActor{OrgID: "org-a", UserID: 20},
			want:  true,
		},
		{
			name:  "other organization may not mutate asset",
			asset: &model.DataAsset{ExternalOrgID: "org-a", CreatedBy: 10},
			actor: model.OverlayActor{OrgID: "org-b", UserID: 10},
			want:  false,
		},
		{
			name:  "standalone owner may mutate asset",
			asset: &model.DataAsset{CreatedBy: 10},
			actor: model.OverlayActor{UserID: 10},
			want:  true,
		},
		{
			name:  "standalone user may not mutate another users asset",
			asset: &model.DataAsset{CreatedBy: 10},
			actor: model.OverlayActor{UserID: 20},
			want:  false,
		},
		{
			name:  "standalone actor may not mutate organization asset",
			asset: &model.DataAsset{ExternalOrgID: "org-a", CreatedBy: 10},
			actor: model.OverlayActor{UserID: 10},
			want:  false,
		},
		{
			name:  "platform administrator may mutate any asset",
			asset: &model.DataAsset{ExternalOrgID: "org-a", CreatedBy: 10},
			actor: model.OverlayActor{Role: string(model.SystemRoleSuperAdmin), UserID: 99},
			want:  true,
		},
		{
			name:  "nil asset is rejected",
			asset: nil,
			actor: model.OverlayActor{Role: string(model.SystemRoleSuperAdmin)},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canMutateDataAsset(tt.asset, tt.actor); got != tt.want {
				t.Fatalf("canMutateDataAsset() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarkAssetDeletingAllowsMissingUploadFile(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	uploadFileID := uint(77)
	asset := &model.DataAsset{ID: 42, UploadFileID: &uploadFileID, Status: model.FileStatusCompleted}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "data_assets" WHERE "data_assets"."id" = $1 ORDER BY "data_assets"."id" LIMIT $2 FOR UPDATE`)).
		WithArgs(uint(42), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "upload_file_id", "status"}).
			AddRow(42, uploadFileID, model.FileStatusCompleted))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "upload_files" WHERE "upload_files"."id" = $1 ORDER BY "upload_files"."id" LIMIT $2 FOR UPDATE`)).
		WithArgs(uploadFileID, 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "data_assets" SET "status"=$1,"updated_at"=$2 WHERE "id" = $3`)).
		WithArgs(model.FileStatusDeleting, sqlmock.AnyArg(), uint(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	state, err := markAssetDeleting(db, asset)
	if err != nil {
		t.Fatalf("markAssetDeleting: %v", err)
	}
	if state.hasFile {
		t.Fatal("missing upload file should not be recorded as present")
	}
	if state.assetStatus != model.FileStatusCompleted {
		t.Fatalf("asset status = %q, want %q", state.assetStatus, model.FileStatusCompleted)
	}
	if asset.Status != model.FileStatusDeleting {
		t.Fatalf("asset status after lock = %q, want %q", asset.Status, model.FileStatusDeleting)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
