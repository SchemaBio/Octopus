package service

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"testing"
)

func TestSelfHostedSampleCreationDoesNotRequireOrFabricateConsent(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	mock.ExpectQuery(`SELECT count\(\*\) FROM "samples"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "samples"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()
	service := NewSampleService(&config.Config{Edition: "opensource"})
	sample, err := service.CreateSample(context.Background(), &model.SampleCreateRequest{InternalID: "local-no-consent"}, model.OverlayActor{UserID: 1, SharedInstance: true})
	if err != nil {
		t.Fatal(err)
	}
	if sample == nil || sample.IdentityPolicyAcknowledgedAt != nil || sample.IdentityPolicyVersion != "" {
		t.Fatalf("fabricated consent: %#v", sample)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
