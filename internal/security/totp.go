package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
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

	counter := uint64(t.Unix() / 30)
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
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}

	for _, delta := range []int64{-30, 0, 30} {
		candidate, err := GenerateTOTPCode(secretBase32, t.Add(time.Duration(delta)*time.Second))
		if err == nil && candidate == code {
			return true
		}
	}
	return false
}

// GenerateTOTPURI menghasilkan tautan URI otpauth untuk aplikasi authenticator (Google Auth dsb)
func GenerateTOTPURI(secretBase32, accountName, issuer string) string {
	escapedIssuer := url.QueryEscape(issuer)
	escapedAccount := url.QueryEscape(accountName)
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		escapedIssuer, escapedAccount, secretBase32, escapedIssuer)
}
