package testseed

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/security"
)

type AccountSpec struct {
	Key         string `json:"key"`
	Email       string `json:"email"`
	NoHP        string `json:"no_hp"`
	Role        string `json:"role"`
	MFAEnabled  bool   `json:"mfa_enabled"`
	TOTPSecret  string `json:"totp_secret,omitempty"`
	Description string `json:"description"`
}

type ResourceIDs struct {
	JenisIuranID  string `json:"jenis_iuran_id"`
	ProdukID      string `json:"produk_id"`
	RekeningQRIS  string `json:"rekening_qris_id"`
}

func Accounts() []AccountSpec {
	return []AccountSpec{
		{Key: "admin", Email: "admin@kopdes-test.local", NoHP: "08110000001", Role: "admin", MFAEnabled: true, Description: "Admin koperasi"},
		{Key: "bendahara", Email: "bendahara@kopdes-test.local", NoHP: "08110000002", Role: "bendahara", MFAEnabled: true, Description: "Bendahara koperasi"},
		{Key: "petugas_toko", Email: "petugas@kopdes-test.local", NoHP: "08110000003", Role: "petugas_toko", MFAEnabled: false, Description: "Petugas toko"},
		{Key: "anggota_1", Email: "anggota1@kopdes-test.local", NoHP: "08110000004", Role: "anggota", MFAEnabled: false, Description: "Anggota aktif 1"},
		{Key: "anggota_2", Email: "anggota2@kopdes-test.local", NoHP: "08110000005", Role: "anggota", MFAEnabled: false, Description: "Anggota aktif 2"},
	}
}

func Resources() ResourceIDs {
	return ResourceIDs{}
}

func ResolveSeedPassword(_ config.AppConfig, override string) (string, error) {
	if p := strings.TrimSpace(override); p != "" {
		return p, nil
	}
	return "kopdes_test_setA_user_pass_123!", nil
}

func DeterministicTOTPSecret(cfg config.AppConfig, email string) string {
	h := security.HashBlindIndex(strings.ToLower(strings.TrimSpace(email)), []byte(cfg.AESMasterKey))
	return strings.ToUpper(h[:32])
}

func SeedAll(_ context.Context, _ *pgxpool.Pool, cfg config.AppConfig, override string) (string, error) {
	return ResolveSeedPassword(cfg, override)
}
