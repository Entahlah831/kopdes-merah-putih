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

// HashPasswordWithCost menghasilkan hash bcrypt dengan biaya yang dikonfigurasi.
func HashPasswordWithCost(password string, cost int) (string, error) {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return "", fmt.Errorf("bcrypt cost harus antara %d dan %d, dapat %d", bcrypt.MinCost, bcrypt.MaxCost, cost)
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	return string(b), err
}

// DecodeSecretMaterial menguraikan materi kunci dari format hex (>= 32 byte), base64 (>= 32 byte), atau raw UTF-8.
func DecodeSecretMaterial(secret string) []byte {
	s := strings.TrimSpace(secret)
	if idx := strings.IndexByte(s, '='); idx > 0 && strings.Contains(s[:idx], ":") {
		s = strings.TrimSpace(s[idx+1:])
	}
	if dec, err := hex.DecodeString(s); err == nil && len(dec) >= 32 {
		return dec
	}
	if dec, err := base64.StdEncoding.DecodeString(s); err == nil && len(dec) >= 32 {
		return dec
	}
	return []byte(s)
}

// ParseKeyIDSpec mengekstrak version & keyID dari spec ("v1:k1" -> "v1","k1").
func ParseKeyIDSpec(spec string) (string, string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "v1", "k1"
	}
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1]
	}
	return "v1", spec
}

// ParseMasterKeySpec mengekstrak optional prefix "ver:id=material" dari AES_MASTER_KEY.
func ParseMasterKeySpec(spec string) (string, string, string) {
	s := strings.TrimSpace(spec)
	if idx := strings.IndexByte(s, '='); idx > 0 && strings.Contains(s[:idx], ":") {
		ver, kid := ParseKeyIDSpec(s[:idx])
		return ver, kid, strings.TrimSpace(s[idx+1:])
	}
	return "v1", "k1", s
}

// MultiKeyProvider mendukung rotasi kunci AES-256-GCM tanpa waktu henti.
type MultiKeyProvider struct {
	activeVersion string
	activeKeyID   string
	active        *MasterKeyProvider
	providers     map[string]*MasterKeyProvider
}

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
		entries := strings.FieldsFunc(previousKeysSpec, func(r rune) bool { return r == ';' || r == ',' })
		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			eq := strings.IndexByte(entry, '=')
			if eq <= 0 {
				return nil, fmt.Errorf("entri AES_PREVIOUS_KEYS tidak valid %q", entry)
			}
			tag := strings.TrimSpace(entry[:eq])
			mat := DecodeSecretMaterial(entry[eq+1:])
			if len(mat) < 32 {
				return nil, fmt.Errorf("materi kunci AES_PREVIOUS_KEYS untuk %q harus >= 32 byte", tag)
			}
			ver, kid := ParseKeyIDSpec(tag)
			oldKP, err := NewMasterKeyProvider(ver, kid, mat[:32])
			if err != nil {
				return nil, err
			}
			providers[ver+":"+kid+":"] = oldKP
		}
	}

	return &MultiKeyProvider{
		activeVersion: activeVersion,
		activeKeyID:   activeKeyID,
		active:        activeKP,
		providers:     providers,
	}, nil
}

func (m *MultiKeyProvider) KeyID() string {
	return m.active.KeyID()
}

func (m *MultiKeyProvider) EncryptWithAAD(plaintext, aad []byte) ([]byte, error) {
	return m.active.EncryptWithAAD(plaintext, aad)
}

func (m *MultiKeyProvider) DecryptWithAAD(ciphertext, aad []byte) ([]byte, error) {
	if bytes.HasPrefix(ciphertext, []byte(m.activeVersion+":"+m.activeKeyID+":")) {
		return m.active.DecryptWithAAD(ciphertext, aad)
	}
	for prefix, kp := range m.providers {
		if bytes.HasPrefix(ciphertext, []byte(prefix)) {
			return kp.DecryptWithAAD(ciphertext, aad)
		}
	}
	return nil, errors.New("versi kunci atau key_id tidak dikenali pada konfigurasi AES")
}
