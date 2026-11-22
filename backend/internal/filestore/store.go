package filestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/security"
)

const (
	NamespaceKYC   = "kyc"
	NamespaceBukti = "bukti"
)

// ErrStorageUnavailable menandakan penyimpanan berkas sedang tidak tersedia (HTTP 503).
var ErrStorageUnavailable = response.NewDomainError(
	http.StatusServiceUnavailable,
	"STORAGE_UNAVAILABLE",
	"Penyimpanan berkas terenkripsi sedang tidak tersedia",
)

// ImageMeta berisi hasil validasi gambar JPEG/PNG asli.
type ImageMeta struct {
	MimeType  string
	Ext       string
	SHA256Hex string
	WidthPx   int32
	HeightPx  int32
	SizeBytes int64
}

// StoredObject berisi metadata berkas terenkripsi yang disimpan ke FileStore.
type StoredObject struct {
	ObjectKey  string
	KeyID      string
	DEKWrapped []byte
	SizeBytes  int64
}

// FileStore mendefinisikan kontrak penyimpanan berkas terenkripsi lintas backend (lokal / remote).
type FileStore interface {
	SaveEncrypted(ctx context.Context, namespace, objectKey string, plaintext, aad []byte) (StoredObject, error)
	LoadDecrypted(ctx context.Context, namespace, objectKey string, wrappedDEK, aad []byte) ([]byte, error)
	Delete(ctx context.Context, namespace, objectKey string) error
	Exists(ctx context.Context, namespace, objectKey string) (bool, error)
	HealthCheck(ctx context.Context) error
}

// ValidateImage memvalidasi ukuran, magic bytes JPEG/PNG, dan dimensi maksimal 8000x8000px.
func ValidateImage(raw []byte, maxBytes int64) (ImageMeta, error) {
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024
	}
	if int64(len(raw)) == 0 || int64(len(raw)) > maxBytes {
		return ImageMeta{}, response.NewDomainError(http.StatusBadRequest, "INVALID_FILE_SIZE", "Ukuran berkas gambar tidak valid atau melebihi batas")
	}
	mime := http.DetectContentType(raw)
	ext := imageExtFromMIME(mime)
	if ext == "" {
		return ImageMeta{}, response.NewDomainError(http.StatusBadRequest, "INVALID_MIME_TYPE", "Berkas wajib berupa gambar JPEG atau PNG asli")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8000 || cfg.Height > 8000 {
		return ImageMeta{}, response.NewDomainError(http.StatusBadRequest, "INVALID_IMAGE_DIMENSIONS", "Berkas gambar rusak atau dimensi melebihi 8000x8000px")
	}
	sum := sha256.Sum256(raw)
	return ImageMeta{
		MimeType:  mime,
		Ext:       ext,
		SHA256Hex: hex.EncodeToString(sum[:]),
		WidthPx:   int32(cfg.Width),  // #nosec G115 -- dibatasi <= 8000
		HeightPx:  int32(cfg.Height), // #nosec G115 -- dibatasi <= 8000
		SizeBytes: int64(len(raw)),
	}, nil
}

func imageExtFromMIME(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	default:
		return ""
	}
}

// NamespaceConfig mengatur direktori dan penyedia kunci enkripsi per ruang nama berkas.
type NamespaceConfig struct {
	Dir               string
	FallbackDirs      []string
	KeyProvider       security.KeyProvider
	LegacyKeyProvider security.KeyProvider
}

type localNamespace struct {
	dir          string
	fallbackDirs []string
	kp           security.KeyProvider
	legacyKP     security.KeyProvider
}

// LocalFileStore menyimpan berkas terenkripsi AES-256-GCM (amplop DEK) pada sistem berkas lokal.
type LocalFileStore struct {
	namespaces map[string]localNamespace
}

// NewLocalFileStore menyiapkan penyimpanan terenkripsi per namespace (mis. kyc, bukti).
func NewLocalFileStore(cfg map[string]NamespaceConfig) (*LocalFileStore, error) {
	nsMap := make(map[string]localNamespace, len(cfg))
	for name, entry := range cfg {
		nsName := strings.ToLower(strings.TrimSpace(name))
		dirRaw := strings.TrimSpace(entry.Dir)
		if nsName == "" || dirRaw == "" {
			return nil, fmt.Errorf("direktori namespace %q wajib diisi", name)
		}
		absDir, err := filepath.Abs(dirRaw)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(absDir, 0o700); err != nil {
			return nil, fmt.Errorf("buat direktori storage %s (%s): %w", nsName, absDir, err)
		}
		fallbacks := normalizeFallbackDirs(absDir, entry.FallbackDirs)
		nsMap[nsName] = localNamespace{
			dir: absDir, fallbackDirs: fallbacks, kp: entry.KeyProvider, legacyKP: entry.LegacyKeyProvider,
		}
	}
	return &LocalFileStore{namespaces: nsMap}, nil
}

func normalizeFallbackDirs(primary string, rawDirs []string) []string {
	var out []string
	for _, fd := range rawDirs {
		if strings.TrimSpace(fd) == "" {
			continue
		}
		if abs, err := filepath.Abs(strings.TrimSpace(fd)); err == nil && abs != primary {
			out = append(out, abs)
		}
	}
	return out
}

func SafeObjectPath(baseDir, objectKey string) (string, error) {
	trimmed := strings.TrimSpace(objectKey)
	clean := filepath.Base(trimmed)
	if clean == "" || clean == "." || clean == ".." || clean != trimmed || strings.ContainsAny(trimmed, `/\:`) {
		return "", fmt.Errorf("kunci objek penyimpanan tidak valid")
	}
	full := filepath.Join(baseDir, clean)
	if !strings.HasPrefix(full, baseDir+string(filepath.Separator)) {
		return "", fmt.Errorf("path objek keluar dari direktori namespace")
	}
	return full, nil
}

func (s *LocalFileStore) resolveNS(namespace string) (localNamespace, error) {
	if s == nil {
		return localNamespace{}, ErrStorageUnavailable
	}
	ns, ok := s.namespaces[strings.ToLower(strings.TrimSpace(namespace))]
	if !ok {
		return localNamespace{}, fmt.Errorf("namespace penyimpanan %q tidak dikonfigurasi", namespace)
	}
	return ns, nil
}

func (s *LocalFileStore) SaveEncrypted(ctx context.Context, namespace, objectKey string, plaintext, aad []byte) (StoredObject, error) {
	if err := ctx.Err(); err != nil {
		return StoredObject{}, err
	}
	ns, err := s.resolveNS(namespace)
	if err != nil {
		return StoredObject{}, err
	}
	if ns.kp == nil {
		return StoredObject{}, ErrStorageUnavailable
	}
	target, err := SafeObjectPath(ns.dir, objectKey)
	if err != nil {
		return StoredObject{}, err
	}
	cipherBytes, wrappedDEK, keyID, err := security.EncryptEnvelopeFile(ns.kp, plaintext, aad)
	if err != nil {
		return StoredObject{}, fmt.Errorf("enkripsi berkas %s: %w", namespace, err)
	}
	if err := writeAtomicFile(ns.dir, target, cipherBytes); err != nil {
		return StoredObject{}, fmt.Errorf("simpan berkas terenkripsi %s: %w", namespace, err)
	}
	return StoredObject{
		ObjectKey:  filepath.Base(target),
		KeyID:      keyID,
		DEKWrapped: wrappedDEK,
		SizeBytes:  int64(len(plaintext)),
	}, nil
}

func writeAtomicFile(dir, target string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Chmod(tmpName, 0o600)
	if err := os.Rename(tmpName, target); err != nil { // #nosec G703 -- target tervalidasi SafeObjectPath di dalam dir
		return err
	}
	cleanup = false
	return nil
}

func (s *LocalFileStore) LoadDecrypted(ctx context.Context, namespace, objectKey string, wrappedDEK, aad []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ns, err := s.resolveNS(namespace)
	if err != nil {
		return nil, err
	}
	cipherBytes, err := readFromDirs(append([]string{ns.dir}, ns.fallbackDirs...), objectKey)
	if err != nil {
		return nil, err
	}
	plain, err := security.DecryptEnvelopeFile(ns.kp, cipherBytes, wrappedDEK, aad)
	if err != nil && ns.legacyKP != nil {
		if plainLegacy, lErr := security.DecryptEnvelopeFile(ns.legacyKP, cipherBytes, wrappedDEK, aad); lErr == nil {
			return plainLegacy, nil
		}
	}
	return plain, err
}

func readFromDirs(dirs []string, objectKey string) ([]byte, error) {
	var lastErr error
	for _, dir := range dirs {
		target, err := SafeObjectPath(dir, objectKey)
		if err != nil {
			return nil, err
		}
		// #nosec G304,G703 -- target diverifikasi SafeObjectPath berada tepat di bawah dir
		raw, rErr := os.ReadFile(target)
		if rErr == nil {
			return raw, nil
		}
		lastErr = rErr
	}
	if lastErr == nil {
		return nil, os.ErrNotExist
	}
	return nil, lastErr
}

func (s *LocalFileStore) Delete(ctx context.Context, namespace, objectKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ns, err := s.resolveNS(namespace)
	if err != nil {
		return err
	}
	for _, dir := range append([]string{ns.dir}, ns.fallbackDirs...) {
		target, pErr := SafeObjectPath(dir, objectKey)
		if pErr != nil {
			return pErr
		}
		if rErr := os.Remove(target); rErr != nil && !errors.Is(rErr, os.ErrNotExist) {
			return rErr
		}
	}
	return nil
}

func (s *LocalFileStore) Exists(ctx context.Context, namespace, objectKey string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	ns, err := s.resolveNS(namespace)
	if err != nil {
		return false, err
	}
	for _, dir := range append([]string{ns.dir}, ns.fallbackDirs...) {
		target, pErr := SafeObjectPath(dir, objectKey)
		if pErr != nil {
			return false, pErr
		}
		if _, sErr := os.Stat(target); sErr == nil {
			return true, nil
		}
	}
	return false, nil
}

func (s *LocalFileStore) HealthCheck(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || len(s.namespaces) == 0 {
		return ErrStorageUnavailable
	}
	for name, ns := range s.namespaces {
		if err := probeWritableDir(ns.dir); err != nil {
			return fmt.Errorf("namespace %s tidak dapat ditulis: %w", name, err)
		}
	}
	return nil
}

func probeWritableDir(dir string) error {
	tmp, err := os.CreateTemp(dir, ".health-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(name)
	return nil
}
