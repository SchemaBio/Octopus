package service

import (
	"testing"
	"time"
)

func TestSignAndParseIdentityToken(t *testing.T) {
	secret := "identity-secret-with-enough-length"
	token, err := SignIdentityToken(secret, IdentityClaims{
		UserID:            42,
		Email:             "doctor@example.com",
		Role:              "ORG_USER",
		OrgID:             "org-1",
		StorageQuotaBytes: 100,
	}, time.Now())
	if err != nil {
		t.Fatalf("SignIdentityToken: %v", err)
	}
	if !IdentityTokenLooksLike(token) {
		t.Fatalf("expected JWT identity token, got %q", token)
	}
	claims, err := ParseIdentityToken(secret, token)
	if err != nil {
		t.Fatalf("ParseIdentityToken: %v", err)
	}
	if claims.UserID != 42 || claims.OrgID != "org-1" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestParseIdentityTokenRejectsWrongSecret(t *testing.T) {
	secret := "identity-secret-with-enough-length"
	token, err := SignIdentityToken(secret, IdentityClaims{UserID: 1, Email: "a@b.c", Role: "ORG_USER", OrgID: "org-1"}, time.Now())
	if err != nil {
		t.Fatalf("SignIdentityToken: %v", err)
	}
	if _, err := ParseIdentityToken("other-secret-with-enough-length", token); err == nil {
		t.Fatal("expected wrong secret to be rejected")
	}
}
