package security

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestT3_SecretValidation_RequiresStrongKey(t *testing.T) {
	// 1. Secret < 32 bytes must be rejected
	shortKey := []byte("too-short-key")
	err := ValidateSecretKey("TEST_KEY", shortKey)
	if err == nil {
		t.Fatalf("Expected error for secret < 32 bytes, got nil")
	}

	// 2. Secret >= 32 bytes is valid
	validKey := bytes.Repeat([]byte("a"), 32)
	if err := ValidateSecretKey("TEST_KEY", validKey); err != nil {
		t.Fatalf("Expected valid 32-byte secret to pass, got: %v", err)
	}
}

func TestT3_AESGCM_WithAAD_TamperProof(t *testing.T) {
	key := bytes.Repeat([]byte("k"), 32)
	kp, err := NewMasterKeyProvider("v1", "key-01", key)
	if err != nil {
		t.Fatalf("NewMasterKeyProvider error: %v", err)
	}

	plaintext := []byte("3201010101010001") // Mock NIK
	aadRecord1 := FormatAAD("anggota", "nik", "018d0000-0000-7000-8000-000000000001")
	aadRecord2 := FormatAAD("anggota", "nik", "018d0000-0000-7000-8000-000000000002")

	ciphertext, err := kp.EncryptWithAAD(plaintext, aadRecord1)
	if err != nil {
		t.Fatalf("EncryptWithAAD error: %v", err)
	}

	// 1. Ciphertext must include key version prefix
	if !strings.HasPrefix(string(ciphertext), "v1:key-01:") {
		t.Fatalf("Ciphertext expected to start with version prefix 'v1:key-01:', got: %s", string(ciphertext[:15]))
	}

	// 2. Decrypt with correct AAD succeeds
	decrypted, err := kp.DecryptWithAAD(ciphertext, aadRecord1)
	if err != nil {
		t.Fatalf("DecryptWithAAD with correct AAD failed: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("Decrypted plaintext mismatch: got %s, want %s", string(decrypted), string(plaintext))
	}

	// 3. Decrypt with tampered AAD (different record ID) MUST fail
	_, err = kp.DecryptWithAAD(ciphertext, aadRecord2)
	if err == nil {
		t.Fatalf("VULNERABILITY REPRODUCED: Decryption succeeded with tampered AAD record ID!")
	}

	// 4. Decrypt with tampered column name MUST fail
	aadTamperedColumn := FormatAAD("anggota", "alamat", "018d0000-0000-7000-8000-000000000001")
	_, err = kp.DecryptWithAAD(ciphertext, aadTamperedColumn)
	if err == nil {
		t.Fatalf("VULNERABILITY REPRODUCED: Decryption succeeded with tampered AAD column name!")
	}
}

func TestT3_HKDF_TenantBlindIndex_Isolation(t *testing.T) {
	masterSecret := bytes.Repeat([]byte("s"), 32)
	koperasi1 := uuid.MustParse("018d0000-0000-7000-8000-000000000001")
	koperasi2 := uuid.MustParse("018d0000-0000-7000-8000-000000000002")

	val := "3201010101010001" // Same NIK in two different cooperatives

	hashKop1, err := HashTenantBlindIndex(val, masterSecret, koperasi1)
	if err != nil {
		t.Fatalf("HashTenantBlindIndex kop1 error: %v", err)
	}

	hashKop2, err := HashTenantBlindIndex(val, masterSecret, koperasi2)
	if err != nil {
		t.Fatalf("HashTenantBlindIndex kop2 error: %v", err)
	}

	// Two cooperatives MUST have completely different blind indexes for the same NIK
	if hashKop1 == hashKop2 {
		t.Fatalf("CRITICAL: Blind index for different tenants resulted in identical hash (%s)! HKDF per tenant failed.", hashKop1)
	}

	// Deterministic within the same cooperative
	hashKop1Repeat, _ := HashTenantBlindIndex(val, masterSecret, koperasi1)
	if hashKop1 != hashKop1Repeat {
		t.Fatalf("Blind index for same tenant must be deterministic: %s != %s", hashKop1, hashKop1Repeat)
	}
}

func TestT3_RedactDSN_HidesPassword(t *testing.T) {
	dsn := "postgres://kopdes_app:SuperSecretPassword123!@127.0.0.1:5433/kopdes_db?sslmode=disable"
	redacted := RedactDSN(dsn)

	if strings.Contains(redacted, "SuperSecretPassword123!") {
		t.Fatalf("VULNERABILITY: Redacted DSN still contains plain password! Got: %s", redacted)
	}

	if !strings.Contains(redacted, "kopdes_app") || !strings.Contains(redacted, "127.0.0.1:5433") {
		t.Fatalf("Redacted DSN should preserve user and host, got: %s", redacted)
	}
}
