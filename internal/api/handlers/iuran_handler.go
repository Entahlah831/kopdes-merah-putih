package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

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

type CreateJenisIuranRequest struct {
	Kode           string `json:"kode"`
	Nama           string `json:"nama"`
	Tipe           string `json:"tipe"` // 'pokok' | 'wajib' | 'sukarela'
	NominalStandar int64  `json:"nominal_standar"`
	Nominal        int64  `json:"nominal"`
}

// HandleCreateJenisIuran membuat jenis iuran baru
func (h *IuranHandler) HandleCreateJenisIuran(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	var req CreateJenisIuranRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.Kode == "" || req.Nama == "" {
		response.BadRequest(w, "Kode dan nama iuran wajib diisi")
		return
	}

	nominal := req.Nominal
	if nominal <= 0 {
		nominal = req.NominalStandar
	}
	if nominal <= 0 {
		nominal = 10000
	}

	ctx := r.Context()
	var newID uuid.UUID

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO jenis_iuran (
				koperasi_id, kode, nama, nominal, berlaku_mulai, created_by
			) VALUES ($1, $2, $3, $4, CURRENT_DATE, $5)
			RETURNING id;
		`, claims.KoperasiID, req.Kode, req.Nama, nominal, claims.UserID).Scan(&newID)
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal membuat jenis iuran: %v", err))
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"id":                    newID,
		"klasifikasi_akuntansi": "KEPUTUSAN AKUNTAN",
		"message":               "Jenis iuran berhasil dibuat",
	})
}

// HandleListJenisIuran menampilkan daftar jenis iuran aktif
func (h *IuranHandler) HandleListJenisIuran(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var list []map[string]interface{}

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, kode, nama, nominal::bigint, periode_tagih, status_aktif
			FROM jenis_iuran
			WHERE koperasi_id = $1 AND deleted_at IS NULL
			ORDER BY created_at ASC;
		`, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				id           uuid.UUID
				kode, nama   string
				nominal      int64
				periodeTagih string
				statusAktif  bool
			)
			if err := rows.Scan(&id, &kode, &nama, &nominal, &periodeTagih, &statusAktif); err != nil {
				return err
			}
			list = append(list, map[string]interface{}{
				"id":                    id,
				"kode":                  kode,
				"nama":                  nama,
				"tipe":                  periodeTagih,
				"nominal_standar":       nominal,
				"klasifikasi_akuntansi": "KEPUTUSAN AKUNTAN",
				"status_aktif":          statusAktif,
			})
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, "Gagal mengambil daftar jenis iuran")
		return
	}

	if list == nil {
		list = []map[string]interface{}{}
	}

	response.JSON(w, http.StatusOK, list)
}

// HandleGetTagihanSaya menampilkan daftar tagihan iuran milik anggota yang sedang login
func (h *IuranHandler) HandleGetTagihanSaya(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var tagihanList []map[string]interface{}

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// Cari anggota_id dari user_id login
		var anggotaID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT id FROM anggota WHERE user_id = $1 AND koperasi_id = $2 AND deleted_at IS NULL LIMIT 1;
		`, claims.UserID, claims.KoperasiID).Scan(&anggotaID)
		if err != nil {
			return errors.New("pengguna tidak terdaftar sebagai anggota koperasi")
		}

		rows, err := tx.Query(ctx, `
			SELECT t.id, t.periode, j.nama, t.nominal_tagihan::bigint, t.nominal_terbayar::bigint, t.status::text, t.jatuh_tempo
			FROM tagihan_iuran t
			JOIN jenis_iuran j ON j.id = t.jenis_iuran_id
			WHERE t.anggota_id = $1 AND t.koperasi_id = $2 AND t.deleted_at IS NULL
			ORDER BY t.periode DESC;
		`, anggotaID, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				tID            uuid.UUID
				periode, jNama string
				nomTag, nomTer int64
				status         string
				jatuhTempo     *time.Time
			)
			if err := rows.Scan(&tID, &periode, &jNama, &nomTag, &nomTer, &status, &jatuhTempo); err != nil {
				return err
			}
			tagihanList = append(tagihanList, map[string]interface{}{
				"id":                tID,
				"periode":           periode,
				"jenis_iuran_nama":  jNama,
				"nominal_tagihan":   nomTag,
				"nominal_terbayar":  nomTer,
				"status":            status,
				"jatuh_tempo":       jatuhTempo,
				"batas_waktu_bayar": jatuhTempo,
			})
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal mengambil tagihan anggota: %v", err))
		return
	}

	if tagihanList == nil {
		tagihanList = []map[string]interface{}{}
	}

	response.JSON(w, http.StatusOK, tagihanList)
}

