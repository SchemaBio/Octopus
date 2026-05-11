package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// JWTService handles JWT token operations
type JWTService struct {
	cfg *config.Config
}

// NewJWTService creates a new JWT service
func NewJWTService(cfg *config.Config) *JWTService {
	return &JWTService{cfg: cfg}
}

// Claims represents JWT claims
type Claims struct {
	UserID     uint   `json:"user_id"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	jwt.RegisteredClaims
}

// GenerateToken generates a JWT token for a user
// Returns: accessToken, refreshToken, expiresAt (RFC3339 string), error
func (j *JWTService) GenerateToken(user *model.User) (string, string, string, error) {
	expiresAt := time.Now().Add(j.cfg.JWT.ExpireDuration)

	claims := Claims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   string(user.SystemRole),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    j.cfg.JWT.Issuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(j.cfg.JWT.Secret))
	if err != nil {
		return "", "", "", err
	}

	// Generate refresh token (longer expiry)
	refreshExpiresAt := time.Now().Add(j.cfg.JWT.RefreshDuration)
	refreshClaims := Claims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   string(user.SystemRole),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshExpiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    j.cfg.JWT.Issuer,
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(j.cfg.JWT.Secret))
	if err != nil {
		return "", "", "", err
	}

	return tokenString, refreshTokenString, expiresAt.Format(time.RFC3339), nil
}

// ValidateToken validates a JWT token and returns claims
func (j *JWTService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(j.cfg.JWT.Secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// RefreshToken refreshes an access token using refresh token
// Returns: accessToken, refreshToken, expiresAt (RFC3339 string), error
func (j *JWTService) RefreshToken(refreshTokenString string) (string, string, string, error) {
	claims, err := j.ValidateToken(refreshTokenString)
	if err != nil {
		return "", "", "", err
	}

	// Generate new access token
	expiresAt := time.Now().Add(j.cfg.JWT.ExpireDuration)
	newClaims := Claims{
		UserID: claims.UserID,
		Email:  claims.Email,
		Role:   claims.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    j.cfg.JWT.Issuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, newClaims)
	tokenString, err := token.SignedString([]byte(j.cfg.JWT.Secret))
	if err != nil {
		return "", "", "", err
	}

	// Generate new refresh token
	refreshExpiresAt := time.Now().Add(j.cfg.JWT.RefreshDuration)
	refreshClaims := Claims{
		UserID: claims.UserID,
		Email:  claims.Email,
		Role:   claims.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshExpiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    j.cfg.JWT.Issuer,
		},
	}

	newRefreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	newRefreshTokenString, err := newRefreshToken.SignedString([]byte(j.cfg.JWT.Secret))
	if err != nil {
		return "", "", "", err
	}

	return tokenString, newRefreshTokenString, expiresAt.Format(time.RFC3339), nil
}

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPassword checks if a password matches the hash
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// SHA256Hash computes SHA-256 hex digest of a string
func SHA256Hash(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

// PreparePassword applies client-side hash if enabled, returns the password for bcrypt
// When CLIENT_PASSWORD_HASH_ENABLED=true, the frontend sends SHA256(password+email)
// and we pass it through directly (bcrypt will hash it again)
func PreparePassword(rawPassword, email string, enabled bool) string {
	if enabled {
		return SHA256Hash(rawPassword + email)
	}
	return rawPassword
}

// SetTokenCookies sets httpOnly cookies for access and refresh tokens
func SetTokenCookies(c *gin.Context, cfg *config.JWTConfig, accessToken, refreshToken string) {
	accessMaxAge := int(cfg.ExpireDuration.Seconds())
	refreshMaxAge := int(cfg.RefreshDuration.Seconds())

	accessCookie := &http.Cookie{
		Name:     "access_token",
		Value:    accessToken,
		Path:     "/",
		Domain:   cfg.CookieDomain,
		MaxAge:   accessMaxAge,
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}

	refreshCookie := &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/",
		Domain:   cfg.CookieDomain,
		MaxAge:   refreshMaxAge,
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}

	http.SetCookie(c.Writer, accessCookie)
	http.SetCookie(c.Writer, refreshCookie)
}

// ClearTokenCookies clears the token cookies (logout)
func ClearTokenCookies(c *gin.Context, cfg *config.JWTConfig) {
	accessCookie := &http.Cookie{
		Name:     "access_token",
		Value:    "",
		Path:     "/",
		Domain:   cfg.CookieDomain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}

	refreshCookie := &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/",
		Domain:   cfg.CookieDomain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}

	http.SetCookie(c.Writer, accessCookie)
	http.SetCookie(c.Writer, refreshCookie)
}