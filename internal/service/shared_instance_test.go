package service

import (
	"github.com/SchemaBio/Octopus/internal/model"
	"testing"
)

func TestSharedInstanceResourceAccessDoesNotCrossSaaSTenants(t *testing.T) {
	actor := model.OverlayActor{UserID: 1, Role: "USER", SharedInstance: true}
	for _, org := range []string{"", "saas-org"} {
		want := org == ""
		asset := &model.DataAsset{CreatedBy: 2, ExternalOrgID: org}
		job := &model.UploadJob{UserID: 2, ExternalOrgID: org}
		if canMutateDataAsset(asset, actor) != want || taskDataAssetUseAllowed(asset, actor) != want {
			t.Fatalf("asset sharing crossed scope %q", org)
		}
		if uploadJobAccessAllowed(job, actor) != want || uploadJobUseAllowed(job, actor) != want {
			t.Fatalf("upload sharing crossed scope %q", org)
		}
	}
	actor.SharedInstance = false
	if uploadJobAccessAllowed(&model.UploadJob{UserID: 2}, actor) {
		t.Fatal("SaaS/legacy access silently widened")
	}
}
