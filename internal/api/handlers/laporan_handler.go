package handlers

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
)

type LaporanHandler struct {
	pool *pgxpool.Pool
}

func NewLaporanHandler(pool *pgxpool.Pool) *LaporanHandler {
	return &LaporanHandler{pool: pool}
}

// HandleLaporanKeuangan menghasilkan ringkasan total pemasukan, pengeluaran, dan saldo kas terkini
func (h *LaporanHandler) HandleLaporanKeuangan(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var (
		totalPemasukan   int64
		totalPengeluaran int64
		saldoKas         int64
		periodeAktif     string
	)

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// Total mutasi pemasukan
		_ = tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(nominal), 0)::bigint
			FROM kas_mutasi
			WHERE koperasi_id = $1 AND jenis = 'pemasukan';
		`, claims.KoperasiID).Scan(&totalPemasukan)

		// Total mutasi pengeluaran
		_ = tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(nominal), 0)::bigint
			FROM kas_mutasi
			WHERE koperasi_id = $1 AND jenis = 'pengeluaran';
		`, claims.KoperasiID).Scan(&totalPengeluaran)

		// Periode kas aktif dan saldo
		_ = tx.QueryRow(ctx, `
			SELECT periode, (saldo_awal + total_pemasukan - total_pengeluaran)::bigint
			FROM periode_kas
			WHERE koperasi_id = $1 AND status = 'buka'
			ORDER BY created_at DESC LIMIT 1;
		`, claims.KoperasiID).Scan(&periodeAktif, &saldoKas)

		return nil
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal menyusun laporan keuangan: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"periode_aktif":     periodeAktif,
		"total_pemasukan":   totalPemasukan,
		"total_pengeluaran": totalPengeluaran,
		"saldo_kas_aktif":   saldoKas,
		"surplus_defisit":   totalPemasukan - totalPengeluaran,
	})
}

// HandleLaporanRekapAnggota menghasilkan ringkasan jumlah anggota per status
func (h *LaporanHandler) HandleLaporanRekapAnggota(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	rekap := make(map[string]int64)
	var totalAnggota int64

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT status::text, COUNT(*)
			FROM anggota
			WHERE koperasi_id = $1 AND deleted_at IS NULL
			GROUP BY status;
		`, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var st string
			var cnt int64
			if err := rows.Scan(&st, &cnt); err != nil {
				return err
			}
			rekap[st] = cnt
			totalAnggota += cnt
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, "Gagal menyusun laporan rekap anggota")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"total_anggota": totalAnggota,
		"rincian_status": rekap,
	})
}
