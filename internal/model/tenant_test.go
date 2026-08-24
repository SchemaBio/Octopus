package model

import "testing"

func TestTenantIDForIdentity(t *testing.T) {
	tests := []struct {
		name string
		org  string
		user uint
		want string
	}{
		{name: "organization", org: "acme", user: 42, want: "org:acme"},
		{name: "standalone", user: 42, want: "user:42"},
		{name: "unresolvable", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TenantIDForIdentity(tt.org, tt.user); got != tt.want {
				t.Fatalf("TenantIDForIdentity(%q, %d) = %q, want %q", tt.org, tt.user, got, tt.want)
			}
		})
	}
}
