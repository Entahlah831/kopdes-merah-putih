package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/db"
)

type HealthHandler struct {
	pool *pgxpool.Pool
}

func NewHealthHandler(pool *pgxpool.Pool) *HealthHandler {
	return &HealthHandler{pool: pool}
}

// HandleHealthz memeriksa liveness server HTTP
func (h *HealthHandler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"status":    "UP",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// HandleReadyz memeriksa kesiapan database PostgreSQL dan telemetri tugas worker terjadwal.
func (h *HealthHandler) HandleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if h.pool == nil {
		response.Error(w, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "Pool database belum diinisialisasi")
		return
	}
	if err := h.pool.Ping(ctx); err != nil {
		response.Error(w, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "Koneksi ke database gagal")
		return
	}

	jobs, degraded := collectWorkerTelemetry(ctx, h.pool)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"status":          "READY",
		"database":        "CONNECTED",
		"worker_degraded": degraded,
		"worker_jobs":     jobs,
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	})
}

func collectWorkerTelemetry(ctx context.Context, pool *pgxpool.Pool) ([]map[string]interface{}, bool) {
	rows, err := db.New(pool).ListWorkerRuns(ctx)
	if err != nil {
		return []map[string]interface{}{}, false
	}
	jobs := make([]map[string]interface{}, 0, len(rows))
	degraded := false
	now := time.Now().UTC()
	for _, r := range rows {
		stale := isWorkerJobStale(r.JobName, r.LastFinishedAt.Time, r.LastFinishedAt.Valid, now)
		if r.LastStatus == "error" || stale {
			degraded = true
		}
		var fin interface{}
		if r.LastFinishedAt.Valid {
			fin = r.LastFinishedAt.Time.UTC().Format(time.RFC3339)
		}
		jobs = append(jobs, map[string]interface{}{
			"job_name":         r.JobName,
			"last_status":      r.LastStatus,
			"last_summary":     r.LastSummary,
			"last_error":       r.LastError,
			"run_count":        r.RunCount,
			"last_finished_at": fin,
			"stale":            stale,
		})
	}
	return jobs, degraded
}

func isWorkerJobStale(jobName string, finishedAt time.Time, valid bool, now time.Time) bool {
	if !valid {
		return false
	}
	age := now.Sub(finishedAt)
	if jobName == "pesanan_expire" || jobName == "rate_limits_cleanup" {
		return age > 15*time.Minute
	}
	return age > 26*time.Hour
}
