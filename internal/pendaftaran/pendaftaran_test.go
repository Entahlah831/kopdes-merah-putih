package pendaftaran

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	store, err := NewKYCFileStore(dir, kp1, 1<<20)
	if err != nil {
		t.Fatalf("NewKYCFileStore: %v", err)
	}
	raw := decodeSamplePNG(t)
	saved, err := store.SaveEncrypted("reg-123", "ktp", raw)
	if err != nil {
		t.Fatalf("SaveEncrypted: %v", err)
	}
	diskBytes, err := os.ReadFile(filepath.Join(dir, saved.StorageKey))
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
	store2, err := NewKYCFileStore(dir, kp2, 1<<20)
	if err != nil {
		t.Fatalf("store2: %v", err)
	}
	plain, err := store2.LoadDecrypted("reg-123", "ktp", saved.StorageKey, saved.DEKWrapped)
	if err != nil {
		t.Fatalf("LoadDecrypted with rotated key failed: %v", err)
	}
	if !bytes.Equal(plain, raw) {
		t.Fatal("decrypted bytes mismatch original PNG")
	}
}

func TestKYCValidationAndTurnstile(t *testing.T) {
	if _, _, _, err := validateImageBytes([]byte("not-an-image"), 1<<20); err == nil {
		t.Fatal("expected MIME error for text payload")
	}
	fs, _ := NewKYCFileStore(t.TempDir(), nil, 1024)
	if _, err := fs.safePath("../escape.enc"); err == nil {
		t.Fatal("expected path traversal rejection")
	}
	svc := NewService(nil, fs, Config{CaptchaEnabled: true})
	if err := svc.verifyTurnstile(context.Background(), "", "127.0.0.1"); err == nil {
		t.Fatal("expected error when captcha token missing")
	}
}
