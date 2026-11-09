package handlers

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	db "github.com/kopdes/merah-putih/internal/db"
)

type KatalogHandler struct {
	pool *pgxpool.Pool
}

func NewKatalogHandler(pool *pgxpool.Pool) *KatalogHandler {
	return &KatalogHandler{pool: pool}
}

// HandleListStokGudang menampilkan stok saldo produk di seluruh gudang koperasi
func (h *KatalogHandler) HandleListStokGudang(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var stok []db.ListStokProdukGudangRow

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		queries := db.New(tx)
		kopUUID := pgtype.UUID{Bytes: claims.KoperasiID, Valid: true}
		rows, err := queries.ListStokProdukGudang(ctx, kopUUID)
		if err != nil {
			return err
		}
		stok = rows
		return nil
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal memuat stok: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, stok, map[string]interface{}{
		"total_varian": len(stok),
	})
}

// HandleGetHargaAnggota menghitung harga final ritel dengan diskon anggota & promo terbaik
func (h *KatalogHandler) HandleGetHargaAnggota(w http.ResponseWriter, r *http.Request) {
	produkIDStr := r.URL.Query().Get("produk_id")
	anggotaIDStr := r.URL.Query().Get("anggota_id")

	produkID, err := uuid.Parse(produkIDStr)
	if err != nil {
		response.BadRequest(w, "Parameter produk_id (UUID) tidak valid")
		return
	}

	anggotaID, err := uuid.Parse(anggotaIDStr)
	if err != nil {
		response.BadRequest(w, "Parameter anggota_id (UUID) tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var harga db.GetHargaAkhirAnggotaDenganPromoTerbaikRow

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		queries := db.New(tx)
		row, err := queries.GetHargaAkhirAnggotaDenganPromoTerbaik(ctx, db.GetHargaAkhirAnggotaDenganPromoTerbaikParams{
			KoperasiID: pgtype.UUID{Bytes: claims.KoperasiID, Valid: true},
			ID:         pgtype.UUID{Bytes: produkID, Valid: true},
			ID_2:       pgtype.UUID{Bytes: anggotaID, Valid: true},
		})
		if err != nil {
			return err
		}
		harga = row
		return nil
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal menghitung harga produk: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, harga)
}
