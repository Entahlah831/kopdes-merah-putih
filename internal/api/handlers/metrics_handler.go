package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type MetricsHandler struct {
	pool      *pgxpool.Pool
	startTime time.Time
}

func NewMetricsHandler(pool *pgxpool.Pool) *MetricsHandler {
	return &MetricsHandler{
		pool:      pool,
		startTime: time.Now(),
	}
}

// HandleMetrics mengembalikan metrik Prometheus operasional sistem
func (h *MetricsHandler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var (
		antreanVerifikasi int64
		slotAktif         int64
		jobGagal          int64
		partisiCount      int64
	)

	// 1. Antrean verifikasi pembayaran
	_ = h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pembayaran 
		WHERE status = 'menunggu_verifikasi' AND deleted_at IS NULL;
	`).Scan(&antreanVerifikasi)

	// 2. Slot kode unik aktif
	_ = h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pembayaran 
		WHERE status IN ('menunggu_pembayaran', 'menunggu_verifikasi') 
		  AND berlaku_sampai > NOW() 
		  AND deleted_at IS NULL;
	`).Scan(&slotAktif)

	// 3. Total job gagal
	_ = h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM job_log WHERE status = 'gagal';
	`).Scan(&jobGagal)

	// 4. Partisi kas_mutasi yang terpasang
	_ = h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_inherits
		WHERE inhparent = 'kas_mutasi'::regclass;
	`).Scan(&partisiCount)

	uptimeSeconds := time.Since(h.startTime).Seconds()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	_, _ = fmt.Fprintf(w, `# HELP kopdes_uptime_seconds Total waktu server aktif dalam detik
# TYPE kopdes_uptime_seconds gauge
kopdes_uptime_seconds %.2f

# HELP kopdes_pembayaran_antrean_verifikasi Jumlah transaksi yang menunggu verifikasi admin
# TYPE kopdes_pembayaran_antrean_verifikasi gauge
kopdes_pembayaran_antrean_verifikasi %d

# HELP kopdes_pembayaran_slot_kode_unik_aktif Jumlah slot kode unik QRIS yang sedang aktif digunakan
# TYPE kopdes_pembayaran_slot_kode_unik_aktif gauge
kopdes_pembayaran_slot_kode_unik_aktif %d

# HELP kopdes_job_gagal_total Jumlah kegagalan eksekusi background job
# TYPE kopdes_job_gagal_total counter
kopdes_job_gagal_total %d

# HELP kopdes_partisi_tersisa Jumlah partisi kas_mutasi aktif terkonfigurasi di database
# TYPE kopdes_partisi_tersisa gauge
kopdes_partisi_tersisa %d
`, uptimeSeconds, antreanVerifikasi, slotAktif, jobGagal, partisiCount)
}
