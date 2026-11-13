package security_test

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/kopdes/merah-putih/internal/security"
	"golang.org/x/crypto/bcrypt"
)

func TestHashPasswordWithCost_AndMultiKeyRotation(t *testing.T) {
	// 1. BCRYPT_COST dari parameter
	h, err := security.HashPasswordWithCost("P@ssw0rdUji!", 4)
	if err != nil {
		t.Fatalf("HashPasswordWithCost(4) failed: %v", err)
	}
	cost, err := bcrypt.Cost([]byte(h))
	if err != nil {
		t.Fatalf("bcrypt.Cost failed: %v", err)
	}
	if cost != 4 {
		t.Fatalf("Expected bcrypt cost 4, got %d", cost)
	}
	if !security.CheckPassword("P@ssw0rdUji!", h) {
		t.Fatal("CheckPassword returned false for valid password")
	}
	if _, err := security.HashPasswordWithCost("P@ssw0rdUji!", 3); err == nil {
		t.Fatal("Expected error for bcrypt cost < 4")
	}

	// 2. Rotasi kunci AES: kunci lama v1:k1 -> kunci aktif baru v2:k2 + previousKeys
	key1Raw := []byte("0123456789abcdef0123456789abcdef")
	key2Raw := []byte("fedcba9876543210fedcba9876543210")

	oldKP, err := security.NewMasterKeyProvider("v1", "k1", key1Raw)
	if err != nil {
		t.Fatalf("NewMasterKeyProvider(v1:k1) failed: %v", err)
	}
	aad := []byte("pendaftaran_anggota:nik:1234")
	ctOld, err := oldKP.EncryptWithAAD([]byte("3201010101010001"), aad)
	if err != nil {
		t.Fatalf("EncryptWithAAD(old) failed: %v", err)
	}
	if !strings.HasPrefix(string(ctOld), "v1:k1:") {
		t.Fatalf("Expected v1:k1 prefix, got %q", string(ctOld[:10]))
	}

	prevSpec := "v1:k1=" + hex.EncodeToString(key1Raw)
	rotKP, err := security.NewMultiKeyProviderFromConfig("v2", "k2", hex.EncodeToString(key2Raw), prevSpec)
	if err != nil {
		t.Fatalf("NewMultiKeyProviderFromConfig failed: %v", err)
	}

	// Harus dapat mendekripsi ciphertext lama v1:k1
	ptFromOld, err := rotKP.DecryptWithAAD(ctOld, aad)
	if err != nil {
		t.Fatalf("Expected MultiKeyProvider to decrypt historical v1:k1 ciphertext, got: %v", err)
	}
	if string(ptFromOld) != "3201010101010001" {
		t.Fatalf("Plaintext mismatch: %q", string(ptFromOld))
	}

	// Enkripsi baru harus memakai v2:k2
	ctNew, err := rotKP.EncryptWithAAD([]byte("3201010101010002"), aad)
	if err != nil {
		t.Fatalf("EncryptWithAAD(new) failed: %v", err)
	}
	if !bytes.HasPrefix(ctNew, []byte("v2:k2:")) {
		t.Fatalf("Expected v2:k2 prefix for new encryption")
	}
	ptNew, err := rotKP.DecryptWithAAD(ctNew, aad)
	if err != nil || string(ptNew) != "3201010101010002" {
		t.Fatalf("Decrypt new failed: %v %q", err, string(ptNew))
	}
}
