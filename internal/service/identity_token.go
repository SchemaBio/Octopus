package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	IdentityTokenIssuer   = "squid"
	IdentityTokenAudience = "octopus"
	IdentityTokenType     = "identity"
)

type IdentityClaims struct {
	UserID            uint   `json:"user_id"`
	Email             string `json:"email"`
	Role              string `json:"role"`
	OrgID             string `json:"org_id"`
	StorageQuotaBytes int64  `json:"storage_quota_bytes,omitempty"`
	BreakGlass        bool   `json:"break_glass,omitempty"`
	Type              string `json:"typ"`
	jwt.RegisteredClaims
}

func SignIdentityToken(secret string, claims IdentityClaims, now time.Time) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf("identity token secret is required")
	}
	if claims.UserID == 0 || strings.TrimSpace(claims.Email) == "" || strings.TrimSpace(claims.OrgID) == "" {
		return "", fmt.Errorf("identity token requires user_id, email, and org_id")
	}
	if now.IsZero() {
		now = time.Now()
	}
	claims.Type = IdentityTokenType
	claims.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    IdentityTokenIssuer,
		Audience:  jwt.ClaimStrings{IdentityTokenAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(2 * time.Minute)),
		Subject:   strconv.FormatUint(uint64(claims.UserID), 10),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func IdentityTokenLooksLike(token string) bool {
	parts := strings.Split(token, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" && !strings.HasPrefix(token, "st1.")
}

func ParseIdentityToken(secret, token string) (*IdentityClaims, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf("identity token secret is required")
	}
	claims := &IdentityClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(IdentityTokenIssuer), jwt.WithAudience(IdentityTokenAudience))
	if err != nil {
		return nil, err
	}
	if !parsed.Valid || claims.Type != IdentityTokenType {
		return nil, fmt.Errorf("invalid identity token")
	}
	if claims.UserID == 0 || strings.TrimSpace(claims.Email) == "" || strings.TrimSpace(claims.OrgID) == "" {
		return nil, fmt.Errorf("identity token missing required claims")
	}
	return claims, nil
}
