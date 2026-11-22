package security

import (
	"bytes"
	"strings"
	"testing"
)

func TestSecretValidation_RequiresStrongKey(t *testing.T) {
	if err := ValidateSecretKey("TEST_KEY", []byte("too-short-key")); err == nil {
		t.Fatalf("Expected error for secret < 32 bytes, got nil")
	}
	if err := ValidateSecretKey("TEST_KEY", bytes.Repeat([]byte("a"), 32)); err != nil {
		t.Fatalf("Expected valid 32-byte secret to pass, got: %v", err)
	}
}

func TestAESGCM_WithAAD_TamperProof(t *testing.T) {
	key := bytes.Repeat([]byte("k"), 32)
	kp, err := NewMasterKeyProvider("v1", "key-01", key)
	if err != nil {
		t.Fatalf("NewMasterKeyProvider error: %v", err)
	}

	plaintext := []byte("3201010101010001")
	aad1 := FormatAAD("anggota", "nik", "018d0000-0000-7000-8000-000000000001")
	aad2 := FormatAAD("anggota", "nik", "018d0000-0000-7000-8000-000000000002")

	ct, err := kp.EncryptWithAAD(plaintext, aad1)
	if err != nil {
		t.Fatalf("EncryptWithAAD error: %v", err)
	}
	if !strings.HasPrefix(string(ct), "v1:key-01:") {
		t.Fatalf("Unexpected prefix: %q", string(ct[:15]))
	}
	dec, err := kp.DecryptWithAAD(ct, aad1)
	if err != nil || !bytes.Equal(dec, plaintext) {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if _, err := kp.DecryptWithAAD(ct, aad2); err == nil {
		t.Fatal("Decryption must fail for mismatched AAD")
	}
}

func TestEnvelopeFileEncryption_AndHelpers(t *testing.T) {
	kp, err := NewMasterKeyProvider("v1", "k1", bytes.Repeat([]byte("e"), 32))
	if err != nil {
		t.Fatal(err)
	}
	pngStub := []byte("\x89PNG\r\n\x1a\nfake-png-content-for-kyc-test")
	aad := FormatAAD("dokumen_kyc", "file", "doc-1")
	cf, wrapped, kid, err := EncryptEnvelopeFile(kp, pngStub, aad)
	if err != nil {
		t.Fatalf("EncryptEnvelopeFile: %v", err)
	}
	if kid != "v1:k1" {
		t.Fatalf("kid=%q, want v1:k1", kid)
	}
	if bytes.Contains(cf, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("Ciphertext on disk MUST NOT contain raw PNG magic bytes!")
	}
	plain, err := DecryptEnvelopeFile(kp, cf, wrapped, aad)
	if err != nil || !bytes.Equal(plain, pngStub) {
		t.Fatalf("DecryptEnvelopeFile failed: %v", err)
	}

	if got := NeutralizeCSVCell("=cmd|'/C calc'!A0"); !strings.HasPrefix(got, "'=") {
		t.Fatalf("NeutralizeCSVCell failed: %q", got)
	}
	if got := MaskNIK("3201010101010001"); got != "3201********0001" {
		t.Fatalf("MaskNIK=%q", got)
	}
	if got := MaskPhone("081234567890"); got != "0812*****890" {
		t.Fatalf("MaskPhone=%q", got)
	}
}

func TestRedactDSN_HidesPassword(t *testing.T) {
	dsn := "postgres://kopdes_app:SuperSecretPassword123!@127.0.0.1:5433/kopdes_db?sslmode=disable"
	redacted := RedactDSN(dsn)
	if strings.Contains(redacted, "SuperSecretPassword123!") {
		t.Fatalf("Redacted DSN leaked password: %s", redacted)
	}
	if !strings.Contains(redacted, "kopdes_app") || !strings.Contains(redacted, "127.0.0.1:5433") {
		t.Fatalf("Redacted DSN missing host/user: %s", redacted)
	}
}
