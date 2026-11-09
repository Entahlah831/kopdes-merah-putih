package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
)

type KasHandler struct {
	pool *pgxpool.Pool
}

func NewKasHandler(pool *pgxpool.Pool) *KasHandler {
	return &KasHandler{pool: pool}
}

type KasMutasiItem struct {
	ID         uuid.UUID `json:"id"`
	NoBuktiKas string    `json:"no_bukti_kas"`
	Jenis      string    `json:"jenis"` // 'masuk' atau 'keluar'
	Kategori   string    `json:"kategori"`
	Nominal    string    `json:"nominal"`
	Keterangan string    `json:"keterangan"`
	CreatedAt  time.Time `json:"created_at"`
}

// HandleListKasMutasi mengambil riwayat pembukuan mutasi kas masuk/keluar (Pengurus / Pengawas / Auditor)
func (h *KasHandler) HandleListKasMutasi(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var list []KasMutasiItem

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, no_bukti_kas, jenis::text, kategori, nominal::text, keterangan, created_at
			FROM kas_mutasi
			WHERE koperasi_id = $1
			ORDER BY created_at DESC
			LIMIT 50;
		`, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var it KasMutasiItem
			if err := rows.Scan(
				&it.ID, &it.NoBuktiKas, &it.Jenis, &it.Kategori,
				&it.Nominal, &it.Keterangan, &it.CreatedAt,
			); err != nil {
				return err
			}
			list = append(list, it)
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal mengambil mutasi kas: %v", err))
		return
	}

	if list == nil {
		list = []KasMutasiItem{}
	}

	response.JSON(w, http.StatusOK, list, map[string]interface{}{
		"total": len(list),
	})
}
