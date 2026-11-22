package security

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrCiphertextTooShort = errors.New("ciphertext terlalu pendek")
	ErrInvalidKeySize     = errors.New("ukuran kunci AES-256 wajib 32 byte")
)

// ValidateSecretKey memastikan kunci kriptografis memenuhi standar keamanan minimal (32 byte).
func ValidateSecretKey(name string, key []byte) error {
	if len(key) < 32 {
		return fmt.Errorf("secret %s tidak valid: panjang minimal 32 byte (didapat %d byte)", name, len(key))
	}
	return nil
}

// FormatAAD menyusun Associated Authenticated Data (AAD) untuk AES-256-GCM.
func FormatAAD(tableName, columnName, recordID string) []byte {
	return []byte(fmt.Sprintf("%s:%s:%s", tableName, columnName, recordID))
}

// KeyProvider mendefinisikan manajemen kunci enkripsi AES-256-GCM dengan AAD dan rotasi key ID.
type KeyProvider interface {
	KeyID() string
	EncryptWithAAD(plaintext, aad []byte) ([]byte, error)
	DecryptWithAAD(ciphertext, aad []byte) ([]byte, error)
}

// MasterKeyProvider mengelola enkripsi AES-256-GCM dengan prefix versi & key ID.
type MasterKeyProvider struct {
	version string
	keyID   string
	key     []byte
}

func NewMasterKeyProvider(version, keyID string, masterKey []byte) (*MasterKeyProvider, error) {
	if len(masterKey) != 32 {
		return nil, ErrInvalidKeySize
	}
	if version == "" {
		version = "v1"
	}
	if keyID == "" {
		keyID = "k1"
	}
	return &MasterKeyProvider{version: version, keyID: keyID, key: masterKey}, nil
}

func (kp *MasterKeyProvider) KeyID() string {
	return kp.version + ":" + kp.keyID
}

func (kp *MasterKeyProvider) EncryptWithAAD(plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(kp.key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("rand nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, plaintext, aad)
	prefix := []byte(fmt.Sprintf("%s:%s:", kp.version, kp.keyID))
	result := make([]byte, len(prefix)+len(nonce)+len(sealed))
	copy(result, prefix)
	copy(result[len(prefix):], nonce)
	copy(result[len(prefix)+len(nonce):], sealed)
	return result, nil
}

func (kp *MasterKeyProvider) DecryptWithAAD(ciphertext, aad []byte) ([]byte, error) {
	prefix := []byte(fmt.Sprintf("%s:%s:", kp.version, kp.keyID))
	if len(ciphertext) < len(prefix)+12+16 {
		return nil, ErrCiphertextTooShort
	}
	if !bytes.HasPrefix(ciphertext, prefix) {
		return nil, errors.New("versi kunci atau key_id tidak cocok")
	}
	data := ciphertext[len(prefix):]
	block, err := aes.NewCipher(kp.key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, ErrCiphertextTooShort
	}
	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], aad)
	if err != nil {
		return nil, fmt.Errorf("gcm.Open: %w", err)
	}
	return plaintext, nil
}

// HashBlindIndex menghitung HMAC-SHA256 hex untuk pencarian deterministik terenkripsi.
func HashBlindIndex(value string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// BlindIndexKeyVersion menghasilkan tag versi kunci blind index non-rahasia dari secret aktif.
func BlindIndexKeyVersion(secret []byte) string {
	sum := sha256.Sum256(secret)
	return "bi-" + hex.EncodeToString(sum[:4])
}

// HashPassword membuat hash bcrypt dengan biaya default aman (12).
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(b), err
}

// CheckPassword memvalidasi kecocokan password plaintext terhadap hash bcrypt.
func CheckPassword(password, hash string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// DummyBcryptCheck menjalankan bcrypt waktu konstan saat akun tidak ditemukan.
func DummyBcryptCheck(password string, dummyHash string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
}

// ConstantTimeEqualHex membandingkan dua string secara waktu konstan.
func ConstantTimeEqualHex(a, b string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// RedactDSN menyamarkan password pada string DSN koneksi database agar tidak bocor di log.
func RedactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err == nil && u.User != nil {
		if _, hasPass := u.User.Password(); hasPass {
			u.User = url.UserPassword(u.User.Username(), "******")
			return u.String()
		}
	}
	reURL := regexp.MustCompile(`://([^:]+):([^@]+)@`)
	if reURL.MatchString(dsn) {
		return reURL.ReplaceAllString(dsn, `://$1:******@`)
	}
	reKW := regexp.MustCompile(`(?i)(password=)[^\s]+`)
	return reKW.ReplaceAllString(dsn, `${1}******`)
}
