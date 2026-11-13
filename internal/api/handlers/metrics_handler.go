package handlers

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/db"
)

type MetricsHandler struct {
	pool                 *pgxpool.Pool
	startTime            time.Time
	metricsToken         string
	metricsPreviousToken string
	trustedProxies       *middleware.TrustedProxyMatcher
}

func NewMetricsHandler(pool *pgxpool.Pool, metricsToken string, trustedProxies *middleware.TrustedProxyMatcher) *MetricsHandler {
	return NewMetricsHandlerWithRotation(pool, metricsToken, "", trustedProxies)
}

func NewMetricsHandlerWithRotation(pool *pgxpool.Pool, metricsToken, metricsPreviousToken string, trustedProxies *middleware.TrustedProxyMatcher) *MetricsHandler {
	return &MetricsHandler{
		pool:                 pool,
		startTime:            time.Now(),
		metricsToken:         metricsToken,
		metricsPreviousToken: metricsPreviousToken,
		trustedProxies:       trustedProxies,
	}
}

func matchMetricsTokenConstantTime(expected, provided string) bool {
	if expected == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

// HandleMetrics mengembalikan metrik Prometheus operasional sistem (wajib token otentikasi internal)
func (h *MetricsHandler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	customHeader := r.Header.Get("X-Metrics-Token")
	tokenProvided := ""
	if strings.HasPrefix(authHeader, "Bearer ") {
		tokenProvided = strings.TrimPrefix(authHeader, "Bearer ")
	} else if customHeader != "" {
		tokenProvided = customHeader
	}

	tokenValid := matchMetricsTokenConstantTime(h.metricsToken, tokenProvided) ||
		matchMetricsTokenConstantTime(h.metricsPreviousToken, tokenProvided)

	// Wajibkan token selalu, jangan pernah percaya "IP internal" dari RemoteAddr atau proxy
	if !tokenValid {
		w.Header().Set("WWW-Authenticate", `Bearer realm="kopdes-metrics"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"UNAUTHORIZED","message":"Akses ke /metrics mewajibkan token otentikasi internal yang valid"}}`))
		return
	}

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

	// 5. Metrik partisi bulanan & partisi default (Task 2)
	partMetrics, _ := db.GetPartitionMetrics(ctx, h.pool)

	alertVal := 0
	if partMetrics.AlertActive {
		alertVal = 1
	}

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

# HELP kopdes_partisi_bulan_tersisa Jumlah bulan ke depan yang memiliki partisi terdaftar
# TYPE kopdes_partisi_bulan_tersisa gauge
kopdes_partisi_bulan_tersisa{tabel="audit_log"} %d
kopdes_partisi_bulan_tersisa{tabel="kas_mutasi"} %d
kopdes_partisi_bulan_tersisa{tabel="stok_mutasi"} %d
kopdes_partisi_bulan_tersisa{tabel="notifikasi"} %d

# HELP kopdes_partisi_default_row_count Jumlah baris data di partisi DEFAULT (harus 0)
# TYPE kopdes_partisi_default_row_count gauge
kopdes_partisi_default_row_count{tabel="audit_log"} %d
kopdes_partisi_default_row_count{tabel="kas_mutasi"} %d
kopdes_partisi_default_row_count{tabel="stok_mutasi"} %d
kopdes_partisi_default_row_count{tabel="notifikasi"} %d

# HELP kopdes_partisi_peringatan Status peringatan partisi (1 jika partisi default terisi atau bulan tersisa < 2, 0 jika normal)
# TYPE kopdes_partisi_peringatan gauge
kopdes_partisi_peringatan %d
`, uptimeSeconds, antreanVerifikasi, slotAktif, jobGagal, partisiCount,
		partMetrics.MonthsRemaining["audit_log"],
		partMetrics.MonthsRemaining["kas_mutasi"],
		partMetrics.MonthsRemaining["stok_mutasi"],
		partMetrics.MonthsRemaining["notifikasi"],
		partMetrics.AuditLogDefaultRows,
		partMetrics.KasMutasiDefaultRows,
		partMetrics.StokMutasiDefaultRows,
		partMetrics.NotifikasiDefaultRows,
		alertVal,
	)
}
