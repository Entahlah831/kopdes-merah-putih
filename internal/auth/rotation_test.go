package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/auth"
)

func TestTokenManager_AlgorithmAndSecretRotation(t *testing.T) {
	// 1. Algoritma "none" dan "RS256" wajib ditolak
	if _, err := auth.NewTokenManagerWithConfig("0123456789abcdef0123456789abcdef", "", "none", "kopdes"); err == nil {
		t.Fatal("Expected error for unsupported JWT algorithm 'none'")
	}
	if _, err := auth.NewTokenManagerWithConfig("0123456789abcdef0123456789abcdef", "", "RS256", "kopdes"); err == nil {
		t.Fatal("Expected error for asymmetric JWT algorithm 'RS256'")
	}

	oldSecret := "old_secret_0123456789abcdef0123456789abcdef"
	newSecret := "new_secret_fedcba9876543210fedcba9876543210"
	uid := uuid.New()
	kid := uuid.New()

	// 2. Token ditandatangani dengan secret lama (misal sebelum rotasi)
	oldTM, err := auth.NewTokenManagerWithConfig(oldSecret, "", "HS512", "kopdes-test")
	if err != nil {
		t.Fatalf("NewTokenManagerWithConfig(old) failed: %v", err)
	}
	oldToken, err := oldTM.GenerateToken(uid, kid, "admin", false, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken(old) failed: %v", err)
	}

	// 3. Masa transisi rotasi: aktif = newSecret, previous = oldSecret
	rotTM, err := auth.NewTokenManagerWithConfig(newSecret, oldSecret, "HS512", "kopdes-test")
	if err != nil {
		t.Fatalf("NewTokenManagerWithConfig(rot) failed: %v", err)
	}
	claims, err := rotTM.ValidateToken(oldToken)
	if err != nil {
		t.Fatalf("Expected old token to remain valid during transition with JWT_PREVIOUS_SECRET, got: %v", err)
	}
	if claims.UserID != uid || claims.KoperasiID != kid {
		t.Errorf("Claims mismatch after rotation validation")
	}

	// Token baru harus ditandatangani dengan newSecret
	newToken, err := rotTM.GenerateToken(uid, kid, "admin", false, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken(new) failed: %v", err)
	}
	if _, err := oldTM.ValidateToken(newToken); err == nil {
		t.Fatal("Expected new token NOT to validate against old-only manager")
	}

	// 4. Setelah JWT_PREVIOUS_SECRET dihapus, token lama harus gagal validasi
	finalTM, err := auth.NewTokenManagerWithConfig(newSecret, "", "HS512", "kopdes-test")
	if err != nil {
		t.Fatalf("NewTokenManagerWithConfig(final) failed: %v", err)
	}
	if _, err := finalTM.ValidateToken(oldToken); err == nil {
		t.Fatal("Expected old token to fail after previous secret is retired")
	}
	if _, err := finalTM.ValidateToken(newToken); err != nil {
		t.Fatalf("Expected new token to validate against final manager, got: %v", err)
	}
}
