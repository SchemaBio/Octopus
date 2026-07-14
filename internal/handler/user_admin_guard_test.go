package handler

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestValidateUserSelfMutationRejectsDangerousSelfChanges(t *testing.T) {
	inactive := false
	tests := []struct {
		name     string
		req      *model.UserUpdateRequest
		deleting bool
	}{
		{
			name:     "delete self",
			deleting: true,
		},
		{
			name: "deactivate self",
			req:  &model.UserUpdateRequest{IsActive: &inactive},
		},
		{
			name: "demote self",
			req:  &model.UserUpdateRequest{SystemRole: model.SystemRoleUser},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateUserSelfMutation(1, 1, tt.req, tt.deleting); err == nil {
				t.Fatal("expected self mutation to be rejected")
			}
		})
	}
}

func TestValidateUserSelfMutationAllowsSafeAndOtherUserChanges(t *testing.T) {
	if err := validateUserSelfMutation(1, 1, &model.UserUpdateRequest{Name: "New Name"}, false); err != nil {
		t.Fatalf("expected own name update to be allowed: %v", err)
	}
	if err := validateUserSelfMutation(1, 2, &model.UserUpdateRequest{SystemRole: model.SystemRoleUser}, false); err != nil {
		t.Fatalf("expected changing another user to be allowed by self guard: %v", err)
	}
}

func TestValidateSystemRoleRejectsUnknownRole(t *testing.T) {
	if err := validateSystemRole(model.SystemRole("OWNER")); err == nil {
		t.Fatal("expected unknown role to be rejected")
	}
	if err := validateSystemRole(model.SystemRoleSuperAdmin); err != nil {
		t.Fatalf("expected super admin role to be accepted: %v", err)
	}
}
