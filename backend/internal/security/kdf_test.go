package security

import (
	"bytes"
	"strings"
	"testing"
)

func TestHKDFPurposeSeparation_IsolationAndRotation(t *testing.T) {
	key1Hex := strings.Repeat("0123456789abcdef", 4)
	key2Hex := strings.Repeat("fedcba9876543210", 4)

	mkp1, err := NewMultiKeyProviderFromConfig("v1", "k1", key1Hex, "")
	if err != nil {
		t.Fatalf("NewMultiKeyProviderFromConfig mkp1: %v", err)
	}

	dbKP1, err := mkp1.DerivePurpose(PurposeDBColumns)
	if err != nil {
		t.Fatalf("DerivePurpose db: %v", err)
	}
	kycKP1, err := mkp1.DerivePurpose(PurposeKYCFiles)
	if err != nil {
		t.Fatalf("DerivePurpose kyc: %v", err)
	}
	buktiKP1, err := mkp1.DerivePurpose(PurposeBuktiFiles)
	if err != nil {
		t.Fatalf("DerivePurpose bukti: %v", err)
	}
	backupKP1, err := mkp1.DerivePurpose(PurposeBackup)
	if err != nil {
		t.Fatalf("DerivePurpose backup: %v", err)
	}
	if _, err := mkp1.DerivePurpose(""); err == nil {
		t.Fatal("expected error for empty KDF purpose")
	}

	// Enkripsi dengan kycKP1 wajib gagal didekripsi oleh buktiKP1, dbKP1, backupKP1, maupun master mentah
	aad := FormatAAD("dokumen_kyc", "ktp", "reg-1")
	plain := []byte("data-rahasia-kyc-ktp")
	ctKYC, err := kycKP1.EncryptWithAAD(plain, aad)
	if err != nil {
		t.Fatalf("kycKP1.EncryptWithAAD: %v", err)
	}
	for name, kp := range map[string]KeyProvider{
		"bukti":  buktiKP1,
		"db":     dbKP1,
		"backup": backupKP1,
		"master": mkp1,
	} {
		if _, decErr := kp.DecryptWithAAD(ctKYC, aad); decErr == nil {
			t.Fatalf("%s key MUST fail decrypting %s ciphertext", name, PurposeKYCFiles)
		}
	}

	// Envelope encryption memakai buktiKP1 gagal dibuka kycKP1
	envCipher, envWrapped, _, err := EncryptEnvelopeFile(buktiKP1, plain, FormatAAD("pembayaran", "bukti", "pay-1"))
	if err != nil {
		t.Fatalf("EncryptEnvelopeFile: %v", err)
	}
	if _, err := DecryptEnvelopeFile(kycKP1, envCipher, envWrapped, FormatAAD("pembayaran", "bukti", "pay-1")); err == nil {
		t.Fatal("kycKP MUST NOT decrypt bukti envelope")
	}

	// Rotasi kunci: mkp2 (v2:k2 aktif + v1:k1 previous) tetap membuka ciphertext lama domain sama
	mkp2, err := NewMultiKeyProviderFromConfig("v2", "k2", key2Hex, "v1:k1="+key1Hex)
	if err != nil {
		t.Fatalf("NewMultiKeyProviderFromConfig mkp2: %v", err)
	}
	kycKP2, err := mkp2.DerivePurpose(PurposeKYCFiles)
	if err != nil {
		t.Fatalf("mkp2.DerivePurpose kyc: %v", err)
	}
	buktiKP2, err := mkp2.DerivePurpose(PurposeBuktiFiles)
	if err != nil {
		t.Fatalf("mkp2.DerivePurpose bukti: %v", err)
	}

	gotKYC, err := kycKP2.DecryptWithAAD(ctKYC, aad)
	if err != nil || !bytes.Equal(gotKYC, plain) {
		t.Fatalf("rotated kycKP2 failed decrypting old kyc ciphertext: err=%v", err)
	}
	gotBukti, err := DecryptEnvelopeFile(buktiKP2, envCipher, envWrapped, FormatAAD("pembayaran", "bukti", "pay-1"))
	if err != nil || !bytes.Equal(gotBukti, plain) {
		t.Fatalf("rotated buktiKP2 failed decrypting old bukti envelope: err=%v", err)
	}
	if _, err := kycKP2.DecryptWithAAD(envWrapped, FormatAAD("pembayaran", "bukti", "pay-1")); err == nil {
		t.Fatal("rotated kycKP2 MUST NOT unwrap bukti DEK")
	}
}
