package testseed

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
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
	JenisIuranID string `json:"jenis_iuran_id"`
	TagihanID    string `json:"tagihan_id,omitempty"`
	KategoriID   string `json:"kategori_id,omitempty"`
	ProdukID     string `json:"produk_id"`
	RekeningQRIS string `json:"rekening_qris_id"`
	PromoKode    string `json:"promo_kode,omitempty"`
	AnggotaID    string `json:"anggota_id,omitempty"`
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

func AccountsWithSecrets(cfg config.AppConfig) []AccountSpec {
	list := Accounts()
	for i := range list {
		if list[i].MFAEnabled {
			list[i].TOTPSecret = DeterministicTOTPSecret(cfg, list[i].Email)
		}
	}
	return list
}

func Resources() ResourceIDs {
	return ResourceIDs{}
}

func LookupResources(ctx context.Context, pool *pgxpool.Pool) ResourceIDs {
	var out ResourceIDs
	if pool == nil {
		return out
	}
	q := db.New(pool)
	if reks, err := q.ListRekeningQRIS(ctx); err == nil && len(reks) > 0 {
		out.RekeningQRIS = response.FromPGUUID(reks[0].ID).String()
	}
	if jen, err := q.ListJenisIuranAktif(ctx); err == nil && len(jen) > 0 {
		out.JenisIuranID = response.FromPGUUID(jen[0].ID).String()
	}
	if kat, err := q.ListKategoriAktif(ctx); err == nil && len(kat) > 0 {
		out.KategoriID = response.FromPGUUID(kat[0].ID).String()
	}
	if prods, err := q.ListProduk(ctx, db.ListProdukParams{Lim: 1, Off: 0}); err == nil && len(prods) > 0 {
		out.ProdukID = response.FromPGUUID(prods[0].ID).String()
	}
	if promos, err := q.ListPromoAktif(ctx); err == nil && len(promos) > 0 {
		out.PromoKode = promos[0].Kode
	}
	if ags, err := q.ListAnggota(ctx, db.ListAnggotaParams{StatusFilter: "aktif", Lim: 1, Off: 0}); err == nil && len(ags) > 0 {
		out.AnggotaID = response.FromPGUUID(ags[0].ID).String()
	}
	if tgs, err := q.ListTagihanIuran(ctx, db.ListTagihanIuranParams{StatusFilter: "belum_bayar", Lim: 1, Off: 0}); err == nil && len(tgs) > 0 {
		out.TagihanID = response.FromPGUUID(tgs[0].ID).String()
	}
	return out
}

func ResolveSeedPassword(_ config.AppConfig, override string) (string, error) {
	if p := strings.TrimSpace(override); p != "" {
		return p, nil
	}
	return "kopdes_test_setA_user_pass_123!", nil
}

func DeterministicTOTPSecret(cfg config.AppConfig, email string) string {
	mac := hmac.New(sha256.New, []byte(cfg.AESMasterKey))
	_, _ = mac.Write([]byte(strings.ToLower(strings.TrimSpace(email))))
	sum := mac.Sum(nil)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:20])
}

func SeedAll(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, override string) (string, error) {
	pwd, err := ResolveSeedPassword(cfg, override)
	if err != nil {
		return "", err
	}
	ver, kid, mat := security.ParseMasterKeySpec(cfg.AESMasterKey)
	kp, err := security.NewMultiKeyProviderFromConfig(ver, kid, mat, cfg.AESPreviousKeys)
	if err != nil {
		return "", fmt.Errorf("inisialisasi KeyProvider seed: %w", err)
	}
	blindKey := security.DecodeSecretMaterial(cfg.BlindIndexSecret)
	passHash, err := security.HashPasswordWithCost(pwd, 10)
	if err != nil {
		return "", fmt.Errorf("hash sandi seed: %w", err)
	}

	q := db.New(pool)
	rekID, err := seedQRISAndPengaturan(ctx, q, cfg)
	if err != nil {
		return "", err
	}
	_ = rekID
	if err := seedAccountsAndMembers(ctx, q, cfg, kp, blindKey, passHash); err != nil {
		return "", err
	}
	if err := seedIuranAndCatalog(ctx, q); err != nil {
		return "", err
	}
	return pwd, nil
}

func seedQRISAndPengaturan(ctx context.Context, q *db.Queries, cfg config.AppConfig) (pgtype.UUID, error) {
	reks, err := q.ListRekeningQRIS(ctx)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("cek rekening QRIS: %w", err)
	}
	var rekID pgtype.UUID
	if len(reks) > 0 {
		rekID = reks[0].ID
	} else {
		row, err := q.CreateRekeningQRIS(ctx, db.CreateRekeningQRISParams{
			NamaMerchant:  "Koperasi Desa Merah Putih Uji",
			Nmid:          "ID1020260000001",
			NamaBank:      "Bank BRI Uji",
			NomorRekening: "000101000001501",
			NamaPemilik:   "Koperasi Desa Merah Putih",
			JenisPemilik:  "badan_hukum",
			QrisImageUrl:  "/api/v1/public/config",
			QrisPayload:   "00020101021126570011ID.CO.BRI.WWW01189360000200000000010215ID10202600000015204549953033605802ID5924KOPDES MERAH PUTIH TEST6007JAKARTA6304ABCD",
		})
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("buat rekening QRIS seed: %w", err)
		}
		rekID = row.ID
	}
	digits := int32(cfg.QRISUniqueDigits) // #nosec G115 -- validasi 3 atau 4
	if digits != 4 {
		digits = 3
	}
	_, err = q.UpdatePengaturan(ctx, db.UpdatePengaturanParams{
		NamaKoperasi:        "Kopdes Merah Putih (Lingkungan Uji)",
		AlamatKoperasi:      "Jl. Balai Desa No. 1",
		TeleponKoperasi:     "021-5550100",
		QrisUniqueDigits:    digits,
		OngkirTetap:         5000,
		KebijakanTeks:       "Lingkup koperasi tunggal; simpan bukti pembayaran untuk verifikasi bendahara.",
		ActiveRekeningQrisID: rekID,
	})
	return rekID, err
}

func seedAccountsAndMembers(ctx context.Context, q *db.Queries, cfg config.AppConfig, kp security.KeyProvider, blindKey []byte, passHash string) error {
	for idx, spec := range Accounts() {
		uid, err := ensureSeedUser(ctx, q, cfg, kp, blindKey, passHash, spec)
		if err != nil {
			return err
		}
		if spec.Role == "anggota" {
			if err := ensureSeedAnggota(ctx, q, kp, blindKey, uid, spec, idx+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureSeedUser(ctx context.Context, q *db.Queries, cfg config.AppConfig, kp security.KeyProvider, blindKey []byte, passHash string, spec AccountSpec) (uuid.UUID, error) {
	emailHash := security.HashBlindIndex(spec.Email, blindKey)
	existing, err := q.GetUserByEmailHash(ctx, response.ToPGText(emailHash))
	var userID uuid.UUID
	if errors.Is(err, pgx.ErrNoRows) {
		userID = uuid.New()
		emailEnc, _ := kp.EncryptWithAAD([]byte(spec.Email), security.FormatAAD("users", "email", userID.String()))
		hpNorm := security.NormalizePhone(spec.NoHP)
		hpEnc, _ := kp.EncryptWithAAD([]byte(hpNorm), security.FormatAAD("users", "hp", userID.String()))
		hpHash := security.HashBlindIndex(hpNorm, blindKey)
		created, cErr := q.CreateUser(ctx, db.CreateUserParams{
			ID:           response.ToPGUUID(userID),
			EmailEnc:     emailEnc,
			EmailHash:    response.ToPGText(emailHash),
			HpEnc:        hpEnc,
			HpHash:       response.ToPGText(hpHash),
			NamaLengkap:  spec.Description,
			PasswordHash: passHash,
			Peran:        spec.Role,
			StatusAktif:  true,
		})
		if cErr != nil {
			return uuid.Nil, fmt.Errorf("buat user seed %s: %w", spec.Key, cErr)
		}
		userID = response.FromPGUUID(created.ID)
	} else if err != nil {
		return uuid.Nil, err
	} else {
		userID = response.FromPGUUID(existing.ID)
	}
	if spec.MFAEnabled {
		secret := DeterministicTOTPSecret(cfg, spec.Email)
		mfaEnc, _ := kp.EncryptWithAAD([]byte(secret), security.FormatAAD("users", "mfa_secret", userID.String()))
		recHash := security.HashBlindIndex(strings.ToUpper("RCV-"+spec.Key+"-0001"), blindKey)
		if err := q.EnableUserMFA(ctx, db.EnableUserMFAParams{
			MfaSecretEnc:      mfaEnc,
			MfaRecoveryHashes: []string{recHash},
			MfaLastStep:       0,
			ID:                response.ToPGUUID(userID),
		}); err != nil {
			return uuid.Nil, fmt.Errorf("aktifkan MFA seed %s: %w", spec.Key, err)
		}
	}
	return userID, nil
}

func ensureSeedAnggota(ctx context.Context, q *db.Queries, kp security.KeyProvider, blindKey []byte, userID uuid.UUID, spec AccountSpec, seq int) error {
	if _, err := q.GetAnggotaByUserID(ctx, response.ToPGUUID(userID)); err == nil {
		return nil
	}
	aid := uuid.New()
	nik := fmt.Sprintf("320101010101%04d", seq)
	hpNorm := security.NormalizePhone(spec.NoHP)
	nikEnc, _ := kp.EncryptWithAAD([]byte(nik), security.FormatAAD("anggota", "nik", aid.String()))
	hpEnc, _ := kp.EncryptWithAAD([]byte(hpNorm), security.FormatAAD("anggota", "hp", aid.String()))
	tglEnc, _ := kp.EncryptWithAAD([]byte("1995-05-15"), security.FormatAAD("anggota", "tanggal_lahir", aid.String()))
	alamatEnc, _ := kp.EncryptWithAAD([]byte(fmt.Sprintf("Dusun Uji RT 0%d Desa Merah Putih", seq)), security.FormatAAD("anggota", "alamat", aid.String()))
	_, err := q.CreateAnggota(ctx, db.CreateAnggotaParams{
		ID:              response.ToPGUUID(aid),
		UserID:          response.ToPGUUID(userID),
		NomorAnggota:    fmt.Sprintf("KMP-2026-%04d", seq),
		NamaLengkap:     spec.Description,
		NikEnc:          nikEnc,
		NikHash:         security.HashBlindIndex(nik, blindKey),
		NikKeyVersion:   kp.KeyID(),
		NikMasked:       security.MaskNIK(nik),
		HpEnc:           hpEnc,
		HpMasked:        security.MaskPhone(hpNorm),
		TanggalLahirEnc: tglEnc,
		AlamatEnc:       alamatEnc,
		Status:          "aktif",
	})
	return err
}

func seedIuranAndCatalog(ctx context.Context, q *db.Queries) error {
	jen, err := q.ListJenisIuranAktif(ctx)
	if err != nil {
		return err
	}
	var jenisRow db.JenisIuran
	if len(jen) == 0 {
		jenisRow, err = q.CreateJenisIuran(ctx, db.CreateJenisIuranParams{
			Kode:                 "IURAN_WAJIB",
			Nama:                 "Iuran Wajib Bulanan",
			Nominal:              25000,
			HariJatuhTempo:       10,
			KlasifikasiAkuntansi: "kewajiban_atau_modal_iuran_keputusan_akuntan",
		})
		if err != nil {
			return fmt.Errorf("seed jenis iuran: %w", err)
		}
	} else {
		jenisRow = jen[0]
	}
	now := time.Now().UTC()
	due := time.Date(now.Year(), now.Month(), int(jenisRow.HariJatuhTempo), 0, 0, 0, 0, time.UTC)
	if agIDs, err := q.ListAktifAnggotaIDs(ctx); err == nil {
		for _, aid := range agIDs {
			_, _ = q.InsertTagihanIuranIfMissing(ctx, db.InsertTagihanIuranIfMissingParams{
				AnggotaID: aid, JenisIuranID: jenisRow.ID, PeriodeTahun: int32(now.Year()), // #nosec G115 -- tahun kalender wajar
				PeriodeBulan: int32(now.Month()), Nominal: jenisRow.Nominal, TanggalJatuhTempo: response.ToPGDate(due), // #nosec G115 -- bulan 1..12
			})
		}
	}
	return seedCatalogItems(ctx, q)
}

func seedCatalogItems(ctx context.Context, q *db.Queries) error {
	katList, err := q.ListKategoriAktif(ctx)
	if err != nil {
		return err
	}
	var katID pgtype.UUID
	if len(katList) == 0 {
		k, err := q.CreateKategori(ctx, db.CreateKategoriParams{Slug: "sembako", Nama: "Sembako Desa"})
		if err != nil {
			return err
		}
		katID = k.ID
	} else {
		katID = katList[0].ID
	}
	prods, err := q.ListProduk(ctx, db.ListProdukParams{Lim: 10, Off: 0})
	if err != nil {
		return err
	}
	if len(prods) == 0 {
		p1 := int64(70000)
		p1Mem := int64(65000)
		if _, err := q.CreateProduk(ctx, db.CreateProdukParams{
			KategoriID: katID, Sku: "BERAS-5KG", Nama: "Beras Premium 5 Kg",
			Deskripsi: "Beras hasil panen petani lokal", Satuan: "sak",
			HargaUmum: response.ToPGInt8(&p1), HargaAnggota: response.ToPGInt8(&p1Mem),
			Stok: 50, Status: "aktif",
		}); err != nil {
			return err
		}
		p2 := int64(18000)
		if _, err := q.CreateProduk(ctx, db.CreateProdukParams{
			KategoriID: katID, Sku: "GULA-UMUM-1KG", Nama: "Gula Kemasan Khusus Umum 1 Kg",
			Deskripsi: "Produk demo tanpa harga_anggota untuk uji 409 MISSING_ACTIVE_PRICE", Satuan: "pack",
			HargaUmum: response.ToPGInt8(&p2), Stok: 25, Status: "aktif",
		}); err != nil {
			return err
		}
	}
	promos, err := q.ListPromoAktif(ctx)
	if err == nil && len(promos) == 0 {
		now := time.Now().UTC()
		_, err = q.CreatePromo(ctx, db.CreatePromoParams{
			Kode: "DESA10", Nama: "Diskon Anggota 10%", TipeDiskon: "persen",
			NilaiDiskon: 10, MinBelanja: 20000, KuotaTotal: 100, SekaliPerAnggota: true,
			MulaiPada: response.ToPGTimestamptz(now.Add(-time.Hour)), BerakhirPada: response.ToPGTimestamptz(now.Add(60 * 24 * time.Hour)),
		})
	}
	return err
}
