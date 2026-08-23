package service

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestUploadJobAccessAllowed(t *testing.T) {
	tests := []struct {
		name  string
		job   *model.UploadJob
		actor model.OverlayActor
		want  bool
	}{
		{
			name:  "same organization uploader",
			job:   &model.UploadJob{ExternalOrgID: "org-1", UserID: 10},
			actor: model.OverlayActor{OrgID: "org-1", UserID: 10},
			want:  true,
		},
		{
			name:  "same organization different uploader",
			job:   &model.UploadJob{ExternalOrgID: "org-1", UserID: 10},
			actor: model.OverlayActor{OrgID: "org-1", UserID: 20},
			want:  false,
		},
		{
			name:  "different organization rejects original creator",
			job:   &model.UploadJob{ExternalOrgID: "org-1", UserID: 10},
			actor: model.OverlayActor{OrgID: "org-2", UserID: 10},
			want:  false,
		},
		{
			name:  "missing organization rejects SaaS upload creator",
			job:   &model.UploadJob{ExternalOrgID: "org-1", UserID: 10},
			actor: model.OverlayActor{UserID: 10},
			want:  false,
		},
		{
			name:  "standalone creator",
			job:   &model.UploadJob{UserID: 10},
			actor: model.OverlayActor{UserID: 10},
			want:  true,
		},
		{
			name:  "standalone different user",
			job:   &model.UploadJob{UserID: 10},
			actor: model.OverlayActor{UserID: 20},
			want:  false,
		},
		{
			name:  "super admin",
			job:   &model.UploadJob{ExternalOrgID: "org-1", UserID: 10},
			actor: model.OverlayActor{Role: string(model.SystemRoleSuperAdmin)},
			want:  true,
		},
		{
			name:  "nil upload job",
			actor: model.OverlayActor{Role: string(model.SystemRoleSuperAdmin)},
			want:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := uploadJobAccessAllowed(test.job, test.actor); got != test.want {
				t.Fatalf("uploadJobAccessAllowed() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestUploadJobUseAllowed(t *testing.T) {
	job := &model.UploadJob{ExternalOrgID: "org-1", UserID: 10}
	if !uploadJobUseAllowed(job, model.OverlayActor{OrgID: "org-1", UserID: 20}) {
		t.Fatal("another member of the same organization should be able to use completed organization data")
	}
	if uploadJobUseAllowed(job, model.OverlayActor{OrgID: "org-2", UserID: 10}) {
		t.Fatal("the original uploader must not use an upload after switching organizations")
	}
	if uploadJobUseAllowed(job, model.OverlayActor{UserID: 10}) {
		t.Fatal("an organization upload must not be usable without its organization context")
	}
}
