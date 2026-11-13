package security

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// HashPasswordWithCost menghasilkan hash bcrypt dengan biaya yang dapat dikonfigurasi (BCRYPT_COST).
func HashPasswordWithCost(password string, cost int) (string, error) {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return "", fmt.Errorf("bcrypt cost harus antara %d dan %d, dapat %d", bcrypt.MinCost, bcrypt.MaxCost, cost)
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	return string(bytes), err
}

// DecodeSecretMaterial menguraikan materi kunci dari format hex (>= 32 byte), base64 (>= 32 byte), atau raw UTF-8.
func DecodeSecretMaterial(secret string) []byte {
	s := strings.TrimSpace(secret)
	if dec, err := hex.DecodeString(s); err == nil && len(dec) >= 32 {
		return dec
	}
	if dec, err := base64.StdEncoding.DecodeString(s); err == nil && len(dec) >= 32 {
		return dec
	}
	return []byte(s)
}

// MultiKeyProvider mendukung rotasi kunci AES-256-GCM tanpa waktu henti:
// enkripsi baru memakai active provider (version:keyID), sedangkan dekripsi
// memilih provider berdasarkan prefix "version:keyID:" pada ciphertext.
type MultiKeyProvider struct {
	activeVersion string
	activeKeyID   string
	active        *MasterKeyProvider
	providers     map[string]*MasterKeyProvider
}

// NewMultiKeyProviderFromConfig membangun MultiKeyProvider dari kunci aktif (AES_KEY_VERSION, AES_KEY_ID, AES_MASTER_KEY)
// dan kunci historis pada AES_PREVIOUS_KEYS dengan format "v1:k1=<hex/b64/raw>;v1:k2=<hex/b64/raw>".
func NewMultiKeyProviderFromConfig(activeVersion, activeKeyID, activeKeyMaterial, previousKeysSpec string) (*MultiKeyProvider, error) {
	if strings.TrimSpace(activeVersion) == "" {
		activeVersion = "v1"
	}
	if strings.TrimSpace(activeKeyID) == "" {
		activeKeyID = "k1"
	}
	activeBytes := DecodeSecretMaterial(activeKeyMaterial)
	if len(activeBytes) < 32 {
		return nil, ErrInvalidKeySize
	}
	activeKP, err := NewMasterKeyProvider(activeVersion, activeKeyID, activeBytes[:32])
	if err != nil {
		return nil, err
	}

	providers := map[string]*MasterKeyProvider{
		activeVersion + ":" + activeKeyID + ":": activeKP,
	}

	if strings.TrimSpace(previousKeysSpec) != "" {
		entries := strings.FieldsFunc(previousKeysSpec, func(r rune) bool {
			return r == ';' || r == ','
		})
		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			eq := strings.IndexByte(entry, '=')
			if eq <= 0 {
				return nil, fmt.Errorf("entri AES_PREVIOUS_KEYS tidak valid %q (harapkan version:key_id=material)", entry)
			}
			tag := strings.TrimSpace(entry[:eq])
			mat := DecodeSecretMaterial(entry[eq+1:])
			if len(mat) < 32 {
				return nil, fmt.Errorf("materi kunci AES_PREVIOUS_KEYS untuk %q harus minimal 32 byte", tag)
			}
			parts := strings.SplitN(tag, ":", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return nil, fmt.Errorf("identifier AES_PREVIOUS_KEYS tidak valid %q (harapkan version:key_id)", tag)
			}
			oldKP, err := NewMasterKeyProvider(parts[0], parts[1], mat[:32])
			if err != nil {
				return nil, err
			}
			providers[parts[0]+":"+parts[1]+":"] = oldKP
		}
	}

	return &MultiKeyProvider{
		activeVersion: activeVersion,
		activeKeyID:   activeKeyID,
		active:        activeKP,
		providers:     providers,
	}, nil
}

// EncryptWithAAD selalu mengenkripsi memakai kunci aktif (AES_KEY_VERSION:AES_KEY_ID).
func (m *MultiKeyProvider) EncryptWithAAD(plaintext, aad []byte) ([]byte, error) {
	return m.active.EncryptWithAAD(plaintext, aad)
}

// DecryptWithAAD memilih provider sesuai prefix version:keyID agar ciphertext lama tetap terbaca setelah rotasi.
func (m *MultiKeyProvider) DecryptWithAAD(ciphertext, aad []byte) ([]byte, error) {
	if bytes.HasPrefix(ciphertext, []byte(m.activeVersion+":"+m.activeKeyID+":")) {
		return m.active.DecryptWithAAD(ciphertext, aad)
	}
	for prefix, kp := range m.providers {
		if bytes.HasPrefix(ciphertext, []byte(prefix)) {
			return kp.DecryptWithAAD(ciphertext, aad)
		}
	}
	return nil, errors.New("versi kunci atau key_id tidak dikenali pada konfigurasi AES aktif maupun riwayat rotasi")
}
