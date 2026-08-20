package services

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/imanjo/fiber-api/internal/config"
)

// JWTClaims represents the JWT payload
type JWTClaims struct {
	UserID      uint   `json:"user_id"`
	Email       string `json:"email"`
	CountryID   uint   `json:"country_id,omitempty"`
	AuthVersion uint64 `json:"auth_version,omitempty"`
	jwt.RegisteredClaims
}

// JWTService handles token generation and validation
type JWTService struct {
	secret       []byte
	expireHours  int
	refreshHours int
}

// NewJWTService creates a new JWTService
func NewJWTService() *JWTService {
	cfg := config.Get().JWT
	return &JWTService{
		secret:       []byte(cfg.Secret),
		expireHours:  cfg.ExpireHours,
		refreshHours: cfg.RefreshHours,
	}
}

// GenerateToken creates a new JWT access token.
// countryID is embedded in the token so the server can verify the request country
// matches the token without a DB lookup.
func (s *JWTService) GenerateToken(userID uint, email string, countryID ...uint) (string, error) {
	return s.GenerateTokenWithAuthVersion(userID, email, 0, countryID...)
}

// GenerateTokenWithAuthVersion creates an access token bound to the
// authentication state that existed when the user authenticated.
func (s *JWTService) GenerateTokenWithAuthVersion(userID uint, email string, authVersion uint64, countryID ...uint) (string, error) {
	var cid uint
	if len(countryID) > 0 {
		cid = countryID[0]
	}

	claims := JWTClaims{
		UserID:      userID,
		Email:       email,
		CountryID:   cid,
		AuthVersion: authVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(s.expireHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "imanjo-api",
			Subject:   email,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// GenerateRefreshToken creates a long-lived refresh token
func (s *JWTService) GenerateRefreshToken(userID uint, email string, countryID ...uint) (string, error) {
	return s.GenerateRefreshTokenWithAuthVersion(userID, email, 0, countryID...)
}

// GenerateRefreshTokenWithAuthVersion creates a refresh token bound to
// the same authentication version as its access-token/session family.
func (s *JWTService) GenerateRefreshTokenWithAuthVersion(userID uint, email string, authVersion uint64, countryID ...uint) (string, error) {
	var cid uint
	if len(countryID) > 0 {
		cid = countryID[0]
	}

	jti := s.GenerateRandomString(16)
	if jti == "" {
		return "", errors.New("failed to generate refresh token id")
	}

	claims := JWTClaims{
		UserID:      userID,
		Email:       email,
		CountryID:   cid,
		AuthVersion: authVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(s.refreshHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "imanjo-api-refresh",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// parseToken verifies the JWT signature, algorithm and registered time claims.
// Token purpose (access vs refresh) is enforced by the public validators below.
func (s *JWTService) parseToken(tokenStr string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})

	if err != nil {
		return nil, MapError(err)
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

// ValidateToken accepts ACCESS tokens only.
// Refresh tokens use the same signing secret but a different issuer and must
// never be accepted as bearer authentication credentials.
func (s *JWTService) ValidateToken(tokenStr string) (*JWTClaims, error) {
	claims, err := s.parseToken(tokenStr)
	if err != nil {
		return nil, err
	}

	if claims.Issuer != "imanjo-api" {
		return nil, errors.New("not an access token")
	}

	return claims, nil
}

// DownloadClaims is embedded in short-lived signed download tokens.
type DownloadClaims struct {
	FileID    uint64 `json:"file_id"`
	CountryID uint   `json:"country_id"`
	jwt.RegisteredClaims
}

// GenerateDownloadToken creates a short-lived (15 min) signed token for a file download.
func (s *JWTService) GenerateDownloadToken(fileID uint64, countryID uint) (string, error) {
	claims := DownloadClaims{
		FileID:    fileID,
		CountryID: countryID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "imanjo-api-download",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// ValidateDownloadToken parses a download token and returns its claims.
func (s *JWTService) ValidateDownloadToken(tokenStr string) (*DownloadClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &DownloadClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, MapError(err)
	}
	claims, ok := token.Claims.(*DownloadClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid download token")
	}
	if claims.Issuer != "imanjo-api-download" {
		return nil, errors.New("not a download token")
	}
	return claims, nil
}

// ValidateRefreshToken parses and validates a refresh token (issued by GenerateRefreshToken).
// It rejects access tokens by checking the issuer field.
func (s *JWTService) ValidateRefreshToken(tokenStr string) (*JWTClaims, error) {
	claims, err := s.parseToken(tokenStr)
	if err != nil {
		return nil, MapError(err)
	}
	if claims.Issuer != "imanjo-api-refresh" {
		return nil, errors.New("not a refresh token")
	}
	return claims, nil
}

// GenerateRandomString creates a cryptographically secure random string
func (s *JWTService) GenerateRandomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", b)
}
