package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/response"
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

// HandleReadyz memeriksa kesiapan database PostgreSQL
func (h *HealthHandler) HandleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.pool.Ping(ctx); err != nil {
		response.Error(w, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "Koneksi ke database gagal")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"status":    "READY",
		"database":  "CONNECTED",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}
