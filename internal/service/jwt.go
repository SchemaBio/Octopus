package service

import (
	"errors"
	"time"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
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
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// GenerateToken generates a JWT token for a user
func (j *JWTService) GenerateToken(user *model.User) (string, string, int64, error) {
	expiresAt := time.Now().Add(j.cfg.JWT.ExpireDuration)

	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
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
		return "", "", 0, err
	}

	// Generate refresh token (longer expiry)
	refreshExpiresAt := time.Now().Add(j.cfg.JWT.RefreshDuration)
	refreshClaims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshExpiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    j.cfg.JWT.Issuer,
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(j.cfg.JWT.Secret))
	if err != nil {
		return "", "", 0, err
	}

	return tokenString, refreshTokenString, expiresAt.Unix(), nil
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
func (j *JWTService) RefreshToken(refreshTokenString string) (string, string, int64, error) {
	claims, err := j.ValidateToken(refreshTokenString)
	if err != nil {
		return "", "", 0, err
	}

	// Generate new access token
	expiresAt := time.Now().Add(j.cfg.JWT.ExpireDuration)
	newClaims := Claims{
		UserID:   claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
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
		return "", "", 0, err
	}

	// Generate new refresh token
	refreshExpiresAt := time.Now().Add(j.cfg.JWT.RefreshDuration)
	refreshClaims := Claims{
		UserID:   claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshExpiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    j.cfg.JWT.Issuer,
		},
	}

	newRefreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	newRefreshTokenString, err := newRefreshToken.SignedString([]byte(j.cfg.JWT.Secret))
	if err != nil {
		return "", "", 0, err
	}

	return tokenString, newRefreshTokenString, expiresAt.Unix(), nil
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