package security

import (
	"crypto/hmac"
	"crypto/rand"
	// #nosec G505 -- SHA-1 diwajibkan oleh spesifikasi standar TOTP RFC 6238
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// GenerateTOTPSecret menghasilkan secret acak 20-byte (160 bit) terenkode Base32 tanpa padding (RFC 6238)
func GenerateTOTPSecret() (string, error) {
	bytes := make([]byte, 20)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("gagal menghasilkan secret acak: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes), nil
}

// GenerateTOTPCode menghasilkan 6 digit kode TOTP berdasarkan waktu t (RFC 6238)
func GenerateTOTPCode(secretBase32 string, t time.Time) (string, error) {
	cleanSecret := strings.ToUpper(strings.TrimSpace(secretBase32))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cleanSecret)
	if err != nil {
		// Coba dengan padding jika gagal
		key, err = base32.StdEncoding.DecodeString(cleanSecret)
		if err != nil {
			return "", fmt.Errorf("secret Base32 tidak valid: %w", err)
		}
	}

	unixSec := t.Unix()
	if unixSec < 0 {
		return "", fmt.Errorf("timestamp Unix negatif tidak valid: %d", unixSec)
	}
	// #nosec G115 -- unixSec divalidasi >= 0 sebelum pembagian dan konversi uint64 untuk counter TOTP RFC 6238
	counter := uint64(unixSec / 30)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	binaryCode := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	code := binaryCode % 1000000

	return fmt.Sprintf("%06d", code), nil
}

// ValidateTOTPCode memverifikasi kode 6-digit dengan toleransi drift waktu +/- 30 detik
func ValidateTOTPCode(secretBase32 string, code string, t time.Time) bool {
	valid, _ := ValidateTOTPCodeWithStep(secretBase32, code, t)
	return valid
}

// ValidateTOTPCodeWithStep memverifikasi kode 6-digit dan mengembalikan step waktu yang cocok jika valid
func ValidateTOTPCodeWithStep(secretBase32 string, code string, t time.Time) (bool, int64) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false, 0
	}

	for _, delta := range []int64{0, -30, 30} {
		targetTime := t.Add(time.Duration(delta) * time.Second)
		candidate, err := GenerateTOTPCode(secretBase32, targetTime)
		if err == nil && candidate == code {
			matchedStep := targetTime.Unix() / 30
			return true, matchedStep
		}
	}
	return false, 0
}

// GenerateTOTPURI menghasilkan tautan URI otpauth untuk aplikasi authenticator (Google Auth dsb)
func GenerateTOTPURI(secretBase32, accountName, issuer string) string {
	escapedIssuer := url.QueryEscape(issuer)
	escapedAccount := url.QueryEscape(accountName)
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		escapedIssuer, escapedAccount, secretBase32, escapedIssuer)
}

// GenerateRecoveryCodes menghasilkan sejumlah kode pemulihan acak 8-karakter hex (e.g. "a1b2-c3d4")
func GenerateRecoveryCodes(count int) ([]string, error) {
	if count <= 0 {
		count = 8
	}
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("gagal menghasilkan recovery code: %w", err)
		}
		codes[i] = fmt.Sprintf("%04x-%04x", b[:2], b[2:])
	}
	return codes, nil
}

// HashRecoveryCode menghasilkan hash SHA-256 dari kode pemulihan untuk disimpan aman
func HashRecoveryCode(code string) string {
	clean := strings.ToLower(strings.TrimSpace(code))
	h := sha256.Sum256([]byte(clean))
	return hex.EncodeToString(h[:])
}
