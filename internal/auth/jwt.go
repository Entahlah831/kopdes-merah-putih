package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalidToken = errors.New("token otorisasi tidak valid")
	ErrExpiredToken = errors.New("token otorisasi telah kedaluwarsa")
)

type UserClaims struct {
	UserID          uuid.UUID `json:"user_id"`
	KoperasiID      uuid.UUID `json:"koperasi_id"`
	Role            string    `json:"role"`
	IsPlatformAdmin bool      `json:"is_platform_admin"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secretKey     []byte
	previousKey   []byte
	signingMethod jwt.SigningMethod
	issuer        string
}

func NewTokenManager(secretKey string, issuer string) *TokenManager {
	if issuer == "" {
		issuer = "kopdes-merah-putih"
	}
	return &TokenManager{
		secretKey:     []byte(secretKey),
		signingMethod: jwt.SigningMethodHS256,
		issuer:        issuer,
	}
}

// NewTokenManagerWithConfig membuat TokenManager dengan algoritma HMAC eksplisit dan dukungan JWT_PREVIOUS_SECRET untuk masa transisi rotasi.
func NewTokenManagerWithConfig(secretKey, previousSecret, algorithm, issuer string) (*TokenManager, error) {
	if issuer == "" {
		issuer = "kopdes-merah-putih"
	}
	var method jwt.SigningMethod
	switch strings.ToUpper(strings.TrimSpace(algorithm)) {
	case "", "HS256":
		method = jwt.SigningMethodHS256
	case "HS384":
		method = jwt.SigningMethodHS384
	case "HS512":
		method = jwt.SigningMethodHS512
	default:
		return nil, fmt.Errorf("algoritma JWT tidak diizinkan: %q (wajib HS256, HS384, atau HS512)", algorithm)
	}
	var prev []byte
	if strings.TrimSpace(previousSecret) != "" {
		prev = []byte(previousSecret)
	}
	return &TokenManager{
		secretKey:     []byte(secretKey),
		previousKey:   prev,
		signingMethod: method,
		issuer:        issuer,
	}, nil
}

// GenerateToken membuat token JWT bertanda tangan HMAC terkonfigurasi
func (tm *TokenManager) GenerateToken(userID, koperasiID uuid.UUID, role string, isPlatformAdmin bool, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := UserClaims{
		UserID:          userID,
		KoperasiID:      koperasiID,
		Role:            role,
		IsPlatformAdmin: isPlatformAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   userID.String(),
			Issuer:    tm.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	method := tm.signingMethod
	if method == nil {
		method = jwt.SigningMethodHS256
	}
	token := jwt.NewWithClaims(method, claims)
	return token.SignedString(tm.secretKey)
}

// ValidateToken memvalidasi tanda tangan dan klaim JWT (termasuk previousKey selama rotasi)
func (tm *TokenManager) ValidateToken(tokenString string) (*UserClaims, error) {
	claims, err := tm.parseWithKey(tokenString, tm.secretKey)
	if err == nil {
		return claims, nil
	}
	if errors.Is(err, ErrExpiredToken) || len(tm.previousKey) == 0 {
		return nil, err
	}
	return tm.parseWithKey(tokenString, tm.previousKey)
}

func (tm *TokenManager) parseWithKey(tokenString string, key []byte) (*UserClaims, error) {
	method := tm.signingMethod
	if method == nil {
		method = jwt.SigningMethodHS256
	}
	token, err := jwt.ParseWithClaims(
		tokenString,
		&UserClaims{},
		func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, ErrInvalidToken
			}
			return key, nil
		},
		jwt.WithValidMethods([]string{method.Alg()}),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*UserClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// GenerateRefreshToken menghasilkan string random 32-byte berkode hex dan hash SHA-256 untuk database
func GenerateRefreshToken() (plainToken string, tokenHash string, err error) {
	bytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", "", err
	}
	plainToken = hex.EncodeToString(bytes)
	h := sha256.Sum256([]byte(plainToken))
	tokenHash = hex.EncodeToString(h[:])
	return plainToken, tokenHash, nil
}

// HashRefreshToken menghitung hash SHA-256 dari plain token untuk pencarian database
func HashRefreshToken(plainToken string) string {
	h := sha256.Sum256([]byte(plainToken))
	return hex.EncodeToString(h[:])
}
