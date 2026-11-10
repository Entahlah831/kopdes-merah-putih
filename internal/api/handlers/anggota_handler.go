package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
)

type AnggotaHandler struct {
	pool *pgxpool.Pool
}

func NewAnggotaHandler(pool *pgxpool.Pool) *AnggotaHandler {
	return &AnggotaHandler{pool: pool}
}

type AnggotaItemResponse struct {
	ID               uuid.UUID `json:"id"`
	NoAnggota        string    `json:"no_anggota"`
	NamaLengkap      string    `json:"nama_lengkap"`
	Status           string    `json:"status"`
	JenisKelamin     string    `json:"jenis_kelamin"`
	TanggalBergabung string    `json:"tanggal_bergabung"`
	CreatedAt        time.Time `json:"created_at"`
}

// HandleListAnggota mengambil daftar anggota dalam koperasi
func (h *AnggotaHandler) HandleListAnggota(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var anggotaList []AnggotaItemResponse

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, no_anggota, nama_lengkap, status::text, jenis_kelamin, tanggal_bergabung::text, created_at
			FROM anggota
			WHERE koperasi_id = $1 AND deleted_at IS NULL
			ORDER BY created_at DESC
			LIMIT 100;
		`, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var a AnggotaItemResponse
			if err := rows.Scan(&a.ID, &a.NoAnggota, &a.NamaLengkap, &a.Status, &a.JenisKelamin, &a.TanggalBergabung, &a.CreatedAt); err != nil {
				return err
			}
			anggotaList = append(anggotaList, a)
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal mengambil data anggota: %v", err))
		return
	}

	if anggotaList == nil {
		anggotaList = []AnggotaItemResponse{}
	}

	response.JSON(w, http.StatusOK, anggotaList, map[string]interface{}{
		"total": len(anggotaList),
	})
}

// HandleGetAnggota mengambil detail anggota
func (h *AnggotaHandler) HandleGetAnggota(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	anggotaID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID anggota tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var a AnggotaItemResponse

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, no_anggota, nama_lengkap, status::text, jenis_kelamin, tanggal_bergabung::text, created_at
			FROM anggota
			WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL;
		`, anggotaID, claims.KoperasiID).Scan(&a.ID, &a.NoAnggota, &a.NamaLengkap, &a.Status, &a.JenisKelamin, &a.TanggalBergabung, &a.CreatedAt)
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, "Anggota tidak ditemukan")
			return
		}
		response.InternalServerError(w, "Gagal mengambil data anggota")
		return
	}

	response.JSON(w, http.StatusOK, a)
}

type UpdateStatusAnggotaRequest struct {
	Status  string `json:"status"`
	Alasan  string `json:"alasan"`
	Catatan string `json:"catatan"`
}

// HandleUpdateStatusAnggota mengubah status keanggotaan dan mencatat ke anggota_status_log (append-only)
func (h *AnggotaHandler) HandleUpdateStatusAnggota(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	anggotaID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID anggota tidak valid")
		return
	}

	var req UpdateStatusAnggotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.Status == "" {
		response.BadRequest(w, "Status baru wajib diisi")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		var oldStatus string
		err := tx.QueryRow(ctx, `
			SELECT status::text FROM anggota
			WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL
			FOR UPDATE;
		`, anggotaID, claims.KoperasiID).Scan(&oldStatus)
		if err != nil {
			return err
		}

		// Update status anggota
		_, err = tx.Exec(ctx, `
			UPDATE anggota
			SET status = $1::status_anggota_enum,
			    updated_at = NOW()
			WHERE id = $2 AND koperasi_id = $3;
		`, req.Status, anggotaID, claims.KoperasiID)
		if err != nil {
			return fmt.Errorf("gagal update status anggota: %w", err)
		}

		alasan := req.Alasan
		if alasan == "" {
			if req.Catatan != "" {
				alasan = req.Catatan
			} else {
				alasan = "Perubahan status administratif"
			}
		}

		// Catat ke log append-only anggota_status_log
		_, err = tx.Exec(ctx, `
			INSERT INTO anggota_status_log (
				koperasi_id, anggota_id, status_lama, status_baru,
				alasan, created_by
			) VALUES ($1, $2, $3::status_anggota_enum, $4::status_anggota_enum, $5, $6);
		`, claims.KoperasiID, anggotaID, oldStatus, req.Status, alasan, claims.UserID)
		return err
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, "Anggota tidak ditemukan")
			return
		}
		response.InternalServerError(w, fmt.Sprintf("Gagal memperbarui status anggota: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message":    "Status anggota berhasil diperbarui",
		"status":     req.Status,
		"anggota_id": anggotaID,
	})
}
