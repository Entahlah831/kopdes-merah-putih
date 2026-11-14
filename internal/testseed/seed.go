package testseed

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/qris"
	"github.com/kopdes/merah-putih/internal/security"
)

// Deterministic synthetic UUIDs for test fixtures across Koperasi A and Koperasi B.
var (
	WilayahDesaAID = uuid.MustParse("11111111-1111-4111-8111-000000000001")
	WilayahDesaBID = uuid.MustParse("22222222-2222-4222-8222-000000000001")

	KoperasiAID = uuid.MustParse("11111111-1111-4111-8111-000000000101")
	KoperasiBID = uuid.MustParse("22222222-2222-4222-8222-000000000201")

	PenyediaID       = uuid.MustParse("11111111-1111-4111-8111-000000000301")
	MetodeQRISAID    = uuid.MustParse("11111111-1111-4111-8111-000000000302")
	MetodeQRISBID    = uuid.MustParse("22222222-2222-4222-8222-000000000302")
	RekeningQRISAID  = uuid.MustParse("11111111-1111-4111-8111-000000000303")
	RekeningQRISBID  = uuid.MustParse("22222222-2222-4222-8222-000000000303")

	AnggotaAID      = uuid.MustParse("11111111-1111-4111-8111-000000000401")
	AnggotaBID      = uuid.MustParse("22222222-2222-4222-8222-000000000401")
	JenisIuranAID   = uuid.MustParse("11111111-1111-4111-8111-000000000501")
	JenisIuranBID   = uuid.MustParse("22222222-2222-4222-8222-000000000501")
	TagihanIuranAID = uuid.MustParse("11111111-1111-4111-8111-000000000502")
	TagihanIuranAID2 = uuid.MustParse("11111111-1111-4111-8111-000000000503")
	TagihanIuranBID = uuid.MustParse("22222222-2222-4222-8222-000000000502")

	UnitUsahaAID = uuid.MustParse("11111111-1111-4111-8111-000000000601")
	UnitUsahaBID = uuid.MustParse("22222222-2222-4222-8222-000000000601")
	KategoriAID  = uuid.MustParse("11111111-1111-4111-8111-000000000602")
	KategoriBID  = uuid.MustParse("22222222-2222-4222-8222-000000000602")
	GudangAID    = uuid.MustParse("11111111-1111-4111-8111-000000000603")
	GudangBID    = uuid.MustParse("22222222-2222-4222-8222-000000000603")
	ProdukAID    = uuid.MustParse("11111111-1111-4111-8111-000000000604")
	ProdukBID    = uuid.MustParse("22222222-2222-4222-8222-000000000604")
	PromoAID     = uuid.MustParse("11111111-1111-4111-8111-000000000605")

	PembayaranBID = uuid.MustParse("22222222-2222-4222-8222-000000000701")
)

const PromoAKode = "SEEDPROMOA"

// AccountSpec defines a non-secret seeded test account manifest entry.
type AccountSpec struct {
	Key            string    `json:"key"`
	UserID         uuid.UUID `json:"user_id"`
	KoperasiID     uuid.UUID `json:"koperasi_id"`
	TenantLabel    string    `json:"tenant"`
	Email          string    `json:"email"`
	NamaLengkap    string    `json:"nama_lengkap"`
	Role           string    `json:"role"`
	MFAEnabled     bool      `json:"mfa_enabled"`
	MFAMandatory   bool      `json:"mfa_mandatory"`
	HasAnggotaRow  bool      `json:"has_anggota_row"`
	AnggotaID      string    `json:"anggota_id,omitempty"`
	Description    string    `json:"description"`
}

// ResourceIDs holds non-secret deterministic resource IDs for test consoles and E2E verification.
type ResourceIDs struct {
	RekeningQRISAID      string `json:"rekening_qris_a_id"`
	MetodePembayaranAID  string `json:"metode_pembayaran_a_id"`
	JenisIuranAID        string `json:"jenis_iuran_a_id"`
	TagihanIuranAID      string `json:"tagihan_iuran_a_id"`
	TagihanIuranAID2     string `json:"tagihan_iuran_a2_id"`
	ProdukAID            string `json:"produk_a_id"`
	PromoAID             string `json:"promo_a_id"`
	PromoAKode           string `json:"promo_a_kode"`
	AnggotaAID           string `json:"anggota_a_id"`
	AnggotaBID           string `json:"anggota_b_id"`
	TagihanIuranBID      string `json:"tagihan_iuran_b_id"`
	PembayaranBID        string `json:"pembayaran_b_id"`
	RekeningQRISBID      string `json:"rekening_qris_b_id"`
}

// Accounts returns all deterministic test accounts across Koperasi A and Koperasi B.
func Accounts() []AccountSpec {
	return []AccountSpec{
		{
			Key:          "admin_a",
			UserID:       uuid.MustParse("11111111-1111-4111-8111-000000001001"),
			KoperasiID:   KoperasiAID,
			TenantLabel:  "A",
			Email:        "admin_a@kopdes-test.local",
			NamaLengkap:  "Admin Kopdes A (MFA Aktif)",
			Role:         "admin",
			MFAEnabled:   true,
			MFAMandatory: true,
			Description:  "Admin Koperasi A dengan MFA TOTP aktif",
		},
		{
			Key:          "admin_nomfa_a",
			UserID:       uuid.MustParse("11111111-1111-4111-8111-000000001002"),
			KoperasiID:   KoperasiAID,
			TenantLabel:  "A",
			Email:        "admin_nomfa_a@kopdes-test.local",
			NamaLengkap:  "Admin Kopdes A (Belum Enroll MFA)",
			Role:         "admin",
			MFAEnabled:   false,
			MFAMandatory: true,
			Description:  "Admin Koperasi A tanpa MFA untuk uji MFA_MANDATORY_SETUP_REQUIRED & enrollment",
		},
		{
			Key:          "pengurus_a",
			UserID:       uuid.MustParse("11111111-1111-4111-8111-000000001003"),
			KoperasiID:   KoperasiAID,
			TenantLabel:  "A",
			Email:        "pengurus_a@kopdes-test.local",
			NamaLengkap:  "Pengurus 1 Kopdes A",
			Role:         "pengurus",
			MFAEnabled:   false,
			MFAMandatory: false,
			Description:  "Pengurus utama Koperasi A (Maker/Checker 1)",
		},
		{
			Key:          "pengurus2_a",
			UserID:       uuid.MustParse("11111111-1111-4111-8111-000000001004"),
			KoperasiID:   KoperasiAID,
			TenantLabel:  "A",
			Email:        "pengurus2_a@kopdes-test.local",
			NamaLengkap:  "Pengurus 2 Kopdes A",
			Role:         "pengurus",
			MFAEnabled:   false,
			MFAMandatory: false,
			Description:  "Pengurus kedua Koperasi A untuk uji persetujuan ganda & Maker-Checker",
		},
		{
			Key:          "bendahara_a",
			UserID:       uuid.MustParse("11111111-1111-4111-8111-000000001005"),
			KoperasiID:   KoperasiAID,
			TenantLabel:  "A",
			Email:        "bendahara_a@kopdes-test.local",
			NamaLengkap:  "Bendahara Kopdes A (MFA Aktif)",
			Role:         "bendahara",
			MFAEnabled:   true,
			MFAMandatory: true,
			Description:  "Bendahara Koperasi A dengan MFA TOTP aktif",
		},
		{
			Key:          "ketua_a",
			UserID:       uuid.MustParse("11111111-1111-4111-8111-000000001006"),
			KoperasiID:   KoperasiAID,
			TenantLabel:  "A",
			Email:        "ketua_a@kopdes-test.local",
			NamaLengkap:  "Ketua Kopdes A (MFA Aktif)",
			Role:         "ketua",
			MFAEnabled:   true,
			MFAMandatory: true,
			Description:  "Ketua Koperasi A dengan MFA TOTP aktif",
		},
		{
			Key:          "pengawas_a",
			UserID:       uuid.MustParse("11111111-1111-4111-8111-000000001007"),
			KoperasiID:   KoperasiAID,
			TenantLabel:  "A",
			Email:        "pengawas_a@kopdes-test.local",
			NamaLengkap:  "Pengawas Kopdes A",
			Role:         "pengawas",
			MFAEnabled:   false,
			MFAMandatory: false,
			Description:  "Pengawas Koperasi A (izin baca audit & antrean)",
		},
		{
			Key:          "operator_a",
			UserID:       uuid.MustParse("11111111-1111-4111-8111-000000001008"),
			KoperasiID:   KoperasiAID,
			TenantLabel:  "A",
			Email:        "operator_a@kopdes-test.local",
			NamaLengkap:  "Operator Kopdes A",
			Role:         "operator",
			MFAEnabled:   false,
			MFAMandatory: false,
			Description:  "Operator Koperasi A (verifikasi toko / operasional terbatas)",
		},
		{
			Key:           "anggota_a",
			UserID:        uuid.MustParse("11111111-1111-4111-8111-000000001009"),
			KoperasiID:    KoperasiAID,
			TenantLabel:   "A",
			Email:         "anggota_a@kopdes-test.local",
			NamaLengkap:   "Anggota Kopdes A",
			Role:          "anggota",
			MFAEnabled:    false,
			MFAMandatory:  false,
			HasAnggotaRow: true,
			AnggotaID:     AnggotaAID.String(),
			Description:   "Anggota aktif Koperasi A (memiliki tagihan iuran & akses toko)",
		},
		{
			Key:          "admin_b",
			UserID:       uuid.MustParse("22222222-2222-4222-8222-000000002001"),
			KoperasiID:   KoperasiBID,
			TenantLabel:  "B",
			Email:        "admin_b@kopdes-test.local",
			NamaLengkap:  "Admin Kopdes B (MFA Aktif)",
			Role:         "admin",
			MFAEnabled:   true,
			MFAMandatory: true,
			Description:  "Admin tenant Koperasi B untuk pengujian isolasi lintas-koperasi",
		},
		{
			Key:           "anggota_b",
			UserID:        uuid.MustParse("22222222-2222-4222-8222-000000002002"),
			KoperasiID:    KoperasiBID,
			TenantLabel:   "B",
			Email:         "anggota_b@kopdes-test.local",
			NamaLengkap:   "Anggota Kopdes B",
			Role:          "anggota",
			MFAEnabled:    false,
			MFAMandatory:  false,
			HasAnggotaRow: true,
			AnggotaID:     AnggotaBID.String(),
			Description:   "Anggota aktif tenant Koperasi B beserta tagihan & pembayaran milik tenant B",
		},
	}
}

// Resources returns the deterministic synthetic resource IDs seeded into *_test databases.
func Resources() ResourceIDs {
	return ResourceIDs{
		RekeningQRISAID:     RekeningQRISAID.String(),
		MetodePembayaranAID: MetodeQRISAID.String(),
		JenisIuranAID:       JenisIuranAID.String(),
		TagihanIuranAID:     TagihanIuranAID.String(),
		TagihanIuranAID2:    TagihanIuranAID2.String(),
		ProdukAID:           ProdukAID.String(),
		PromoAID:            PromoAID.String(),
		PromoAKode:          PromoAKode,
		AnggotaAID:          AnggotaAID.String(),
		AnggotaBID:          AnggotaBID.String(),
		TagihanIuranBID:     TagihanIuranBID.String(),
		PembayaranBID:       PembayaranBID.String(),
		RekeningQRISBID:     RekeningQRISBID.String(),
	}
}

// ResolveSeedPassword determines the test account password from explicit flag/env without hardcoding literals.
func ResolveSeedPassword(cfg config.AppConfig, override string) (string, error) {
	if p := strings.TrimSpace(override); p != "" {
		return p, nil
	}
	if p := strings.TrimSpace(cfg.TestSeedPassword); p != "" {
		return p, nil
	}
	if strings.TrimSpace(cfg.JWTSecret) == "" {
		return "", fmt.Errorf("TEST_SEED_PASSWORD maupun JWT_SECRET tidak tersedia untuk menurunkan sandi akun uji")
	}
	mac := hmac.New(sha256.New, []byte(cfg.JWTSecret))
	mac.Write([]byte("kopdes-test-seed-password-v1"))
	sum := mac.Sum(nil)
	return hex.EncodeToString(sum[:10]) + "_Aa1!", nil
}

// DeterministicTOTPSecret derives a test-only RFC6238 Base32 secret from AESMasterKey + email.
func DeterministicTOTPSecret(cfg config.AppConfig, email string) string {
	mac := hmac.New(sha256.New, []byte(cfg.AESMasterKey))
	mac.Write([]byte("kopdes-test-totp-v1:" + strings.ToLower(strings.TrimSpace(email))))
	sum := mac.Sum(nil)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:20])
}

func buildTestQRISPayload(merchantName string) string {
	tag26Val := "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI"
	tag26 := fmt.Sprintf("26%02d%s", len(tag26Val), tag26Val)
	tag59 := fmt.Sprintf("59%02d%s", len(merchantName), merchantName)
	prefix := fmt.Sprintf("000201010211%s5204541153033605802ID%s6007Jakarta6304", tag26, tag59)
	return prefix + qris.CalculateCRC16(prefix)
}

// SeedAll idempotently populates synthetic Koperasi A/B, 11 role-matrix accounts, bills, QRIS, and toko items.
func SeedAll(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, overridePassword string) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "test") {
		return "", fmt.Errorf("seed_test_accounts ditolak: APP_ENV wajib 'test', ditemukan %q", cfg.AppEnv)
	}
	if !strings.HasSuffix(strings.TrimSpace(cfg.DBName), "_test") {
		return "", fmt.Errorf("seed_test_accounts ditolak: DB_NAME wajib berakhiran '_test', ditemukan %q", cfg.DBName)
	}

	seedPass, err := ResolveSeedPassword(cfg, overridePassword)
	if err != nil {
		return "", err
	}
	cost := cfg.BcryptCost
	if cost < 4 {
		cost = 4
	}
	pwdHash, err := security.HashPasswordWithCost(seedPass, cost)
	if err != nil {
		return "", fmt.Errorf("gagal hashing password akun uji: %w", err)
	}

	kp, err := security.NewMultiKeyProviderFromConfig(cfg.AESKeyVersion, cfg.AESKeyID, cfg.AESMasterKey, cfg.AESPreviousKeys)
	if err != nil {
		return "", fmt.Errorf("gagal inisialisasi KeyProvider: %w", err)
	}

	// Phase 1: Global master entities + users (app.current_koperasi_id MUST be empty per users UPDATE policy).
	txGlobal, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer txGlobal.Rollback(ctx)

	if _, err := txGlobal.Exec(ctx, "SELECT set_config('app.current_koperasi_id', '', true)"); err != nil {
		return "", err
	}

	rolesSeed := []struct {
		id   string
		kode string
		nama string
		desc string
	}{
		{"00000000-0000-0000-0000-000000000010", "admin", "admin", "Administrator Koperasi"},
		{"00000000-0000-0000-0000-000000000011", "pengurus", "pengurus", "Pengurus Koperasi"},
		{"00000000-0000-0000-0000-000000000012", "operator", "operator", "Operator Toko/Operasional"},
		{"00000000-0000-0000-0000-000000000013", "anggota", "anggota", "Anggota Koperasi"},
		{"00000000-0000-0000-0000-000000000014", "bendahara", "bendahara", "Bendahara Koperasi"},
		{"00000000-0000-0000-0000-000000000015", "ketua", "ketua", "Ketua Koperasi"},
		{"00000000-0000-0000-0000-000000000016", "pengawas", "pengawas", "Pengawas Koperasi"},
	}
	for _, r := range rolesSeed {
		if _, err := txGlobal.Exec(ctx, `
			INSERT INTO roles (id, kode, nama, deskripsi)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (kode) DO NOTHING;
		`, r.id, r.kode, r.nama, r.desc); err != nil {
			return "", fmt.Errorf("seed role %s: %w", r.kode, err)
		}
	}

	wilayahSeed := []struct {
		id   uuid.UUID
		kode string
		nama string
	}{
		{WilayahDesaAID, "SEED-WIL-A", "Desa Kopdes A"},
		{WilayahDesaBID, "SEED-WIL-B", "Desa Kopdes B"},
	}
	for _, w := range wilayahSeed {
		if _, err := txGlobal.Exec(ctx, `
			INSERT INTO wilayah (id, kode_kemendagri, kode_bps, nama, tingkat)
			VALUES ($1, $2, $2, $3, 'desa_kelurahan')
			ON CONFLICT (id) DO NOTHING;
		`, w.id, w.kode, w.nama); err != nil {
			return "", fmt.Errorf("seed wilayah %s: %w", w.kode, err)
		}
	}

	kopSeed := []struct {
		id      uuid.UUID
		kode    string
		nama    string
		wilID   uuid.UUID
		alamat  string
	}{
		{KoperasiAID, "SEED-KOP-A", "Koperasi Merah Putih Uji A", WilayahDesaAID, "Jl. Uji Tenant A No. 1"},
		{KoperasiBID, "SEED-KOP-B", "Koperasi Merah Putih Uji B", WilayahDesaBID, "Jl. Uji Tenant B No. 2"},
	}
	for _, k := range kopSeed {
		if _, err := txGlobal.Exec(ctx, `
			INSERT INTO koperasi (id, kode_koperasi, nama, jenis_kopdes, wilayah_desa_id, alamat)
			VALUES ($1, $2, $3, 'desa', $4, $5)
			ON CONFLICT (id) DO UPDATE SET nama = EXCLUDED.nama, alamat = EXCLUDED.alamat, deleted_at = NULL;
		`, k.id, k.kode, k.nama, k.wilID, k.alamat); err != nil {
			return "", fmt.Errorf("seed koperasi %s: %w", k.kode, err)
		}
	}

	if _, err := txGlobal.Exec(ctx, `
		INSERT INTO penyedia_pembayaran (id, kode, nama, tipe_integrasi)
		VALUES ($1, 'SEED-ASPI-QRIS', 'PJP QRIS Uji', 'qris_statis_manual')
		ON CONFLICT (id) DO NOTHING;
	`, PenyediaID); err != nil {
		return "", fmt.Errorf("seed penyedia_pembayaran: %w", err)
	}

	accounts := Accounts()
	for i, acc := range accounts {
		emailHash := security.HashBlindIndex(acc.Email, []byte(cfg.BlindIndexSecret))
		phone := fmt.Sprintf("08120000%04d", 100+i)
		phoneHash := security.HashBlindIndex(phone, []byte(cfg.BlindIndexSecret))
		encEmail, err := kp.EncryptWithAAD([]byte(acc.Email), security.FormatAAD("users", "email_enkripsi", acc.UserID.String()))
		if err != nil {
			return "", fmt.Errorf("encrypt email %s: %w", acc.Email, err)
		}
		encPhone, err := kp.EncryptWithAAD([]byte(phone), security.FormatAAD("users", "no_hp_enkripsi", acc.UserID.String()))
		if err != nil {
			return "", fmt.Errorf("encrypt phone %s: %w", acc.Email, err)
		}

		if _, err := txGlobal.Exec(ctx, `
			INSERT INTO users (id, email_enkripsi, email_hash, no_hp_enkripsi, no_hp_hash, password_hash, nama_lengkap, status_aktif)
			VALUES ($1, $2, $3, $4, $5, $6, $7, true)
			ON CONFLICT (id) DO UPDATE SET
				email_enkripsi = EXCLUDED.email_enkripsi,
				email_hash     = EXCLUDED.email_hash,
				no_hp_enkripsi = EXCLUDED.no_hp_enkripsi,
				no_hp_hash     = EXCLUDED.no_hp_hash,
				password_hash  = EXCLUDED.password_hash,
				nama_lengkap   = EXCLUDED.nama_lengkap,
				status_aktif   = true,
				deleted_at     = NULL;
		`, acc.UserID, encEmail, emailHash, encPhone, phoneHash, pwdHash, acc.NamaLengkap); err != nil {
			return "", fmt.Errorf("seed user %s: %w", acc.Email, err)
		}

		if acc.MFAEnabled {
			totpSecret := DeterministicTOTPSecret(cfg, acc.Email)
			encSecret, err := kp.EncryptWithAAD([]byte(totpSecret), []byte(fmt.Sprintf("users:totp:%s", acc.UserID)))
			if err != nil {
				return "", fmt.Errorf("encrypt totp %s: %w", acc.Email, err)
			}
			recoveryCodes := []string{
				fmt.Sprintf("SEED-%s-REC-1", strings.ToUpper(acc.Key)),
				fmt.Sprintf("SEED-%s-REC-2", strings.ToUpper(acc.Key)),
			}
			recHashes := make([]string, len(recoveryCodes))
			for j, rc := range recoveryCodes {
				recHashes[j] = security.HashRecoveryCode(rc)
			}
			recJSON, _ := json.Marshal(recHashes)
			encRecovery, err := kp.EncryptWithAAD(recJSON, []byte(fmt.Sprintf("users:recovery:%s", acc.UserID)))
			if err != nil {
				return "", fmt.Errorf("encrypt recovery %s: %w", acc.Email, err)
			}
			if _, err := txGlobal.Exec(ctx, "SELECT fn_set_user_mfa($1, true, $2, 0, $3);", acc.UserID, encSecret, encRecovery); err != nil {
				return "", fmt.Errorf("fn_set_user_mfa enable %s: %w", acc.Email, err)
			}
		} else {
			if _, err := txGlobal.Exec(ctx, "SELECT fn_set_user_mfa($1, false, NULL, 0, NULL);", acc.UserID); err != nil {
				return "", fmt.Errorf("fn_set_user_mfa reset %s: %w", acc.Email, err)
			}
		}
		_, _ = txGlobal.Exec(ctx, "DELETE FROM mfa_login_sessions WHERE user_id = $1;", acc.UserID)
	}

	if err := txGlobal.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit global seed: %w", err)
	}

	// Phase 2: Tenant A fixtures
	if err := seedTenantFixtures(ctx, pool, cfg, kp, tenantSeedSpec{
		koperasiID:     KoperasiAID,
		tenantTag:      "A",
		creatorUserID:  uuid.MustParse("11111111-1111-4111-8111-000000001001"),
		anggotaUserID:  uuid.MustParse("11111111-1111-4111-8111-000000001009"),
		anggotaID:      AnggotaAID,
		metodeID:       MetodeQRISAID,
		rekeningID:     RekeningQRISAID,
		jenisIuranID:   JenisIuranAID,
		tagihanIDs:     []uuid.UUID{TagihanIuranAID, TagihanIuranAID2},
		unitUsahaID:    UnitUsahaAID,
		kategoriID:     KategoriAID,
		gudangID:       GudangAID,
		produkID:       ProdukAID,
		promoID:        PromoAID,
		withSamplePay:  false,
	}, accounts); err != nil {
		return "", err
	}

	// Phase 3: Tenant B fixtures
	if err := seedTenantFixtures(ctx, pool, cfg, kp, tenantSeedSpec{
		koperasiID:     KoperasiBID,
		tenantTag:      "B",
		creatorUserID:  uuid.MustParse("22222222-2222-4222-8222-000000002001"),
		anggotaUserID:  uuid.MustParse("22222222-2222-4222-8222-000000002002"),
		anggotaID:      AnggotaBID,
		metodeID:       MetodeQRISBID,
		rekeningID:     RekeningQRISBID,
		jenisIuranID:   JenisIuranBID,
		tagihanIDs:     []uuid.UUID{TagihanIuranBID},
		unitUsahaID:    UnitUsahaBID,
		kategoriID:     KategoriBID,
		gudangID:       GudangBID,
		produkID:       ProdukBID,
		withSamplePay:  true,
		samplePayID:    PembayaranBID,
	}, accounts); err != nil {
		return "", err
	}

	return seedPass, nil
}

type tenantSeedSpec struct {
	koperasiID    uuid.UUID
	tenantTag     string
	creatorUserID uuid.UUID
	anggotaUserID uuid.UUID
	anggotaID     uuid.UUID
	metodeID      uuid.UUID
	rekeningID    uuid.UUID
	jenisIuranID  uuid.UUID
	tagihanIDs    []uuid.UUID
	unitUsahaID   uuid.UUID
	kategoriID    uuid.UUID
	gudangID      uuid.UUID
	produkID      uuid.UUID
	promoID       uuid.UUID
	withSamplePay bool
	samplePayID   uuid.UUID
}

func seedTenantFixtures(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, kp security.KeyProvider, spec tenantSeedSpec, accounts []AccountSpec) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", spec.koperasiID.String()); err != nil {
		return err
	}

	for _, acc := range accounts {
		if acc.KoperasiID != spec.koperasiID {
			continue
		}
		var roleID uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT id FROM roles WHERE kode = $1 LIMIT 1;", acc.Role).Scan(&roleID); err != nil {
			return fmt.Errorf("lookup role %s: %w", acc.Role, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_roles (koperasi_id, user_id, role_id, status_aktif)
			VALUES ($1, $2, $3, true)
			ON CONFLICT (koperasi_id, user_id, role_id) DO UPDATE SET status_aktif = true, deleted_at = NULL;
		`, spec.koperasiID, acc.UserID, roleID); err != nil {
			return fmt.Errorf("seed user_role %s: %w", acc.Email, err)
		}
	}

	// Anggota sample row
	nik := fmt.Sprintf("320100000000%s001", map[string]string{"A": "1", "B": "2"}[spec.tenantTag])
	encNIK, err := kp.EncryptWithAAD([]byte(nik), security.FormatAAD("anggota", "nik_enkripsi", spec.anggotaID.String()))
	if err != nil {
		return err
	}
	encTgl, err := kp.EncryptWithAAD([]byte("1995-05-15"), security.FormatAAD("anggota", "tanggal_lahir_enkripsi", spec.anggotaID.String()))
	if err != nil {
		return err
	}
	nikHash, err := security.HashTenantBlindIndex(nik, []byte(cfg.BlindIndexSecret), spec.koperasiID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO anggota (id, koperasi_id, user_id, no_anggota, nik_enkripsi, nik_hash, nama_lengkap, tempat_lahir, tanggal_lahir_enkripsi, jenis_kelamin, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'Bogor', $8, 'L', 'aktif', $9)
		ON CONFLICT (id) DO UPDATE SET status = 'aktif', deleted_at = NULL;
	`, spec.anggotaID, spec.koperasiID, spec.anggotaUserID, "SEED-ANG-"+spec.tenantTag, encNIK, nikHash, "Anggota Kopdes "+spec.tenantTag, encTgl, spec.creatorUserID); err != nil {
		return fmt.Errorf("seed anggota %s: %w", spec.tenantTag, err)
	}

	// Metode pembayaran & Rekening QRIS (badan_hukum_koperasi agar bekerja walau pilot false)
	if _, err := tx.Exec(ctx, `
		INSERT INTO metode_pembayaran (id, koperasi_id, penyedia_id, kode, nama, butuh_kode_unik, butuh_verifikasi_manual)
		VALUES ($1, $2, $3, $4, $5, true, true)
		ON CONFLICT (id) DO NOTHING;
	`, spec.metodeID, spec.koperasiID, PenyediaID, "SEED-QRIS-"+spec.tenantTag, "QRIS Kopdes "+spec.tenantTag); err != nil {
		return fmt.Errorf("seed metode %s: %w", spec.tenantTag, err)
	}

	qrisPayload := buildTestQRISPayload("Kopdes Test " + spec.tenantTag)
	if _, err := tx.Exec(ctx, `
		INSERT INTO rekening_qris_koperasi (
			id, koperasi_id, penyedia_id, nama_merchant, nmid,
			nama_bank_pjp, no_rekening_settlement, nama_pemilik_rekening,
			jenis_pemilik, is_atas_nama_badan_hukum, qris_payload_statis, status_aktif, created_by
		) VALUES (
			$1, $2, $3, $4, $5,
			'Bank Mandiri', $6, $7,
			'badan_hukum_koperasi', true, $8, true, $9
		)
		ON CONFLICT (id) DO UPDATE SET status_aktif = true, deleted_at = NULL;
	`, spec.rekeningID, spec.koperasiID, PenyediaID, "Merchant Kopdes "+spec.tenantTag, "SEEDID"+spec.tenantTag+"00001", "111222333"+spec.tenantTag, "Koperasi Merah Putih "+spec.tenantTag, qrisPayload, spec.creatorUserID); err != nil {
		return fmt.Errorf("seed rekening_qris %s: %w", spec.tenantTag, err)
	}

	// Jenis iuran & Tagihan iuran
	if _, err := tx.Exec(ctx, `
		INSERT INTO jenis_iuran (id, koperasi_id, kode, nama, nominal, hari_jatuh_tempo, berlaku_mulai, status_aktif, created_by)
		VALUES ($1, $2, $3, $4, 50000, 10, '2026-01-01', true, $5)
		ON CONFLICT (id) DO UPDATE SET status_aktif = true, deleted_at = NULL;
	`, spec.jenisIuranID, spec.koperasiID, "SEED-JI-"+spec.tenantTag, "Iuran Wajib Kopdes "+spec.tenantTag, spec.creatorUserID); err != nil {
		return fmt.Errorf("seed jenis_iuran %s: %w", spec.tenantTag, err)
	}

	for idx, tid := range spec.tagihanIDs {
		periode := fmt.Sprintf("2026-%02d", idx+1)
		if _, err := tx.Exec(ctx, `
			INSERT INTO tagihan_iuran (id, koperasi_id, anggota_id, jenis_iuran_id, periode, jatuh_tempo, nominal_pokok, status, created_by)
			VALUES ($1, $2, $3, $4, $5, CURRENT_DATE + 30, 50000, 'belum_bayar', $6)
			ON CONFLICT (id) DO UPDATE SET status = 'belum_bayar', deleted_at = NULL;
		`, tid, spec.koperasiID, spec.anggotaID, spec.jenisIuranID, periode, spec.creatorUserID); err != nil {
			return fmt.Errorf("seed tagihan %s: %w", tid, err)
		}
	}

	// Toko / katalog fixtures
	if _, err := tx.Exec(ctx, `
		INSERT INTO unit_usaha (id, koperasi_id, kode_unit, nama, kode_kbli, jenis_unit, created_by)
		VALUES ($1, $2, $3, 'Unit Toko Uji', '47111', 'gerai_sembako', $4)
		ON CONFLICT (id) DO NOTHING;
	`, spec.unitUsahaID, spec.koperasiID, "SEED-UNIT-"+spec.tenantTag, spec.creatorUserID); err != nil {
		return fmt.Errorf("seed unit_usaha %s: %w", spec.tenantTag, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO kategori_produk (id, koperasi_id, kode, nama, slug)
		VALUES ($1, $2, $3, 'Sembako Uji', $4)
		ON CONFLICT (id) DO NOTHING;
	`, spec.kategoriID, spec.koperasiID, "SEED-KAT-"+spec.tenantTag, "sembako-uji-"+strings.ToLower(spec.tenantTag)); err != nil {
		return fmt.Errorf("seed kategori %s: %w", spec.tenantTag, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO produk (id, koperasi_id, unit_usaha_id, kategori_id, sku, nama, slug, deskripsi, satuan, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'Beras Premium Uji 5kg', 'sak', 'aktif', $8)
		ON CONFLICT (id) DO UPDATE SET status = 'aktif', deleted_at = NULL;
	`, spec.produkID, spec.koperasiID, spec.unitUsahaID, spec.kategoriID, "SEED-BRS-"+spec.tenantTag, "Beras Kopdes "+spec.tenantTag, "beras-kopdes-"+strings.ToLower(spec.tenantTag), spec.creatorUserID); err != nil {
		return fmt.Errorf("seed produk %s: %w", spec.tenantTag, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO produk_harga (id, koperasi_id, produk_id, versi, harga_umum, harga_anggota, berlaku_mulai, status_aktif, created_by)
		VALUES ($1, $2, $3, 1, 75000, 65000, NOW(), true, $4)
		ON CONFLICT (id) DO UPDATE SET status_aktif = true, harga_umum = 75000, harga_anggota = 65000, deleted_at = NULL;
	`, uuid.NewSHA1(uuid.NameSpaceOID, []byte("harga:"+spec.produkID.String())), spec.koperasiID, spec.produkID, spec.creatorUserID); err != nil {
		return fmt.Errorf("seed produk_harga %s: %w", spec.tenantTag, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO gudang (id, koperasi_id, unit_usaha_id, kode, nama, status_aktif)
		VALUES ($1, $2, $3, $4, 'Gudang Uji Utama', true)
		ON CONFLICT (id) DO NOTHING;
	`, spec.gudangID, spec.koperasiID, spec.unitUsahaID, "SEED-GDG-"+spec.tenantTag); err != nil {
		return fmt.Errorf("seed gudang %s: %w", spec.tenantTag, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO stok_saldo (koperasi_id, gudang_id, produk_id, qty_tersedia, qty_dipesan)
		VALUES ($1, $2, $3, 500, 0)
		ON CONFLICT (koperasi_id, gudang_id, produk_id, varian_id) DO UPDATE SET qty_tersedia = GREATEST(stok_saldo.qty_tersedia, 500);
	`, spec.koperasiID, spec.gudangID, spec.produkID); err != nil {
		return fmt.Errorf("seed stok_saldo %s: %w", spec.tenantTag, err)
	}

	if spec.promoID != uuid.Nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO promo (
				id, koperasi_id, kode, nama, tipe_promo, nilai_diskon, cakupan, target_anggota,
				berlaku_mulai, berlaku_sampai, minimal_belanja, kuota_total, status_aktif, created_by
			) VALUES (
				$1, $2, $3, 'Promo Uji Anggota', 'nominal'::tipe_promo_enum, 5000, 'seluruh_katalog', 'semua_anggota_aktif',
				NOW() - interval '1 hour', NOW() + interval '30 days', 10000, 1000, true, $4
			)
			ON CONFLICT (id) DO UPDATE SET status_aktif = true, berlaku_sampai = NOW() + interval '30 days', deleted_at = NULL;
		`, spec.promoID, spec.koperasiID, PromoAKode, spec.creatorUserID); err != nil {
			return fmt.Errorf("seed promo %s: %w", spec.tenantTag, err)
		}
	}

	if spec.withSamplePay && spec.samplePayID != uuid.Nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO pembayaran (
				id, koperasi_id, no_pembayaran, pembayar_user_id, anggota_id,
				metode_pembayaran_id, rekening_qris_id, jenis_transaksi,
				nominal_dasar, kode_unik, kode_unik_awal, jumlah_percobaan,
				berlaku_sampai, status, created_by
			) VALUES (
				$1, $2, 'SEED-PAY-B-001', $3, $4,
				$5, $6, 'iuran_bulanan',
				50000, 111, 111, 1,
				NOW() + interval '24 hours', 'menunggu_pembayaran', $3
			)
			ON CONFLICT (id) DO NOTHING;
		`, spec.samplePayID, spec.koperasiID, spec.anggotaUserID, spec.anggotaID, spec.metodeID, spec.rekeningID); err != nil {
			return fmt.Errorf("seed sample pembayaran B: %w", err)
		}
	}

	_ = time.Now() // ensure time import cleanly retained if extended
	return tx.Commit(ctx)
}
