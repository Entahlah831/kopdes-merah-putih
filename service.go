package kopdes

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrKodeUnikPenuh = errors.New("seluruh 999 slot kode unik QRIS untuk nominal ini sedang aktif digunakan")

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
}

type BuatPembayaranQRISRes struct {
	PembayaranID    uuid.UUID
	KodeUnikAwal    int
	KodeUnikTerpilih int
	JumlahPercobaan int
	NominalTotalRp  int64
	BerlakuSampai   time.Time
}

// HitungKodeUnikAwal menghitung: (uint32(HMAC-SHA256(secret, anggota_id || koperasi_id)[0:4]) mod 999) + 1
func HitungKodeUnikAwal(secret []byte, anggotaID, koperasiID uuid.UUID) int {
	mac := hmac.New(sha256.New, secret)
	mac.Write(anggotaID[:])
	mac.Write(koperasiID[:])
	sum := mac.Sum(nil)
	val := binary.BigEndian.Uint32(sum[0:4])
	return int(val%999) + 1
}

// BuatPembayaranQRISStatis mengalokasikan kode unik (1..999) dengan linear probing dan SAVEPOINT
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

	kodeAwal := HitungKodeUnikAwal(secret, req.AnggotaID, req.KoperasiID)
	berlakuSampai := time.Now().UTC().Add(req.TTL)

	for percobaan := 1; percobaan <= 999; percobaan++ {
		kodeKandidat := ((kodeAwal - 1 + (percobaan - 1)) % 999) + 1

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