package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// GenerateOpaqueToken menghasilkan token acak kriptografis URL-safe dan hash SHA-256 hex untuk disimpan di DB.
func GenerateOpaqueToken(numBytes int) (rawToken string, tokenHash string, err error) {
	if numBytes < 16 {
		numBytes = 32
	}
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("gagal membangkitkan token acak: %w", err)
	}
	rawToken = base64.RawURLEncoding.EncodeToString(b)
	tokenHash = HashOpaqueToken(rawToken)
	return rawToken, tokenHash, nil
}

// HashOpaqueToken menghitung hash SHA-256 hex dari token atau kode aktivasi mentah.
func HashOpaqueToken(rawToken string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawToken)))
	return hex.EncodeToString(sum[:])
}

// EncryptEnvelopeFile mengenkripsi byte berkas memakai DEK acak per berkas, lalu membungkus DEK memakai KeyProvider utama.
func EncryptEnvelopeFile(kp KeyProvider, plain []byte, aad []byte) (cipherFile []byte, wrappedDEK []byte, keyID string, err error) {
	if kp == nil {
		return nil, nil, "", fmt.Errorf("key provider wajib tersedia untuk enkripsi berkas KYC")
	}
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, nil, "", fmt.Errorf("gagal membangkitkan DEK: %w", err)
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, nil, "", fmt.Errorf("aes.NewCipher DEK: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, "", fmt.Errorf("cipher.NewGCM DEK: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, "", fmt.Errorf("rand nonce DEK: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plain, aad)
	wrapped, err := kp.EncryptWithAAD(dek, aad)
	if err != nil {
		return nil, nil, "", fmt.Errorf("gagal membungkus DEK: %w", err)
	}
	return sealed, wrapped, kp.KeyID(), nil
}

// DecryptEnvelopeFile membuka DEK memakai KeyProvider dan mendekripsi byte berkas.
func DecryptEnvelopeFile(kp KeyProvider, cipherFile, wrappedDEK, aad []byte) ([]byte, error) {
	if kp == nil {
		return nil, fmt.Errorf("key provider wajib tersedia untuk dekripsi berkas KYC")
	}
	dek, err := kp.DecryptWithAAD(wrappedDEK, aad)
	if err != nil {
		return nil, fmt.Errorf("gagal membuka DEK: %w", err)
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher DEK: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM DEK: %w", err)
	}
	ns := gcm.NonceSize()
	if len(cipherFile) < ns+16 {
		return nil, ErrCiphertextTooShort
	}
	plain, err := gcm.Open(nil, cipherFile[:ns], cipherFile[ns:], aad)
	if err != nil {
		return nil, fmt.Errorf("dekripsi berkas gagal: %w", err)
	}
	return plain, nil
}

// MaskNIK menyamarkan NIK 16 digit menjadi "3201********0001".
func MaskNIK(nik string) string {
	n := strings.TrimSpace(nik)
	if len(n) <= 8 {
		return strings.Repeat("*", len(n))
	}
	return n[:4] + strings.Repeat("*", len(n)-8) + n[len(n)-4:]
}

// NormalizePhone menormalkan nomor HP Indonesia ke format 08... hanya digit.
func NormalizePhone(raw string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if strings.HasPrefix(digits, "62") && len(digits) > 4 {
		return "0" + digits[2:]
	}
	return digits
}

// MaskPhone menyamarkan nomor HP menjadi "0812****8901".
func MaskPhone(hp string) string {
	p := strings.TrimSpace(hp)
	if len(p) <= 6 {
		return strings.Repeat("*", len(p))
	}
	return p[:4] + strings.Repeat("*", len(p)-7) + p[len(p)-3:]
}

// NeutralizeCSVCell mencegah formula injection pada spreadsheet (=, +, -, @).
func NeutralizeCSVCell(val string) string {
	if val == "" {
		return val
	}
	trimmed := strings.TrimLeft(val, " \t\r\n")
	if trimmed == "" {
		return val
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + val
	default:
		return val
	}
}
