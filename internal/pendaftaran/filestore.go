package pendaftaran

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/security"
)

type StoredKYCFile struct {
	StorageKey  string
	KeyID       string
	DEKWrapped  []byte
	MimeType    string
	SizeBytes   int64
	WidthPx     int32
	HeightPx    int32
	SHA256Plain string
}

type KYCFileStore struct {
	baseDir  string
	kp       security.KeyProvider
	maxBytes int64
}

func NewKYCFileStore(baseDir string, kp security.KeyProvider, maxBytes int64) (*KYCFileStore, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, fmt.Errorf("STORAGE_KYC_DIR wajib diisi")
	}
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("buat direktori KYC %s: %w", abs, err)
	}
	return &KYCFileStore{baseDir: abs, kp: kp, maxBytes: maxBytes}, nil
}

func validateImageBytes(raw []byte, maxBytes int64) (mime string, width, height int32, err error) {
	if int64(len(raw)) == 0 || int64(len(raw)) > maxBytes {
		return "", 0, 0, response.NewDomainError(http.StatusBadRequest, "INVALID_FILE_SIZE", "Ukuran berkas KYC tidak valid atau melebihi batas")
	}
	mime = http.DetectContentType(raw)
	if mime != "image/jpeg" && mime != "image/png" {
		return "", 0, 0, response.NewDomainError(http.StatusBadRequest, "INVALID_MIME_TYPE", "Berkas KYC wajib berupa gambar JPEG atau PNG asli")
	}
	cfg, _, decErr := image.DecodeConfig(bytes.NewReader(raw))
	if decErr != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8000 || cfg.Height > 8000 {
		return "", 0, 0, response.NewDomainError(http.StatusBadRequest, "INVALID_IMAGE_DIMENSIONS", "Berkas gambar rusak atau dimensi melebihi 8000x8000px")
	}
	return mime, int32(cfg.Width), int32(cfg.Height), nil // #nosec G115 -- dibatasi <= 8000
}

func (fs *KYCFileStore) safePath(storageKey string) (string, error) {
	clean := filepath.Base(strings.TrimSpace(storageKey))
	if clean == "" || clean == "." || clean != storageKey {
		return "", fmt.Errorf("kunci penyimpanan tidak valid")
	}
	full := filepath.Join(fs.baseDir, clean)
	if !strings.HasPrefix(full, fs.baseDir+string(filepath.Separator)) {
		return "", fmt.Errorf("path keluar dari direktori KYC")
	}
	return full, nil
}

func (fs *KYCFileStore) SaveEncrypted(pendaftaranID, tipe string, raw []byte) (*StoredKYCFile, error) {
	mime, w, h, err := validateImageBytes(raw, fs.maxBytes)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	aad := security.FormatAAD("dokumen_kyc", tipe, pendaftaranID)
	cipherBytes, wrappedDEK, keyID, err := security.EncryptEnvelopeFile(fs.kp, raw, aad)
	if err != nil {
		return nil, fmt.Errorf("enkripsi berkas KYC: %w", err)
	}
	storageKey := uuid.NewString() + ".enc"
	target, err := fs.safePath(storageKey)
	if err != nil {
		return nil, err
	}
	// #nosec G304,G703 -- target diverifikasi safePath berada di bawah baseDir dengan UUID acak
	if err := os.WriteFile(target, cipherBytes, 0o600); err != nil {
		return nil, fmt.Errorf("simpan berkas KYC terenkripsi: %w", err)
	}
	return &StoredKYCFile{
		StorageKey:  storageKey,
		KeyID:       keyID,
		DEKWrapped:  wrappedDEK,
		MimeType:    mime,
		SizeBytes:   int64(len(raw)),
		WidthPx:     w,
		HeightPx:    h,
		SHA256Plain: hex.EncodeToString(sum[:]),
	}, nil
}

func (fs *KYCFileStore) LoadDecrypted(pendaftaranID, tipe, storageKey string, wrappedDEK []byte) ([]byte, error) {
	target, err := fs.safePath(storageKey)
	if err != nil {
		return nil, err
	}
	// #nosec G304,G703 -- target diverifikasi safePath berada di bawah baseDir
	cipherBytes, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("baca berkas KYC terenkripsi: %w", err)
	}
	aad := security.FormatAAD("dokumen_kyc", tipe, pendaftaranID)
	return security.DecryptEnvelopeFile(fs.kp, cipherBytes, wrappedDEK, aad)
}

func (fs *KYCFileStore) Remove(storageKey string) {
	if target, err := fs.safePath(storageKey); err == nil {
		_ = os.Remove(target)
	}
}
