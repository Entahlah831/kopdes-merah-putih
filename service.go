package kopdes

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/security"
)

var ErrKodeUnikPenuh = errors.New("seluruh slot kode unik QRIS untuk nominal ini sedang aktif digunakan")

type AlokasiReq struct {
	JenisAlokasi   string
	TagihanIuranID *uuid.UUID
	PesananID      *uuid.UUID
	NominalAlokasi int64
	UrutanAlokasi  int
}

type BuatPembayaranQRISReq struct {
	KoperasiID         uuid.UUID
	PembayarUserID     uuid.UUID
	AnggotaID          uuid.UUID
	MetodePembayaranID uuid.UUID
	RekeningQRISID     uuid.UUID
	JenisTransaksi     string
	NoPembayaran       string
	NominalDasarRp     int64
	TTL                time.Duration
	Alokasi            []AlokasiReq
	KodeUnikDigits     int // 3 digit (1..999 slot) atau 4 digit (1..9999 slot)
}

type BuatPembayaranQRISRes struct {
	PembayaranID     uuid.UUID
	KodeUnikAwal     int
	KodeUnikTerpilih int
	JumlahPercobaan  int
	NominalTotalRp   int64
	BerlakuSampai    time.Time
}

// HitungKodeUnikAwal menghitung HMAC-SHA256(secret, anggota_id || koperasi_id) mod 999 + 1
func HitungKodeUnikAwal(secret []byte, anggotaID, koperasiID uuid.UUID) int {
	return HitungKodeUnikAwalWithSlots(secret, anggotaID, koperasiID, 999)
}

// HitungKodeUnikAwalWithSlots menghitung kode unik awal berbasis kapasitas slot (3 digit: 999, 4 digit: 9999)
func HitungKodeUnikAwalWithSlots(secret []byte, anggotaID, koperasiID uuid.UUID, maxSlots int) int {
	if maxSlots <= 0 {
		maxSlots = 999
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(anggotaID[:])
	mac.Write(koperasiID[:])
	sum := mac.Sum(nil)
	val := binary.BigEndian.Uint32(sum[0:4])
	return int(val%uint32(maxSlots)) + 1
}

// GetSlotUsage mengambil data pemakaian slot kode unik terkini untuk kombinasi (koperasi, rekening, nominal)
func GetSlotUsage(ctx context.Context, pool *pgxpool.Pool, koperasiID, rekeningID uuid.UUID, nominalDasar int64, maxSlots int) (int, int, float64, error) {
	if maxSlots <= 0 {
		maxSlots = 999
	}
	var active int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pembayaran
		WHERE koperasi_id = $1
		  AND rekening_qris_id = $2
		  AND nominal_dasar = $3
		  AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi')
		  AND berlaku_sampai > NOW()
		  AND deleted_at IS NULL;
	`, koperasiID, rekeningID, nominalDasar).Scan(&active)
	if err != nil {
		return 0, maxSlots, 0, err
	}
	ratio := float64(active) / float64(maxSlots)
	return active, maxSlots, ratio, nil
}

// BuatPembayaranQRISStatis mengalokasikan kode unik (1..maxSlots) dengan linear probing dan SAVEPOINT
// terhadap partial unique index idx_pembayaran_qris_nominal_aktif.
func BuatPembayaranQRISStatis(ctx context.Context, pool *pgxpool.Pool, secret []byte, req BuatPembayaranQRISReq) (*BuatPembayaranQRISRes, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", req.KoperasiID.String()); err != nil {
		return nil, err
	}

	maxSlots := 999
	if req.KodeUnikDigits == 4 {
		maxSlots = 9999
	}

	// Metrik pemakaian slot & peringatan di atas 70%
	var activeSlots int
	_ = tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pembayaran
		WHERE koperasi_id = $1
		  AND rekening_qris_id = $2
		  AND nominal_dasar = $3
		  AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi')
		  AND berlaku_sampai > NOW()
		  AND deleted_at IS NULL;
	`, req.KoperasiID, req.RekeningQRISID, req.NominalDasarRp).Scan(&activeSlots)

	if maxSlots > 0 {
		rasio := float64(activeSlots) / float64(maxSlots)
		if rasio >= 0.70 {
			log.Printf("[PERINGATAN KAPASITAS QRIS] Pemakaian slot kode unik telah mencapai %.1f%% (%d/%d slot) untuk nominal Rp%d pada rekening %s",
				rasio*100.0, activeSlots, maxSlots, req.NominalDasarRp, req.RekeningQRISID)
		}
	}

	kodeAwal := HitungKodeUnikAwalWithSlots(secret, req.AnggotaID, req.KoperasiID, maxSlots)
	berlakuSampai := time.Now().UTC().Add(req.TTL)

	for percobaan := 1; percobaan <= maxSlots; percobaan++ {
		kodeKandidat := ((kodeAwal - 1 + (percobaan - 1)) % maxSlots) + 1

		if _, err := tx.Exec(ctx, "SAVEPOINT sp_qris_unik"); err != nil {
			return nil, err
		}

		var pembayaranID uuid.UUID
		var nominalTotalStr string
		err = tx.QueryRow(ctx, `
			INSERT INTO pembayaran (
				koperasi_id, no_pembayaran, pembayar_user_id, anggota_id,
				metode_pembayaran_id, rekening_qris_id, jenis_transaksi,
				nominal_dasar, kode_unik, kode_unik_awal, jumlah_percobaan,
				berlaku_sampai, status, created_by
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, $7,
				$8, $9, $10, $11,
				$12, 'menunggu_pembayaran', $3
			)
			RETURNING id, nominal_total::text
		`,
			req.KoperasiID, req.NoPembayaran, req.PembayarUserID, req.AnggotaID,
			req.MetodePembayaranID, req.RekeningQRISID, req.JenisTransaksi,
			req.NominalDasarRp, kodeKandidat, kodeAwal, percobaan,
			berlakuSampai,
		).Scan(&pembayaranID, &nominalTotalStr)

		if err == nil {
			for _, al := range req.Alokasi {
				_, err = tx.Exec(ctx, `
					INSERT INTO pembayaran_alokasi (
						koperasi_id, pembayaran_id, jenis_alokasi,
						tagihan_iuran_id, pesanan_id, urutan_alokasi,
						nominal_alokasi, created_by
					) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				`, req.KoperasiID, pembayaranID, al.JenisAlokasi, al.TagihanIuranID, al.PesananID, al.UrutanAlokasi, al.NominalAlokasi, req.PembayarUserID)
				if err != nil {
					_ = tx.Rollback(ctx)
					return nil, fmt.Errorf("gagal mencatat pembayaran_alokasi: %w", err)
				}
			}

			_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT sp_qris_unik")
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &BuatPembayaranQRISRes{
				PembayaranID:     pembayaranID,
				KodeUnikAwal:     kodeAwal,
				KodeUnikTerpilih: kodeKandidat,
				JumlahPercobaan:  percobaan,
				NominalTotalRp:   req.NominalDasarRp + int64(kodeKandidat),
				BerlakuSampai:    berlakuSampai,
			}, nil
		}

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_pembayaran_qris_nominal_aktif" {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sp_qris_unik")
			continue
		}
		return nil, err
	}

	return nil, ErrKodeUnikPenuh
}

// GenerateTagihanIuranBulanan membuat tagihan bulanan secara idempoten untuk seluruh anggota aktif
// menggunakan ON CONFLICT (koperasi_id, anggota_id, jenis_iuran_id, periode) DO NOTHING.
func GenerateTagihanIuranBulanan(ctx context.Context, pool *pgxpool.Pool, koperasiID, jenisIuranID, actorUserID uuid.UUID, periode string) (int64, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, true)", koperasiID.String()); err != nil {
		return 0, err
	}

	kunciIdempoten := fmt.Sprintf("tagihan_iuran:%s:%s:%s", koperasiID.String(), jenisIuranID.String(), periode)
	var jobID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO job_log (koperasi_id, nama_job, kunci_idempoten, periode, status, created_by)
		VALUES ($1, 'generate_tagihan_iuran_bulanan', $2, $3, 'berjalan', $4)
		ON CONFLICT (kunci_idempoten) DO UPDATE SET mulai_pada = NOW()
		RETURNING id
	`, koperasiID, kunciIdempoten, periode, actorUserID).Scan(&jobID)
	if err != nil {
		return 0, err
	}

	cmdTag, err := tx.Exec(ctx, `
		INSERT INTO tagihan_iuran (
			koperasi_id, anggota_id, jenis_iuran_id, periode,
			jatuh_tempo, nominal_pokok, status, job_eksekusi_id, created_by
		)
		SELECT
			a.koperasi_id,
			a.id,
			ji.id,
			$3,
			make_date(
				substring($3 from 1 for 4)::int,
				substring($3 from 6 for 2)::int,
				ji.hari_jatuh_tempo
			),
			ji.nominal,
			'belum_bayar',
			$4,
			$5
		FROM anggota a
		JOIN jenis_iuran ji ON ji.koperasi_id = a.koperasi_id AND ji.id = $2
		WHERE a.koperasi_id = $1
          AND a.status = 'aktif'
          AND a.deleted_at IS NULL
          AND ji.status_aktif = true
          AND ji.deleted_at IS NULL
		ON CONFLICT (koperasi_id, anggota_id, jenis_iuran_id, periode) DO NOTHING
	`, koperasiID, jenisIuranID, periode, jobID, actorUserID)
	if err != nil {
		return 0, err
	}

	barisBaru := cmdTag.RowsAffected()
	_, err = tx.Exec(ctx, `
		UPDATE job_log
		SET status = 'selesai',
		    jumlah_berhasil = $2,
		    selesai_pada = NOW()
		WHERE id = $1
	`, jobID, barisBaru)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return barisBaru, nil
}

// VerifyHashChain memvalidasi integritas rantai hash kriptografis SHA-256 pada tabel append-only
func VerifyHashChain(ctx context.Context, pool *pgxpool.Pool, tableName string, koperasiID uuid.UUID) (bool, int64, *uuid.UUID, error) {
	if tableName != "audit_log" && tableName != "kas_mutasi" && tableName != "pembayaran_verifikasi_log" {
		return false, 0, nil, fmt.Errorf("tabel %s bukan tabel append-only berantai hash", tableName)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, 0, nil, err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT set_config('app.current_koperasi_id', $1, false)", koperasiID.String()); err != nil {
		return false, 0, nil, err
	}

	query := fmt.Sprintf(`
		SELECT id, prev_hash, row_hash
		FROM %s
		WHERE koperasi_id = $1
		ORDER BY created_at ASC, id ASC;
	`, tableName)

	rows, err := conn.Query(ctx, query, koperasiID)
	if err != nil {
		return false, 0, nil, err
	}
	defer rows.Close()

	var count int64
	expectedPrev := security.GenesisHash

	for rows.Next() {
		var (
			id       uuid.UUID
			prevHash *string
			rowHash  *string
		)
		if err := rows.Scan(&id, &prevHash, &rowHash); err != nil {
			return false, count, nil, err
		}

		actualPrev := security.GenesisHash
		if prevHash != nil && *prevHash != "" {
			actualPrev = *prevHash
		}

		if actualPrev != expectedPrev {
			return false, count, &id, fmt.Errorf("rantai integritas terputus pada id %s: prev_hash didapat %s, seharusnya %s", id, actualPrev, expectedPrev)
		}

		if rowHash == nil || *rowHash == "" {
			return false, count, &id, fmt.Errorf("row_hash kosong pada id %s", id)
		}

		expectedPrev = *rowHash
		count++
	}

	return true, count, nil, rows.Err()
}
