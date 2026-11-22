package filestore

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

const samplePNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func decodeSamplePNG(t *testing.T) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(samplePNGBase64)
	if err != nil {
		t.Fatalf("decode sample png: %v", err)
	}
	return raw
}

func TestLocalFileStore_RoundTripAndKeyIsolation(t *testing.T) {
	kycDir := filepath.Join(t.TempDir(), "kyc")
	buktiDir := filepath.Join(t.TempDir(), "bukti")
	mkp, err := security.NewMultiKeyProviderFromConfig("v1", "k1", strings.Repeat("0123456789abcdef", 4), "")
	if err != nil {
		t.Fatal(err)
	}
	kycKP, _ := mkp.DerivePurpose(security.PurposeKYCFiles)
	buktiKP, _ := mkp.DerivePurpose(security.PurposeBuktiFiles)

	store, err := NewLocalFileStore(map[string]NamespaceConfig{
		NamespaceKYC:   {Dir: kycDir, KeyProvider: kycKP},
		NamespaceBukti: {Dir: buktiDir, KeyProvider: buktiKP},
	})
	if err != nil {
		t.Fatalf("NewLocalFileStore: %v", err)
	}

	raw := decodeSamplePNG(t)
	meta, err := ValidateImage(raw, 1<<20)
	if err != nil || meta.Ext != ".png" || meta.WidthPx != 1 {
		t.Fatalf("ValidateImage: meta=%+v err=%v", meta, err)
	}

	aad := security.FormatAAD("dokumen_kyc", "ktp", "reg-01")
	saved, err := store.SaveEncrypted(context.Background(), NamespaceKYC, "kyc-01.enc", raw, aad)
	if err != nil {
		t.Fatalf("SaveEncrypted: %v", err)
	}

	diskBytes, err := os.ReadFile(filepath.Join(kycDir, saved.ObjectKey))
	if err != nil || bytes.Contains(diskBytes, []byte("PNG")) {
		t.Fatalf("file on disk leaked magic bytes or failed read: err=%v", err)
	}
	if entries, _ := os.ReadDir(buktiDir); len(entries) != 0 {
		t.Fatalf("buktiDir must stay empty, got %d files", len(entries))
	}

	// Memakai namespace bukti (kunci salah) harus gagal
	if _, err := store.LoadDecrypted(context.Background(), NamespaceBukti, "kyc-01.enc", saved.DEKWrapped, aad); err == nil {
		t.Fatal("expected decryption failure with wrong namespace key")
	}

	plain, err := store.LoadDecrypted(context.Background(), NamespaceKYC, "kyc-01.enc", saved.DEKWrapped, aad)
	if err != nil || !bytes.Equal(plain, raw) {
		t.Fatalf("LoadDecrypted mismatch: err=%v len=%d", err, len(plain))
	}

	if err := store.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
	if err := store.Delete(context.Background(), NamespaceKYC, "kyc-01.enc"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if exists, err := store.Exists(context.Background(), NamespaceKYC, "kyc-01.enc"); err != nil || exists {
		t.Fatalf("expected exists=false after delete, got %v err=%v", exists, err)
	}
}

func TestLocalFileStore_SecurityAndValidationGuards(t *testing.T) {
	if _, err := ValidateImage([]byte("not an image"), 1024); err == nil {
		t.Fatal("expected invalid MIME rejection")
	}
	if _, err := SafeObjectPath(t.TempDir(), "../evil.enc"); err == nil {
		t.Fatal("expected path traversal rejection")
	}
	if _, err := SafeObjectPath(t.TempDir(), `sub\evil.enc`); err == nil {
		t.Fatal("expected path separator rejection")
	}

	key1 := strings.Repeat("0123456789abcdef", 4)
	key2 := strings.Repeat("fedcba9876543210", 4)
	kp1, _ := security.NewMasterKeyProvider("v1", "k1", decodeHex32(t, key1))
	kp2, _ := security.NewMultiKeyProviderFromConfig("v2", "k2", key2, "")

	dir := filepath.Join(t.TempDir(), "kyc")
	s1, _ := NewLocalFileStore(map[string]NamespaceConfig{NamespaceKYC: {Dir: dir, KeyProvider: kp1}})
	aad := security.FormatAAD("pembayaran", "bukti", "pay-1")
	raw := decodeSamplePNG(t)
	saved, err := s1.SaveEncrypted(context.Background(), NamespaceKYC, "b.enc", raw, aad)
	if err != nil {
		t.Fatal(err)
	}

	// Tanpa legacy key: harus gagal
	sNoLegacy, _ := NewLocalFileStore(map[string]NamespaceConfig{NamespaceKYC: {Dir: dir, KeyProvider: kp2}})
	if _, err := sNoLegacy.LoadDecrypted(context.Background(), NamespaceKYC, "b.enc", saved.DEKWrapped, aad); err == nil {
		t.Fatal("expected failure without legacy key provider")
	}

	// Dengan legacy key: harus berhasil
	sWithLegacy, _ := NewLocalFileStore(map[string]NamespaceConfig{NamespaceKYC: {Dir: dir, KeyProvider: kp2, LegacyKeyProvider: kp1}})
	plain, err := sWithLegacy.LoadDecrypted(context.Background(), NamespaceKYC, "b.enc", saved.DEKWrapped, aad)
	if err != nil || !bytes.Equal(plain, raw) {
		t.Fatalf("expected legacy fallback decrypt to succeed: err=%v", err)
	}
}

func decodeHex32(t *testing.T, hexStr string) []byte {
	t.Helper()
	b := make([]byte, 32)
	for i := 0; i < 32; i++ {
		var v byte
		for j := 0; j < 2; j++ {
			c := hexStr[i*2+j]
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= c - '0'
			case c >= 'a' && c <= 'f':
				v |= c - 'a' + 10
			}
		}
		b[i] = v
	}
	return b
}
