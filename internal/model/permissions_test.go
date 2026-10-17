package model

import "testing"

func TestBusinessPermissionDefaultsAndValidation(t *testing.T) {
	if len((&User{SystemRole: SystemRoleUser}).EffectivePermissions()) != 0 {
		t.Fatal("ordinary accounts must start read-only")
	}
	if len((&User{SystemRole: SystemRoleSuperAdmin}).EffectivePermissions()) != 9 {
		t.Fatal("administrators have all permissions")
	}
	values, err := NormalizePermissions([]string{"bed.manage", "bed.manage", "samples.write"})
	if err != nil || len(values) != 2 || values[0] != "samples.write" {
		t.Fatalf("normalization: %v %v", values, err)
	}
	if _, err := NormalizePermissions([]string{"users.manage"}); err == nil {
		t.Fatal("unknown permission accepted")
	}
	if len((&User{SystemRole: SystemRoleUser, Permissions: []string{"forged"}}).EffectivePermissions()) != 0 {
		t.Fatal("invalid persisted grants must fail closed")
	}
}
