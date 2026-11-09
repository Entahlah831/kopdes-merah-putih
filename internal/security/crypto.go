package security

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/hkdf"
)

var (
	ErrCiphertextTooShort = errors.New("ciphertext terlalu pendek")
	ErrInvalidKeySize     = errors.New("ukuran kunci AES-256 wajib 32 byte")
)

// ValidateSecretKey memastikan kunci kriptografis memenuhi standar keamanan minimal (32 byte)
func ValidateSecretKey(name string, key []byte) error {
	if len(key) < 32 {
		return fmt.Errorf("secret %s tidak valid: panjang minimal 32 byte (didapat %d byte)", name, len(key))
	}
	return nil
}

// FormatAAD menyusun Associated Authenticated Data (AAD) untuk AES-256-GCM
// Format: "table:column:record_id" untuk mencegah ciphertext shuffling/transposition attack
func FormatAAD(tableName, columnName, recordID string) []byte {
	return []byte(fmt.Sprintf("%s:%s:%s", tableName, columnName, recordID))
}

// KeyProvider mendefinisikan antarmuka manajemen kunci enkripsi dengan AAD
type KeyProvider interface {
	EncryptWithAAD(plaintext, aad []byte) ([]byte, error)
	DecryptWithAAD(ciphertext, aad []byte) ([]byte, error)
}

// MasterKeyProvider mengelola enkripsi AES-256-GCM dengan metadata versi kunci & key ID
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
	return &MasterKeyProvider{
		version: version,
		keyID:   keyID,
		key:     masterKey,
	}, nil
}

// EncryptWithAAD mengenkripsi data dengan AES-256-GCM, AAD, dan prefix versi kunci ("v1:key_id:nonce+ciphertext")
func (kp *MasterKeyProvider) EncryptWithAAD(plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(kp.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plaintext, aad)

	prefix := []byte(fmt.Sprintf("%s:%s:", kp.version, kp.keyID))
	result := make([]byte, len(prefix)+len(nonce)+len(sealed))
	copy(result, prefix)
	copy(result[len(prefix):], nonce)
	copy(result[len(prefix)+len(nonce):], sealed)
	return result, nil
}

// DecryptWithAAD mendekripsi data dan memvalidasi integritas AAD serta prefix versi kunci
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
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, ErrCiphertextTooShort
	}
	nonce, actualCiphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, actualCiphertext, aad)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

// DeriveTenantBlindIndexKey menghasilkan sub-kunci HMAC per-koperasi menggunakan HKDF-SHA256
// Mencegah korelasi blind index lintas koperasi (UU 27/2022 PDP)
func DeriveTenantBlindIndexKey(masterSecret []byte, koperasiID uuid.UUID) ([]byte, error) {
	if len(masterSecret) < 32 {
		return nil, ErrInvalidKeySize
	}
	hkdfReader := hkdf.New(sha256.New, masterSecret, koperasiID[:], []byte("kopdes-blind-index-v1"))
	derivedKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, derivedKey); err != nil {
		return nil, err
	}
	return derivedKey, nil
}

// HashTenantBlindIndex menghitung blind index terisolasi per koperasi
func HashTenantBlindIndex(value string, masterSecret []byte, koperasiID uuid.UUID) (string, error) {
	derivedKey, err := DeriveTenantBlindIndexKey(masterSecret, koperasiID)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, derivedKey)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// HashBlindIndex menghitung HMAC-SHA256 hex global (misal untuk email pencarian akun pengguna)
func HashBlindIndex(value string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// EncryptAESGCM mengenkripsi data standar tanpa AAD (legacy support)
func EncryptAESGCM(plaintext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptAESGCM mendekripsi data terenkripsi standar tanpa AAD (legacy support)
func DecryptAESGCM(ciphertext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrCiphertextTooShort
	}

	nonce, actualCiphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, actualCiphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// HashPassword membuat hash bcrypt yang aman dari plaintext password (biaya default = 12 untuk T5)
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}

// CheckPassword memvalidasi kecocokan password plaintext terhadap hash bcrypt
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// RedactDSN menyamarkan password pada string DSN/URL koneksi database agar tidak bocor di log
func RedactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err == nil && u.User != nil {
		if _, hasPass := u.User.Password(); hasPass {
			u.User = url.UserPassword(u.User.Username(), "******")
			return u.String()
		}
	}

	// Regex fallback jika format bukan standard URL
	reURL := regexp.MustCompile(`://([^:]+):([^@]+)@`)
	if reURL.MatchString(dsn) {
		return reURL.ReplaceAllString(dsn, `://$1:******@`)
	}
	reKW := regexp.MustCompile(`(?i)(password=)[^\s]+`)
	return reKW.ReplaceAllString(dsn, `${1}******`)
}
