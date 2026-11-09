package auth

import (
	"errors"
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
	secretKey []byte
	issuer    string
}

func NewTokenManager(secretKey string, issuer string) *TokenManager {
	if issuer == "" {
		issuer = "kopdes-merah-putih"
	}
	return &TokenManager{
		secretKey: []byte(secretKey),
		issuer:    issuer,
	}
}

// GenerateToken membuat token JWT bertanda tangan HMAC-SHA256
func (tm *TokenManager) GenerateToken(userID, koperasiID uuid.UUID, role string, isPlatformAdmin bool, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := UserClaims{
		UserID:          userID,
		KoperasiID:      koperasiID,
		Role:            role,
		IsPlatformAdmin: isPlatformAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    tm.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tm.secretKey)
}

// ValidateToken memvalidasi tanda tangan dan klaim JWT
func (tm *TokenManager) ValidateToken(tokenString string) (*UserClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return tm.secretKey, nil
	})

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
