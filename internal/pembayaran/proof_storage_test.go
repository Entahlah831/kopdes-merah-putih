package pembayaran

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/security"
)

func TestProofEnvelopeAndSeparateStorageIsolation(t *testing.T) {
	pngBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	kycDir := filepath.Join(t.TempDir(), "kyc_only")
	buktiDir := filepath.Join(t.TempDir(), "bukti_only")
	if err := os.MkdirAll(kycDir, 0o700); err != nil {
		t.Fatal(err)
	}

	mkp, err := security.NewMultiKeyProviderFromConfig("v1", "k1", strings.Repeat("0123456789abcdef", 4), "")
	if err != nil {
		t.Fatal(err)
	}
	kycKP, err := mkp.DerivePurpose(security.PurposeKYCFiles)
	if err != nil {
		t.Fatal(err)
	}
	buktiKP, err := mkp.DerivePurpose(security.PurposeBuktiFiles)
	if err != nil {
		t.Fatal(err)
	}

	svc := &Service{cfg: Config{ProofStorageDir: buktiDir, KeyProvider: buktiKP}}
	id := uuid.New()
	wrappedDEK, keyID, err := svc.saveEncryptedProofEnvelope(id, pngBytes)
	if err != nil {
		t.Fatalf("saveEncryptedProofEnvelope: %v", err)
	}
	if len(wrappedDEK) == 0 || keyID != "v1:k1" {
		t.Fatalf("unexpected wrappedDEK len=%d keyID=%q", len(wrappedDEK), keyID)
	}

	diskPath := filepath.Join(buktiDir, "bukti_"+id.String()+".enc")
	onDisk, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("expected file at %s: %v", diskPath, err)
	}
	if bytes.Contains(onDisk, []byte{0x89, 'P', 'N', 'G'}) || bytes.Contains(onDisk, []byte{0xff, 0xd8, 0xff}) {
		t.Fatal("proof file on disk leaked raw image header bytes")
	}
	if entries, _ := os.ReadDir(kycDir); len(entries) != 0 {
		t.Fatalf("kycDir must stay untouched when storing bukti, found %d files", len(entries))
	}

	// Kunci KYC tidak boleh mampu membuka amplop bukti
	aad := security.FormatAAD("pembayaran", "bukti", id.String())
	if _, err := security.DecryptEnvelopeFile(kycKP, onDisk, wrappedDEK, aad); err == nil {
		t.Fatal("kycKP must fail decrypting proof file")
	}

	plain, mime, err := svc.readDecryptedProofEnvelope(id, wrappedDEK)
	if err != nil {
		t.Fatalf("readDecryptedProofEnvelope: %v", err)
	}
	if mime != "image/png" || !bytes.Equal(plain, pngBytes) {
		t.Fatalf("decrypted proof mismatch: mime=%s len=%d", mime, len(plain))
	}
}
