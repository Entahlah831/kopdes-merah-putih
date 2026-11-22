package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/config"
	"github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/pembayaran"
	"github.com/kopdes/merah-putih/internal/security"
)

func runBuktiPurge(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig, args []string) error {
	fs := flag.NewFlagSet("bukti purge", flag.ContinueOnError)
	days := fs.Int("days", cfg.BuktiRetentionDays, "Retensi hari berkas bukti bayar non-lunas")
	if err := fs.Parse(args); err != nil {
		return err
	}
	kpSet, err := security.NewPurposeKeySet(cfg.AESMasterKey, cfg.AESPreviousKeys)
	if err != nil {
		return err
	}
	svc, err := pembayaran.NewService(pool, pembayaran.Config{
		ProofStorageDir:    cfg.StorageBuktiDir,
		BuktiRetentionDays: *days,
		KeyProvider:        kpSet.Bukti,
		LegacyKeyProvider:  kpSet.Master,
	})
	if err != nil {
		return err
	}
	purged, err := svc.PurgeExpiredBukti(ctx, *days)
	if err != nil {
		return err
	}
	fmt.Printf("Bukti purge selesai: %d berkas bukti non-lunas kedaluwarsa dihapus\n", purged)
	return nil
}

func runStorageMigrateFiles(ctx context.Context, pool *pgxpool.Pool, cfg config.AppConfig) error {
	kpSet, err := security.NewPurposeKeySet(cfg.AESMasterKey, cfg.AESPreviousKeys)
	if err != nil {
		return err
	}
	kycAbs, buktiAbs, err := ensureStorageDirs(cfg.StorageKYCDir, cfg.StorageBuktiDir)
	if err != nil {
		return err
	}
	movedKYC, movedBukti, err := relocateLegacySubdirs(kycAbs, buktiAbs)
	if err != nil {
		return err
	}
	reencKYC, reencBukti, err := reencryptLegacyDBRecords(ctx, pool, kpSet, kycAbs, buktiAbs)
	if err != nil {
		return err
	}
	fmt.Printf("Migrasi storage selesai: kyc_moved=%d kyc_reencrypted=%d bukti_moved=%d bukti_reencrypted=%d\n",
		movedKYC, reencKYC, movedBukti, reencBukti)
	return nil
}

func ensureStorageDirs(kycDir, buktiDir string) (string, string, error) {
	kycAbs, err := filepath.Abs(strings.TrimSpace(kycDir))
	if err != nil {
		return "", "", err
	}
	buktiAbs, err := filepath.Abs(strings.TrimSpace(buktiDir))
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(kycAbs, 0o700); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(buktiAbs, 0o700); err != nil {
		return "", "", err
	}
	return kycAbs, buktiAbs, nil
}

func relocateLegacySubdirs(kycAbs, buktiAbs string) (int, int, error) {
	movedKYC := moveEncFiles(filepath.Join(kycAbs, "kyc"), kycAbs)
	movedBukti := moveEncFiles(filepath.Join(kycAbs, "bukti"), buktiAbs)
	return movedKYC, movedBukti, nil
}

func moveEncFiles(srcDir, dstDir string) int {
	if filepath.Clean(srcDir) == filepath.Clean(dstDir) {
		return 0
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".enc") {
			continue
		}
		name := filepath.Base(e.Name())
		src := filepath.Join(srcDir, name)
		dst := filepath.Join(dstDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		// #nosec G304,G703 -- nama berkas dibatasi filepath.Base akhiran .enc di direktori terkontrol
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		// #nosec G304,G703 -- dst berada di bawah dstDir dengan nama aman
		if err := os.WriteFile(dst, data, 0o600); err == nil {
			_ = os.Remove(src)
			moved++
		}
	}
	return moved
}

func reencryptLegacyDBRecords(ctx context.Context, pool *pgxpool.Pool, kpSet *security.PurposeKeySet, kycAbs, buktiAbs string) (int, int, error) {
	q := db.New(pool)
	reencKYC, err := migrateKYCEnvelopes(ctx, q, kpSet, kycAbs)
	if err != nil {
		return 0, 0, err
	}
	reencBukti, err := migrateBuktiEnvelopes(ctx, q, kpSet, buktiAbs)
	if err != nil {
		return reencKYC, 0, err
	}
	return reencKYC, reencBukti, nil
}

func migrateKYCEnvelopes(ctx context.Context, q *db.Queries, kpSet *security.PurposeKeySet, kycAbs string) (int, error) {
	docs, err := q.ListAllDokumenKYC(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, d := range docs {
		keyName := filepath.Base(strings.TrimSpace(d.StorageKey))
		target := filepath.Join(kycAbs, keyName)
		if !strings.HasPrefix(target, kycAbs+string(filepath.Separator)) {
			continue
		}
		// #nosec G304,G703 -- target terverifikasi di bawah kycAbs
		raw, err := os.ReadFile(target)
		if err != nil {
			continue
		}
		aad := security.FormatAAD("dokumen_kyc", d.TipeDokumen, response.FromPGUUID(d.PendaftaranID).String())
		if _, err := security.DecryptEnvelopeFile(kpSet.KYC, raw, d.DekWrapped, aad); err == nil {
			continue
		}
		plain, err := security.DecryptEnvelopeFile(kpSet.Master, raw, d.DekWrapped, aad)
		if err != nil {
			continue
		}
		cipherNew, wrappedNew, keyIDNew, err := security.EncryptEnvelopeFile(kpSet.KYC, plain, aad)
		if err != nil {
			return count, err
		}
		// #nosec G304,G703 -- target terverifikasi di bawah kycAbs
		if err := os.WriteFile(target, cipherNew, 0o600); err != nil {
			return count, err
		}
		if err := q.UpdateDokumenKYCEnvelope(ctx, db.UpdateDokumenKYCEnvelopeParams{
			ID: d.ID, KeyID: keyIDNew, DekWrapped: wrappedNew,
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func migrateBuktiEnvelopes(ctx context.Context, q *db.Queries, kpSet *security.PurposeKeySet, buktiAbs string) (int, error) {
	rows, err := q.ListPembayaranWithBukti(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, r := range rows {
		idUUID := response.FromPGUUID(r.ID)
		if idUUID == uuid.Nil {
			continue
		}
		target := filepath.Join(buktiAbs, "bukti_"+idUUID.String()+".enc")
		// #nosec G304,G703 -- target disusun dari UUID tervalidasi
		raw, err := os.ReadFile(target)
		if err != nil {
			continue
		}
		aad := security.FormatAAD("pembayaran", "bukti", idUUID.String())
		if len(r.BuktiDekWrapped) > 0 {
			if _, dErr := security.DecryptEnvelopeFile(kpSet.Bukti, raw, r.BuktiDekWrapped, aad); dErr == nil {
				continue
			}
		}
		plain, ok := decryptLegacyBuktiBytes(kpSet, raw, r.BuktiDekWrapped, aad)
		if !ok {
			continue
		}
		cipherNew, wrappedNew, keyIDNew, err := security.EncryptEnvelopeFile(kpSet.Bukti, plain, aad)
		if err != nil {
			return count, err
		}
		// #nosec G304,G703 -- target disusun dari UUID tervalidasi
		if err := os.WriteFile(target, cipherNew, 0o600); err != nil {
			return count, err
		}
		if err := q.UpdatePembayaranBuktiEnvelope(ctx, db.UpdatePembayaranBuktiEnvelopeParams{
			ID: r.ID, BuktiKeyID: keyIDNew, BuktiDekWrapped: wrappedNew,
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func decryptLegacyBuktiBytes(kpSet *security.PurposeKeySet, raw, wrappedDEK, aad []byte) ([]byte, bool) {
	if len(wrappedDEK) > 0 {
		if pt, err := security.DecryptEnvelopeFile(kpSet.Master, raw, wrappedDEK, aad); err == nil {
			return pt, true
		}
		return nil, false
	}
	if pt, err := kpSet.Master.DecryptWithAAD(raw, aad); err == nil {
		return pt, true
	}
	if pt, err := kpSet.Bukti.DecryptWithAAD(raw, aad); err == nil {
		return pt, true
	}
	return nil, false
}
