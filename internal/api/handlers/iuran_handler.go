package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	kopdes "github.com/kopdes/merah-putih"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	db "github.com/kopdes/merah-putih/internal/db"
)

type IuranHandler struct {
	pool *pgxpool.Pool
}

func NewIuranHandler(pool *pgxpool.Pool) *IuranHandler {
	return &IuranHandler{pool: pool}
}

type GenerateTagihanRequest struct {
	JenisIuranID uuid.UUID `json:"jenis_iuran_id"`
	Periode      string    `json:"periode"` // YYYY-MM
}

// HandleGenerateTagihan menghasilkan tagihan iuran bulanan untuk seluruh anggota aktif
func (h *IuranHandler) HandleGenerateTagihan(w http.ResponseWriter, r *http.Request) {
	var req GenerateTagihanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.JenisIuranID == uuid.Nil || req.Periode == "" {
		response.BadRequest(w, "jenis_iuran_id dan periode (YYYY-MM) wajib diisi")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	totalDibuat, err := kopdes.GenerateTagihanIuranBulanan(
		r.Context(),
		h.pool,
		claims.KoperasiID,
		req.JenisIuranID,
		claims.UserID,
		req.Periode,
	)

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal generate tagihan: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"periode":              req.Periode,
		"jumlah_tagihan_baru": totalDibuat,
		"pesan":                "Generate tagihan iuran bulanan selesai secara idempoten.",
	})
}

// HandleListMenunggak menampilkan daftar anggota yang menunggak iuran
func (h *IuranHandler) HandleListMenunggak(w http.ResponseWriter, r *http.Request) {
	minBulanStr := r.URL.Query().Get("min_bulan")
	minBulan := int64(1)
	if minBulanStr != "" {
		if val, err := strconv.ParseInt(minBulanStr, 10, 64); err == nil && val > 0 {
			minBulan = val
		}
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var list []db.ListAnggotaMenunggakIuranRow

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		queries := db.New(tx)
		kopUUID := pgtype.UUID{Bytes: claims.KoperasiID, Valid: true}
		rows, err := queries.ListAnggotaMenunggakIuran(ctx, db.ListAnggotaMenunggakIuranParams{
			KoperasiID: kopUUID,
			MinBulan:   minBulan,
		})
		if err != nil {
			return err
		}
		list = rows
		return nil
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal mengambil data penunggak iuran: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, list, map[string]interface{}{
		"total_anggota": len(list),
		"filter_min":    minBulan,
	})
}
