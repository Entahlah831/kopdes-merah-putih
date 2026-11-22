package security

import (
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
	"strings"
)

const (
	PurposeDBColumns  = "db-columns"
	PurposeKYCFiles   = "kyc-files"
	PurposeBuktiFiles = "bukti-files"
	PurposeBackup     = "backup"
)

const kdfSalt = "kopdes-merah-putih-kdf-v1"

// DerivePurposeKey menurunkan sub-kunci AES-256 32-byte secara deterministik menggunakan HKDF-SHA256.
func DerivePurposeKey(masterKey []byte, purpose string) ([]byte, error) {
	if len(masterKey) != 32 {
		return nil, ErrInvalidKeySize
	}
	p := strings.TrimSpace(purpose)
	if p == "" {
		return nil, fmt.Errorf("purpose HKDF wajib diisi")
	}
	return hkdf.Key(sha256.New, masterKey, []byte(kdfSalt), p, 32)
}

// DerivePurpose menurunkan MultiKeyProvider baru terisolasi untuk domain kriptografi tertentu (termasuk rotasi AES_PREVIOUS_KEYS).
func (m *MultiKeyProvider) DerivePurpose(purpose string) (*MultiKeyProvider, error) {
	if m == nil || m.active == nil {
		return nil, fmt.Errorf("MultiKeyProvider belum diinisialisasi")
	}
	activeSubKey, err := DerivePurposeKey(m.active.key, purpose)
	if err != nil {
		return nil, err
	}
	activeKP, err := NewMasterKeyProvider(m.active.version, m.active.keyID, activeSubKey)
	if err != nil {
		return nil, err
	}
	providers := make(map[string]*MasterKeyProvider, len(m.providers))
	for prefix, origKP := range m.providers {
		subKey, err := DerivePurposeKey(origKP.key, purpose)
		if err != nil {
			return nil, err
		}
		derivedKP, err := NewMasterKeyProvider(origKP.version, origKP.keyID, subKey)
		if err != nil {
			return nil, err
		}
		providers[prefix] = derivedKP
	}
	return &MultiKeyProvider{
		activeVersion: m.activeVersion,
		activeKeyID:   m.activeKeyID,
		active:        activeKP,
		providers:     providers,
	}, nil
}

// PurposeKeySet menampung kunci master serta seluruh sub-kunci terisolasi per domain tujuan.
type PurposeKeySet struct {
	Master *MultiKeyProvider
	DB     *MultiKeyProvider
	KYC    *MultiKeyProvider
	Bukti  *MultiKeyProvider
	Backup *MultiKeyProvider
}

// NewPurposeKeySet membangun MultiKeyProvider master dan menurunkan sub-kunci DB, KYC, Bukti, dan Backup.
func NewPurposeKeySet(masterSpec, previousSpec string) (*PurposeKeySet, error) {
	ver, kid, mat := ParseMasterKeySpec(masterSpec)
	mkp, err := NewMultiKeyProviderFromConfig(ver, kid, mat, previousSpec)
	if err != nil {
		return nil, err
	}
	dbKP, err := mkp.DerivePurpose(PurposeDBColumns)
	if err != nil {
		return nil, err
	}
	kycKP, err := mkp.DerivePurpose(PurposeKYCFiles)
	if err != nil {
		return nil, err
	}
	buktiKP, err := mkp.DerivePurpose(PurposeBuktiFiles)
	if err != nil {
		return nil, err
	}
	backupKP, err := mkp.DerivePurpose(PurposeBackup)
	if err != nil {
		return nil, err
	}
	return &PurposeKeySet{
		Master: mkp,
		DB:     dbKP,
		KYC:    kycKP,
		Bukti:  buktiKP,
		Backup: backupKP,
	}, nil
}
