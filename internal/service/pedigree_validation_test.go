package service

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
)

func newPedigreeValidationService(t *testing.T) (*PedigreeService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := newUploadTransactionTestDB(t)
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	return NewPedigreeService(&config.Config{}), mock
}

func TestSetProbandRejectsMemberFromAnotherPedigree(t *testing.T) {
	service, mock := newPedigreeValidationService(t)
	mock.ExpectQuery(`SELECT \* FROM "pedigrees" WHERE id = \$1 ORDER BY "pedigrees"\."id" LIMIT \$2`).
		WithArgs("pedigree-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("pedigree-1"))
	mock.ExpectQuery(`SELECT \* FROM "pedigree_members" WHERE id = \$1 ORDER BY "pedigree_members"\."id" LIMIT \$2`).
		WithArgs("member-2", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "pedigree_id"}).AddRow("member-2", "pedigree-2"))

	if _, err := service.SetProband("pedigree-1", "member-2"); err == nil {
		t.Fatal("SetProband should reject a member from another pedigree")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestValidateLinkedSampleUsesOrganizationScope(t *testing.T) {
	service, mock := newPedigreeValidationService(t)
	mock.ExpectQuery(`SELECT \* FROM "samples" WHERE uuid = \$1 AND external_org_id = \$2 ORDER BY "samples"\."id" LIMIT \$3`).
		WithArgs("sample-from-other-org", "org-current", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "external_org_id"}))

	err := service.validateLinkedSample("sample-from-other-org", model.OverlayActor{UserID: 42, OrgID: "org-current"})
	if err == nil {
		t.Fatal("validateLinkedSample should reject a sample outside the current organization scope")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestValidateParentMembersRejectsSelfAndDuplicateParents(t *testing.T) {
	service := &PedigreeService{}
	if err := service.validateParentMembers("pedigree-1", "member-1", "member-1", ""); err == nil {
		t.Fatal("member must not reference itself as father")
	}
	if err := service.validateParentMembers("pedigree-1", "member-1", "parent-1", "parent-1"); err == nil {
		t.Fatal("father and mother must be different members")
	}
}
