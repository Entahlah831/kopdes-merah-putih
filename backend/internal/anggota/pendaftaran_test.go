package anggota

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kopdes/merah-putih/internal/filestore"
	"github.com/kopdes/merah-putih/internal/security"
)

const test1x1PNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func decodeSamplePNG(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(test1x1PNGBase64)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return b
}

func TestKYCFileStore_EncryptDecrypt_NoPlaintextMagic(t *testing.T) {
	dir := t.TempDir()
	key1 := strings.Repeat("0123456789abcdef", 4)
	kp1, err := security.NewMultiKeyProviderFromConfig("v1", "k1", key1, "")
	if err != nil {
		t.Fatalf("kp1: %v", err)
	}
	store, err := filestore.NewLocalFileStore(map[string]filestore.NamespaceConfig{
		filestore.NamespaceKYC: {Dir: dir, KeyProvider: kp1},
	})
	if err != nil {
		t.Fatalf("NewLocalFileStore: %v", err)
	}
	raw := decodeSamplePNG(t)
	aad := security.FormatAAD("dokumen_kyc", "ktp", "reg-123")
	saved, err := store.SaveEncrypted(context.Background(), filestore.NamespaceKYC, "kyc-reg123.enc", raw, aad)
	if err != nil {
		t.Fatalf("SaveEncrypted: %v", err)
	}
	diskBytes, err := os.ReadFile(filepath.Join(dir, saved.ObjectKey))
	if err != nil {
		t.Fatalf("read .enc: %v", err)
	}
	if bytes.HasPrefix(diskBytes, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("stored KYC file leaks raw PNG header")
	}
	// Simulate key rotation: k2 active, k1 in previous keys.
	key2 := strings.Repeat("fedcba9876543210", 4)
	kp2, err := security.NewMultiKeyProviderFromConfig("v2", "k2", key2, "v1:k1="+key1)
	if err != nil {
		t.Fatalf("kp2: %v", err)
	}
	store2, err := filestore.NewLocalFileStore(map[string]filestore.NamespaceConfig{
		filestore.NamespaceKYC: {Dir: dir, KeyProvider: kp2},
	})
	if err != nil {
		t.Fatalf("store2: %v", err)
	}
	plain, err := store2.LoadDecrypted(context.Background(), filestore.NamespaceKYC, saved.ObjectKey, saved.DEKWrapped, aad)
	if err != nil {
		t.Fatalf("LoadDecrypted with rotated key failed: %v", err)
	}
	if !bytes.Equal(plain, raw) {
		t.Fatal("decrypted bytes mismatch original PNG")
	}
}

func TestKYCValidationAndTurnstile(t *testing.T) {
	if _, err := filestore.ValidateImage([]byte("not-an-image"), 1<<20); err == nil {
		t.Fatal("expected MIME error for text payload")
	}
	if _, err := filestore.SafeObjectPath(t.TempDir(), "../escape.enc"); err == nil {
		t.Fatal("expected path traversal rejection")
	}
	svc := NewService(nil, Config{CaptchaEnabled: true})
	if err := svc.verifyTurnstile(context.Background(), "", "127.0.0.1"); err == nil {
		t.Fatal("expected error when captcha token missing")
	}
}

func TestValidateRegisterInput_OptionalTanggalLahir(t *testing.T) {
	base := RegisterInput{
		NamaLengkap: "Budi Santoso",
		NIK:         "3201010101900001",
		NoHP:        "081234567890",
		Alamat:      "Dusun Makmur RT 01",
	}
	if _, _, err := validateRegisterInput(base); err != nil {
		t.Fatalf("expected registration without tanggal_lahir to pass (data minimization), got %v", err)
	}
	withDOB := base
	withDOB.TanggalLahir = "1990-01-01"
	if _, _, err := validateRegisterInput(withDOB); err != nil {
		t.Fatalf("expected valid YYYY-MM-DD tanggal_lahir to pass, got %v", err)
	}
	badDOB := base
	badDOB.TanggalLahir = "01-01-1990"
	if _, _, err := validateRegisterInput(badDOB); err == nil {
		t.Fatal("expected malformed tanggal_lahir to return error")
	}
}
