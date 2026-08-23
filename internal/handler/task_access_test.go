package handler

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestTaskAccessAllowed(t *testing.T) {
	tests := []struct {
		name   string
		task   *model.Task
		userID uint
		role   string
		orgID  string
		want   bool
	}{
		{
			name:   "same organization member",
			task:   &model.Task{ExternalOrgID: "org-1", CreatedBy: 10},
			userID: 20,
			role:   string(model.SystemRoleUser),
			orgID:  "org-1",
			want:   true,
		},
		{
			name:   "different organization rejects original creator",
			task:   &model.Task{ExternalOrgID: "org-1", CreatedBy: 10},
			userID: 10,
			role:   string(model.SystemRoleUser),
			orgID:  "org-2",
			want:   false,
		},
		{
			name:   "missing organization rejects SaaS task creator",
			task:   &model.Task{ExternalOrgID: "org-1", CreatedBy: 10},
			userID: 10,
			role:   string(model.SystemRoleUser),
			want:   false,
		},
		{
			name:   "standalone creator",
			task:   &model.Task{CreatedBy: 10},
			userID: 10,
			role:   string(model.SystemRoleUser),
			want:   true,
		},
		{
			name:   "standalone different user",
			task:   &model.Task{CreatedBy: 10},
			userID: 20,
			role:   string(model.SystemRoleUser),
			want:   false,
		},
		{
			name: "super admin",
			task: &model.Task{ExternalOrgID: "org-1", CreatedBy: 10},
			role: string(model.SystemRoleSuperAdmin),
			want: true,
		},
		{
			name: "nil task",
			role: string(model.SystemRoleSuperAdmin),
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := taskAccessAllowed(test.task, test.userID, test.role, test.orgID); got != test.want {
				t.Fatalf("taskAccessAllowed() = %v, want %v", got, test.want)
			}
		})
	}
}
