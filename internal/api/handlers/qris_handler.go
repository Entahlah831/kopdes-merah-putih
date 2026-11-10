package handlers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	kopdes "github.com/kopdes/merah-putih"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	db "github.com/kopdes/merah-putih/internal/db"
	"github.com/kopdes/merah-putih/internal/qris"
)

type QRISHandler struct {
	pool                      *pgxpool.Pool
	qrisSecret                []byte
	dynamicQRISEnabled        bool
	allowPersonalPaymentPilot bool
}

func NewQRISHandler(pool *pgxpool.Pool, qrisSecret []byte) *QRISHandler {
	return NewQRISHandlerWithConfig(pool, qrisSecret, false, false)
}

func NewQRISHandlerWithConfig(pool *pgxpool.Pool, qrisSecret []byte, dynamicQRISEnabled bool, allowPersonalPaymentPilot bool) *QRISHandler {
	return &QRISHandler{
		pool:                      pool,
		qrisSecret:                qrisSecret,
		dynamicQRISEnabled:        dynamicQRISEnabled,
		allowPersonalPaymentPilot: allowPersonalPaymentPilot,
	}
}

type UUIDSlice []uuid.UUID

func (s *UUIDSlice) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 || trimmed == "null" {
		*s = nil
		return nil
	}
	if trimmed[0] == '[' {
		var list []uuid.UUID
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
		*s = list
		return nil
	}
	var single uuid.UUID
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	*s = []uuid.UUID{single}
	return nil
}

type BuatQRISRequest struct {
	NominalDasarRp        int64      `json:"nominal_dasar,omitempty"`
	TagihanIuranIDs       UUIDSlice  `json:"tagihan_iuran_id,omitempty"`
	TagihanIuranIDsPlural UUIDSlice  `json:"tagihan_iuran_ids,omitempty"`
	PesananID             *uuid.UUID `json:"pesanan_id,omitempty"`
	JenisTransaksi        string     `json:"jenis_transaksi,omitempty"`
	AnggotaID             uuid.UUID  `json:"anggota_id,omitempty"`
	MetodePembayaranID    uuid.UUID  `json:"metode_pembayaran_id,omitempty"`
	RekeningQRISID        uuid.UUID  `json:"rekening_qris_id,omitempty"`
}

type BuatQRISResponse struct {
	PembayaranID       uuid.UUID `json:"pembayaran_id"`
	NoPembayaran       string    `json:"no_pembayaran"`
	KodeUnik           int       `json:"kode_unik"`
	NominalDasarRp     int64     `json:"nominal_dasar"`
	NominalTotalRp     int64     `json:"nominal_total"`
	QRISPayload        string    `json:"qris_payload"`
	QRISPayloadDinamis string    `json:"qris_payload_dinamis,omitempty"`
	QRISPayloadStatis  string    `json:"qris_payload_statis,omitempty"`
	IsAutoLockNominal  bool      `json:"is_autolock_nominal"`
	NamaMerchant       string    `json:"nama_merchant"`
	NMID               string    `json:"nmid"`
	BerlakuSampai      time.Time `json:"berlaku_sampai"`
	SisaWaktuDetik     int64     `json:"sisa_waktu_detik"`
}

// HandleBuatQRIS menghasilkan alokasi transaksi QRIS statis 3-digit anti-tabrakan
func (h *QRISHandler) HandleBuatQRIS(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "Gagal membaca body request")
		return
	}

	var req BuatQRISRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Konteks tenant koperasi tidak valid")
		return
	}

	// Idempotency-Key support berbasis database (Task 4)
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	reqHash := db.HashPayload(rawBody)

	if idempotencyKey != "" {
		cachedRec, err := db.CheckIdempotency(r.Context(), h.pool, claims.KoperasiID, claims.UserID, idempotencyKey, reqHash)
		if err != nil {
			if errors.Is(err, db.ErrIdempotencyConflict) {
				response.JSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
					"success": false,
					"error": map[string]interface{}{
						"code":    "IDEMPOTENCY_CONFLICT",
						"message": "Idempotency-Key telah digunakan dengan payload request yang berbeda.",
					},
				})
				return
			}
		} else if cachedRec != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(cachedRec.ResponseCode)
			_, _ = w.Write(cachedRec.ResponseBody)
			return
		}
	}

	// 1. Kumpulkan seluruh ID tagihan yang diminta dan deduplikasi
	var allTagihanIDs []uuid.UUID
	allTagihanIDs = append(allTagihanIDs, req.TagihanIuranIDs...)
	allTagihanIDs = append(allTagihanIDs, req.TagihanIuranIDsPlural...)

	seenTagihan := make(map[uuid.UUID]bool)
	var dedupTagihanIDs []uuid.UUID
	for _, id := range allTagihanIDs {
		if id != uuid.Nil && !seenTagihan[id] {
			seenTagihan[id] = true
			dedupTagihanIDs = append(dedupTagihanIDs, id)
		}
	}
	allTagihanIDs = dedupTagihanIDs

	// Wajib terikat minimal ke 1 tagihan atau 1 pesanan (anti arbitrary payment T1)
	if len(allTagihanIDs) == 0 && req.PesananID == nil {
		response.BadRequest(w, "Pembayaran wajib terikat pada minimal satu tagihan iuran atau pesanan")
		return
	}

	ctx := r.Context()

	if req.JenisTransaksi == "" {
		if req.PesananID != nil {
			req.JenisTransaksi = "pesanan_katalog"
		} else {
			req.JenisTransaksi = "iuran_bulanan"
		}
	}

	// 2. Validasi anggota, rekening QRIS, metode pembayaran, dan hitung nominal_dasar dari tagihan/pesanan
	var (
		targetAnggotaID   uuid.UUID
		rekeningID        = req.RekeningQRISID
		metodeID          = req.MetodePembayaranID
		qrisPayload       string
		namaMerchant      string
		nmid              string
		totalNominalDasar int64
		alokasiList       []kopdes.AlokasiReq
	)

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// Tentukan target AnggotaID: untuk peran anggota wajib murni dari identitas terautentikasi
		if claims.Role == "anggota" {
			err := tx.QueryRow(ctx, `
				SELECT id FROM anggota
				WHERE user_id = $1 AND koperasi_id = $2 AND deleted_at IS NULL
				LIMIT 1;
			`, claims.UserID, claims.KoperasiID).Scan(&targetAnggotaID)
			if err != nil {
				return errors.New("pengguna tidak terdaftar sebagai anggota aktif pada koperasi ini")
			}
		} else {
			// Admin / pengurus membayar atas nama anggota
			if req.AnggotaID != uuid.Nil {
				targetAnggotaID = req.AnggotaID
			} else if len(allTagihanIDs) > 0 {
				_ = tx.QueryRow(ctx, `
					SELECT anggota_id FROM tagihan_iuran WHERE id = $1 AND koperasi_id = $2
				`, allTagihanIDs[0], claims.KoperasiID).Scan(&targetAnggotaID)
			} else if req.PesananID != nil {
				_ = tx.QueryRow(ctx, `
					SELECT COALESCE(anggota_id, '00000000-0000-0000-0000-000000000000'::uuid) FROM pesanan WHERE id = $1 AND koperasi_id = $2
				`, *req.PesananID, claims.KoperasiID).Scan(&targetAnggotaID)
			}

			if targetAnggotaID == uuid.Nil {
				return errors.New("identitas anggota tujuan wajib ditentukan")
			}
		}
		// Validasi rekening QRIS
		if rekeningID == uuid.Nil {
			err := tx.QueryRow(ctx, `
				SELECT id, qris_payload_statis, nama_merchant, nmid
				FROM rekening_qris_koperasi
				WHERE koperasi_id = $1 AND status_aktif = true AND deleted_at IS NULL
				LIMIT 1;
			`, claims.KoperasiID).Scan(&rekeningID, &qrisPayload, &namaMerchant, &nmid)
			if err != nil {
				return fmt.Errorf("rekening QRIS aktif koperasi tidak ditemukan: %w", err)
			}
		} else {
			err := tx.QueryRow(ctx, `
				SELECT qris_payload_statis, nama_merchant, nmid
				FROM rekening_qris_koperasi
				WHERE id = $1 AND koperasi_id = $2 AND status_aktif = true AND deleted_at IS NULL;
			`, rekeningID, claims.KoperasiID).Scan(&qrisPayload, &namaMerchant, &nmid)
			if err != nil {
				return fmt.Errorf("rekening QRIS tidak valid: %w", err)
			}
		}

		// Validasi metode pembayaran
		if metodeID == uuid.Nil {
			err := tx.QueryRow(ctx, `
				SELECT id FROM metode_pembayaran
				WHERE koperasi_id = $1 AND butuh_kode_unik = true AND status_aktif = true AND deleted_at IS NULL
				LIMIT 1;
			`, claims.KoperasiID).Scan(&metodeID)
			if err != nil {
				return fmt.Errorf("metode pembayaran QRIS tidak ditemukan: %w", err)
			}
		}

		// Validasi tagihan iuran
		for _, tagihanID := range allTagihanIDs {
			var (
				ownerAnggotaID uuid.UUID
				sisaNominal    int64
				statusTagihan  string
			)
			err := tx.QueryRow(ctx, `
				SELECT anggota_id, (nominal_tagihan - nominal_terbayar)::bigint, status
				FROM tagihan_iuran
				WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL
				FOR UPDATE;
			`, tagihanID, claims.KoperasiID).Scan(&ownerAnggotaID, &sisaNominal, &statusTagihan)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("tagihan iuran %s tidak ditemukan", tagihanID)
				}
				return fmt.Errorf("gagal memeriksa tagihan iuran %s: %w", tagihanID, err)
			}

			if ownerAnggotaID != targetAnggotaID {
				return fmt.Errorf("tagihan iuran %s bukan milik anggota yang bersangkutan", tagihanID)
			}

			if statusTagihan == "lunas" || sisaNominal <= 0 {
				return fmt.Errorf("tagihan iuran %s sudah lunas", tagihanID)
			}

			// Cek apakah ada pembayaran aktif untuk tagihan ini
			var hasActivePayment bool
			err = tx.QueryRow(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM pembayaran_alokasi pa
					JOIN pembayaran p ON p.id = pa.pembayaran_id
					WHERE pa.tagihan_iuran_id = $1
					  AND pa.koperasi_id = $2
					  AND p.status IN ('menunggu_pembayaran', 'menunggu_verifikasi')
					  AND p.berlaku_sampai > NOW()
					  AND p.deleted_at IS NULL
				);
			`, tagihanID, claims.KoperasiID).Scan(&hasActivePayment)
			if err != nil {
				return fmt.Errorf("gagal memeriksa status pembayaran tagihan: %w", err)
			}
			if hasActivePayment {
				return fmt.Errorf("tagihan iuran %s sedang memiliki pembayaran aktif", tagihanID)
			}

			tID := tagihanID
			alokasiList = append(alokasiList, kopdes.AlokasiReq{
				JenisAlokasi:   "tagihan_iuran",
				TagihanIuranID: &tID,
				NominalAlokasi: sisaNominal,
				UrutanAlokasi:  len(alokasiList) + 1,
			})
			totalNominalDasar += sisaNominal
		}

		// Validasi pesanan jika ada
		if req.PesananID != nil {
			var (
				ownerAnggotaID uuid.UUID
				pembeliUserID  uuid.UUID
				totalAkhir     int64
				statusPesanan  string
			)
			err := tx.QueryRow(ctx, `
				SELECT COALESCE(anggota_id, '00000000-0000-0000-0000-000000000000'::uuid), pembeli_user_id, total_akhir::bigint, status
				FROM pesanan
				WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL
				FOR UPDATE;
			`, *req.PesananID, claims.KoperasiID).Scan(&ownerAnggotaID, &pembeliUserID, &totalAkhir, &statusPesanan)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("pesanan %s tidak ditemukan", *req.PesananID)
				}
				return fmt.Errorf("gagal memeriksa pesanan: %w", err)
			}

			if claims.Role == "anggota" && ownerAnggotaID != targetAnggotaID && pembeliUserID != claims.UserID {
				return fmt.Errorf("pesanan %s bukan milik anggota yang bersangkutan", *req.PesananID)
			}

			if statusPesanan != "draft" && statusPesanan != "menunggu_pembayaran" {
				return fmt.Errorf("pesanan %s tidak dalam status menunggu pembayaran", *req.PesananID)
			}

			var hasActivePayment bool
			err = tx.QueryRow(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM pembayaran_alokasi pa
					JOIN pembayaran p ON p.id = pa.pembayaran_id
					WHERE pa.pesanan_id = $1
					  AND pa.koperasi_id = $2
					  AND p.status IN ('menunggu_pembayaran', 'menunggu_verifikasi')
					  AND p.berlaku_sampai > NOW()
					  AND p.deleted_at IS NULL
				);
			`, *req.PesananID, claims.KoperasiID).Scan(&hasActivePayment)
			if err != nil {
				return fmt.Errorf("gagal memeriksa status pembayaran pesanan: %w", err)
			}
			if hasActivePayment {
				return fmt.Errorf("pesanan %s sedang memiliki pembayaran aktif", *req.PesananID)
			}

			alokasiList = append(alokasiList, kopdes.AlokasiReq{
				JenisAlokasi:   "pesanan",
				PesananID:      req.PesananID,
				NominalAlokasi: totalAkhir,
				UrutanAlokasi:  len(alokasiList) + 1,
			})
			totalNominalDasar += totalAkhir
		}

		if totalNominalDasar <= 0 {
			return errors.New("total nominal dasar tagihan/pesanan wajib lebih besar dari 0")
		}

		return nil
	})

	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	noPembayaran := fmt.Sprintf("PAY-%s-%04d", uuid.New().String()[:8], time.Now().UnixNano()%10000)
	serviceReq := kopdes.BuatPembayaranQRISReq{
		KoperasiID:         claims.KoperasiID,
		PembayarUserID:     claims.UserID,
		AnggotaID:          targetAnggotaID,
		MetodePembayaranID: metodeID,
		RekeningQRISID:     rekeningID,
		JenisTransaksi:     req.JenisTransaksi,
		NoPembayaran:       noPembayaran,
		NominalDasarRp:     totalNominalDasar,
		TTL:                1 * time.Hour,
		Alokasi:            alokasiList,
	}

	res, err := kopdes.BuatPembayaranQRISStatis(ctx, h.pool, h.qrisSecret, serviceReq)
	if err != nil {
		if errors.Is(err, kopdes.ErrKodeUnikPenuh) {
			response.Conflict(w, "Seluruh slot kode unik QRIS untuk nominal ini sedang penuh. Coba beberapa saat lagi.")
			return
		}
		response.InternalServerError(w, fmt.Sprintf("Gagal mengalokasikan QRIS: %v", err))
		return
	}

	// Catat audit log jika pembayaran dibuat oleh admin atas nama anggota
	if claims.Role != "anggota" {
		auditData, _ := json.Marshal(map[string]interface{}{
			"aksi_admin":             "bayar_atas_nama_anggota",
			"admin_user_id":          claims.UserID.String(),
			"target_anggota_id":      targetAnggotaID.String(),
			"nominal_dasar":          totalNominalDasar,
			"alokasi_count":          len(alokasiList),
		})
		_ = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO audit_log (
					koperasi_id, user_id, nama_tabel, record_id, aksi,
					data_baru, created_by
				) VALUES ($1, $2, 'pembayaran', $3, 'INSERT', $4::jsonb, $2);
			`, claims.KoperasiID, claims.UserID, res.PembayaranID, auditData)
			return err
		})
	}

	sisaDetik := int64(time.Until(res.BerlakuSampai).Seconds())
	if sisaDetik < 0 {
		sisaDetik = 0
	}

	// Generate QRIS Dinamis hanya jika feature flag QRIS_DYNAMIC_ENABLED bernilai true (S2)
	finalPayload := qrisPayload
	dynamicPayload := ""
	isAutoLock := false

	if h.dynamicQRISEnabled {
		dynPayload, dynErr := qris.GenerateDynamicQRIS(qrisPayload, res.NominalTotalRp, noPembayaran)
		if dynErr == nil {
			dynamicPayload = dynPayload
			finalPayload = dynamicPayload
			isAutoLock = true
		}
	}

	resObj := BuatQRISResponse{
		PembayaranID:       res.PembayaranID,
		NoPembayaran:       noPembayaran,
		KodeUnik:           res.KodeUnikTerpilih,
		NominalDasarRp:     totalNominalDasar,
		NominalTotalRp:     res.NominalTotalRp,
		QRISPayload:        finalPayload,
		QRISPayloadDinamis: dynamicPayload,
		QRISPayloadStatis:  qrisPayload,
		IsAutoLockNominal:  isAutoLock,
		NamaMerchant:       namaMerchant,
		NMID:               nmid,
		BerlakuSampai:      res.BerlakuSampai,
		SisaWaktuDetik:     sisaDetik,
	}

	if idempotencyKey != "" {
		respBytes, _ := json.Marshal(map[string]interface{}{
			"success": true,
			"data":    resObj,
		})
		_ = db.SaveIdempotency(r.Context(), h.pool, claims.KoperasiID, claims.UserID, idempotencyKey, reqHash, http.StatusCreated, respBytes, 24*time.Hour)
	}

	response.JSON(w, http.StatusCreated, resObj)
}

// HandleGetPembayaranStatus memeriksa status terkini pembayaran QRIS
func (h *QRISHandler) HandleGetPembayaranStatus(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	pembayaranID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID pembayaran tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var (
		status         string
		noPembayaran   string
		nominalTotal   string
		berlakuSampai  time.Time
		referensiRRN   *string
	)

	// Roles pengurus/admin/pengawas yang berhak melihat semua pembayaran di koperasinya
	privilegedRoles := map[string]bool{
		"admin":      true,
		"pengurus":   true,
		"bendahara":  true,
		"ketua":      true,
		"sekretaris": true,
		"operator":   true,
		"pengawas":   true,
	}
	isPrivileged := claims.IsPlatformAdmin || privilegedRoles[claims.Role]

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		if isPrivileged {
			return tx.QueryRow(ctx, `
				SELECT status, no_pembayaran, nominal_total::text, berlaku_sampai, referensi_rrn
				FROM pembayaran
				WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL;
			`, pembayaranID, claims.KoperasiID).Scan(&status, &noPembayaran, &nominalTotal, &berlakuSampai, &referensiRRN)
		}

		// Anggota biasa: hanya boleh melihat data pembayaran miliknya sendiri (anti-IDOR OWASP API1:2023)
		return tx.QueryRow(ctx, `
			SELECT p.status, p.no_pembayaran, p.nominal_total::text, p.berlaku_sampai, p.referensi_rrn
			FROM pembayaran p
			WHERE p.id = $1 
			  AND p.koperasi_id = $2 
			  AND (p.pembayar_user_id = $3 OR p.anggota_id IN (SELECT a.id FROM anggota a WHERE a.user_id = $3))
			  AND p.deleted_at IS NULL;
		`, pembayaranID, claims.KoperasiID, claims.UserID).Scan(&status, &noPembayaran, &nominalTotal, &berlakuSampai, &referensiRRN)
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, "Pembayaran tidak ditemukan")
			return
		}
		response.InternalServerError(w, "Gagal mengambil data pembayaran")
		return
	}

	sisaDetik := int64(time.Until(berlakuSampai).Seconds())
	if sisaDetik < 0 {
		sisaDetik = 0
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"pembayaran_id":    pembayaranID,
		"no_pembayaran":    noPembayaran,
		"status":           status,
		"nominal_total":    nominalTotal,
		"berlaku_sampai":   berlakuSampai.Format(time.RFC3339),
		"sisa_waktu_detik": sisaDetik,
		"referensi_rrn":    referensiRRN,
	})
}

// HandleListAntreanVerifikasi mengambil antrean pembayaran QRIS yang menunggu verifikasi (Operator/Pengurus)
func (h *QRISHandler) HandleListAntreanVerifikasi(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var hasil []db.ListAntreanVerifikasiQRISRow

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		queries := db.New(tx)
		kopUUID := pgtype.UUID{Bytes: claims.KoperasiID, Valid: true}
		rows, err := queries.ListAntreanVerifikasiQRIS(ctx, db.ListAntreanVerifikasiQRISParams{
			KoperasiID: kopUUID,
			Limit:      50,
			Offset:     0,
		})
		if err != nil {
			return err
		}
		hasil = rows
		return nil
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal mengambil antrean verifikasi: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, hasil, map[string]interface{}{
		"total": len(hasil),
	})
}

type VerifikasiRequest struct {
	Disetujui     bool   `json:"disetujui"`
	NominalAktual int64  `json:"nominal_aktual,omitempty"`
	AlasanTolak   string `json:"alasan_tolak,omitempty"`
	ReferensiRRN  string `json:"referensi_rrn,omitempty"`
	Catatan       string `json:"catatan,omitempty"`
}

type KlaimBuktiRequest struct {
	ReferensiRRN string `json:"referensi_rrn,omitempty"`
	Catatan      string `json:"catatan,omitempty"`
}

// HandleKlaimBuktiPembayaran memindahkan status pembayaran dari menunggu_pembayaran ke menunggu_verifikasi
func (h *QRISHandler) HandleKlaimBuktiPembayaran(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	pembayaranID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID pembayaran tidak valid")
		return
	}

	var req KlaimBuktiRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			SELECT fn_klaim_bukti_pembayaran($1, $2, $3, $4);
		`, pembayaranID, claims.UserID, req.ReferensiRRN, req.Catatan)
		return err
	})

	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") {
			response.NotFound(w, "Pembayaran tidak ditemukan")
			return
		}
		if strings.Contains(err.Error(), "tidak dalam status menunggu pembayaran") {
			response.BadRequest(w, "Pembayaran sudah diklaim atau telah diverifikasi sebelumnya")
			return
		}
		response.BadRequest(w, "Gagal memproses klaim pembayaran")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"pembayaran_id": pembayaranID,
		"status":        "menunggu_verifikasi",
		"pesan":         "Bukti pembayaran berhasil diajukan dan masuk antrean verifikasi pengurus.",
	})
}

// HandleVerifikasiPembayaran memvalidasi maker-checker (Aturan 4) dan mengeksekusi persetujuan atomik
func (h *QRISHandler) HandleVerifikasiPembayaran(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	pembayaranID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID pembayaran tidak valid")
		return
	}

	var req VerifikasiRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if !req.Disetujui && strings.TrimSpace(req.AlasanTolak) == "" {
		response.BadRequest(w, "Alasan penolakan wajib disertakan jika pembayaran ditolak")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var isFirstApprovalOfDual bool

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// 1. Kunci baris pembayaran untuk verifikasi konsistensi
		var pembayarUserID uuid.UUID
		var currentStatus string
		var nominalTotal string
		var persetujuanPertamaOleh *uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT pembayar_user_id, status, nominal_total::text, persetujuan_pertama_oleh
			FROM pembayaran
			WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL
			FOR UPDATE;
		`, pembayaranID, claims.KoperasiID).Scan(&pembayarUserID, &currentStatus, &nominalTotal, &persetujuanPertamaOleh)
		if err != nil {
			return err
		}

		// Aturan Maker-Checker: Verifikator dilarang sama dengan pembuat transaksi
		if claims.UserID == pembayarUserID {
			return errors.New("MAKER_CHECKER_VIOLATION: Pembuat transaksi dilarang memverifikasi pembayarannya sendiri")
		}

		if currentStatus != "menunggu_verifikasi" && currentStatus != "menunggu_persetujuan_kedua" {
			return fmt.Errorf("transaksi tidak dalam status menunggu verifikasi (status saat ini: %s)", currentStatus)
		}

		if req.Disetujui {
			// Periksa ambang batas persetujuan ganda dari konfigurasi_aturan
			var ambangGanda int64 = 5000000
			_ = tx.QueryRow(ctx, `
				SELECT COALESCE((nilai_json->>'nominal')::bigint, 5000000)
				FROM konfigurasi_aturan
				WHERE (koperasi_id = $1 OR koperasi_id IS NULL)
				  AND kunci_aturan = 'ambang_persetujuan_ganda_rp'
				  AND status_aktif = true
				  AND deleted_at IS NULL
				ORDER BY koperasi_id NULLS LAST, versi DESC
				LIMIT 1;
			`, claims.KoperasiID).Scan(&ambangGanda)

			nomTotalFloat, _ := strconv.ParseFloat(nominalTotal, 64)
			nomTotalInt := int64(nomTotalFloat)

			if nomTotalInt > ambangGanda {
				// Transaksi di atas ambang memerlukan 2 pengurus berbeda
				if persetujuanPertamaOleh == nil {
					// Persetujuan Pertama
					_, err = tx.Exec(ctx, `
						UPDATE pembayaran
						SET status = 'menunggu_persetujuan_kedua',
						    persetujuan_pertama_oleh = $1,
						    waktu_persetujuan_pertama = NOW(),
						    updated_at = NOW()
						WHERE id = $2 AND koperasi_id = $3;
					`, claims.UserID, pembayaranID, claims.KoperasiID)
					if err != nil {
						return err
					}

					_, err = tx.Exec(ctx, `
						INSERT INTO pembayaran_verifikasi_log (
							koperasi_id, pembayaran_id, status_lama, status_baru, aksi, catatan, created_by
						) VALUES (
							$1, $2, 'menunggu_verifikasi', 'menunggu_persetujuan_kedua', 'setujui', 'Persetujuan pertama dicatat. Menunggu persetujuan kedua karena nominal > ambang.', $3
						);
					`, claims.KoperasiID, pembayaranID, claims.UserID)
					isFirstApprovalOfDual = true
					return err
				}

				// Persetujuan Kedua
				if *persetujuanPertamaOleh == claims.UserID {
					return errors.New("DUAL_APPROVAL_VIOLATION: Persetujuan kedua wajib dilakukan oleh pengurus/pejabat yang berbeda dari penyetuju pertama")
				}

				_, err = tx.Exec(ctx, `
					UPDATE pembayaran
					SET persetujuan_kedua_oleh = $1,
					    waktu_persetujuan_kedua = NOW(),
					    updated_at = NOW()
					WHERE id = $2 AND koperasi_id = $3;
				`, claims.UserID, pembayaranID, claims.KoperasiID)
				if err != nil {
					return err
				}
			}

			// Cari atau buka periode kas aktif untuk koperasi ini
			var periodeKasID uuid.UUID
			err := tx.QueryRow(ctx, `
				SELECT id FROM periode_kas
				WHERE koperasi_id = $1 AND status = 'buka'
				ORDER BY created_at DESC LIMIT 1;
			`, claims.KoperasiID).Scan(&periodeKasID)

			if err != nil {
				// Otomatis buka periode buku kas jika belum tersedia
				err = tx.QueryRow(ctx, `
					INSERT INTO periode_kas (koperasi_id, periode, saldo_awal, status, dibuka_oleh)
					VALUES ($1, to_char(NOW(), 'YYYY-MM'), 0, 'buka', $2)
					ON CONFLICT (koperasi_id, periode) DO UPDATE SET updated_at = NOW()
					RETURNING id;
				`, claims.KoperasiID, claims.UserID).Scan(&periodeKasID)
				if err != nil {
					return fmt.Errorf("gagal inisialisasi buku kas: %w", err)
				}
			}

			nominalBayar := nominalTotal
			if req.NominalAktual > 0 {
				nominalBayar = fmt.Sprintf("%d", req.NominalAktual)
			}

			// Panggil prosedur atomik persetujuan (memperbarui pembayaran, tagihan, kas_mutasi, dan outbox)
			_, err = tx.Exec(ctx, `
				SELECT fn_setujui_pembayaran_qris($1, $2, $3::numeric, $4, $5);
			`, pembayaranID, claims.UserID, nominalBayar, periodeKasID, req.Catatan)
			return err
		}

		// Alur Penolakan Pembayaran
		_, err = tx.Exec(ctx, `
			UPDATE pembayaran
			SET status = 'ditolak',
			    alasan_ditolak = $1,
			    diverifikasi_oleh = $2,
			    waktu_verifikasi = NOW(),
			    updated_at = NOW()
			WHERE id = $3 AND koperasi_id = $4;
		`, req.AlasanTolak, claims.UserID, pembayaranID, claims.KoperasiID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO pembayaran_verifikasi_log (
				koperasi_id, pembayaran_id, status_lama, status_baru, aksi, catatan, created_by
			) VALUES (
				$1, $2, 'menunggu_verifikasi', 'ditolak', 'tolak', $3, $4
			);
		`, claims.KoperasiID, pembayaranID, req.AlasanTolak, claims.UserID)
		return err
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, "Pembayaran tidak ditemukan")
			return
		}
		if strings.Contains(err.Error(), "MAKER_CHECKER_VIOLATION") {
			response.Forbidden(w, "Pelanggaran Maker-Checker: Pembuat pembayaran dilarang memverifikasi pembayarannya sendiri.")
			return
		}
		if strings.Contains(err.Error(), "DUAL_APPROVAL_VIOLATION") {
			response.BadRequest(w, "DUAL_APPROVAL_VIOLATION: Persetujuan kedua wajib dilakukan oleh pengurus/pejabat yang berbeda dari penyetuju pertama.")
			return
		}
		if strings.Contains(err.Error(), "tidak dalam status menunggu verifikasi") {
			response.BadRequest(w, "Pembayaran belum mengajukan bukti klaim atau statusnya tidak valid untuk diverifikasi.")
			return
		}
		if strings.Contains(err.Error(), "kurang dari total pembayaran") {
			response.BadRequest(w, "Nominal pembayaran aktual kurang dari total tagihan.")
			return
		}
		response.BadRequest(w, fmt.Sprintf("Gagal memproses verifikasi pembayaran: %v", err))
		return
	}

	if isFirstApprovalOfDual {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"pembayaran_id": pembayaranID,
			"status":        "menunggu_persetujuan_kedua",
			"pesan":         "Persetujuan pertama berhasil dicatat. Menunggu persetujuan kedua oleh pengurus lain sebelum saldo dibukukan.",
		})
		return
	}

	statusPesan := "Pembayaran berhasil disetujui dan dicatat ke mutasi kas."
	if !req.Disetujui {
		statusPesan = "Pembayaran telah ditolak."
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"pembayaran_id": pembayaranID,
		"disetujui":     req.Disetujui,
		"pesan":         statusPesan,
	})
}

type AdminDaftarRekeningQRISRequest struct {
	NamaMerchant         string `json:"nama_merchant"`
	NMID                 string `json:"nmid"`
	NamaBankPJP          string `json:"nama_bank_pjp"`
	NoRekeningSettlement string `json:"no_rekening_settlement"`
	NamaPemilikRekening  string `json:"nama_pemilik_rekening"`
	IsAtasNamaBadanHukum bool   `json:"is_atas_nama_badan_hukum"`
	JenisPemilik         string `json:"jenis_pemilik,omitempty"` // 'badan_hukum_koperasi' | 'perorangan_pengurus'
	QRISPayloadStatis    string `json:"qris_payload_statis"`
}

type RekeningQRISItem struct {
	ID                   uuid.UUID `json:"id"`
	NamaMerchant         string    `json:"nama_merchant"`
	NMID                 string    `json:"nmid"`
	NamaBankPJP          string    `json:"nama_bank_pjp"`
	NoRekeningSettlement string    `json:"no_rekening_settlement"`
	NamaPemilikRekening  string    `json:"nama_pemilik_rekening"`
	JenisPemilik         string    `json:"jenis_pemilik"`
	QRISPayloadStatis    string    `json:"qris_payload_statis"`
	StatusAktif          bool      `json:"status_aktif"`
	CreatedAt            time.Time `json:"created_at"`
}

// HandleAdminDaftarRekeningQRIS mendaftarkan QRIS Statis resmi badan hukum koperasi
func (h *QRISHandler) HandleAdminDaftarRekeningQRIS(w http.ResponseWriter, r *http.Request) {
	var req AdminDaftarRekeningQRISRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.NamaMerchant == "" {
		response.BadRequest(w, "Nama merchant QRIS wajib diisi")
		return
	}
	if req.NMID == "" {
		response.BadRequest(w, "NMID wajib diisi (contoh: ID1020304050607)")
		return
	}
	if req.NamaBankPJP == "" {
		response.BadRequest(w, "Nama bank PJP wajib diisi (misal: Bank Mandiri, BRI, BNI, BCA)")
		return
	}
	if req.NoRekeningSettlement == "" {
		response.BadRequest(w, "Nomor rekening settlement penampung dana wajib diisi")
		return
	}
	if req.NamaPemilikRekening == "" {
		response.BadRequest(w, "Nama pemilik rekening wajib diisi")
		return
	}

	// 1. Validasi integritas QRIS statis masukan (EMVCo & CRC16)
	if req.QRISPayloadStatis == "" {
		response.BadRequest(w, "Payload QRIS statis (string teks EMVCo) wajib diisi")
		return
	}
	if err := qris.ValidateQRIS(req.QRISPayloadStatis); err != nil {
		response.BadRequest(w, fmt.Sprintf("Payload QRIS statis tidak valid: %v", err))
		return
	}

	// 2. Evaluasi jenis kepemilikan rekening (S2)
	jenisPemilik := req.JenisPemilik
	if jenisPemilik == "" {
		if req.IsAtasNamaBadanHukum {
			jenisPemilik = "badan_hukum_koperasi"
		} else {
			jenisPemilik = "perorangan_pengurus"
		}
	}

	if jenisPemilik != "badan_hukum_koperasi" && jenisPemilik != "perorangan_pengurus" {
		response.BadRequest(w, "jenis_pemilik harus bernilai 'badan_hukum_koperasi' atau 'perorangan_pengurus'")
		return
	}

	// Penolakan rekening perorangan jika pilot mode tidak diaktifkan
	if jenisPemilik == "perorangan_pengurus" && !h.allowPersonalPaymentPilot {
		response.Forbidden(w, "Rekening QRIS perorangan pengurus dinonaktifkan dalam lingkungan produksi. Wajib menggunakan rekening atas nama badan hukum koperasi atau mengaktifkan ALLOW_PERSONAL_PAYMENT_PILOT.")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Konteks tenant koperasi tidak valid")
		return
	}

	ctx := r.Context()
	var rekeningID uuid.UUID
	var createdAt time.Time

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// 1. Pastikan record penyedia_pembayaran default ada
		var penyediaID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO penyedia_pembayaran (kode, nama, tipe_integrasi)
			VALUES ('qris_statis_manual', 'QRIS Statis Bank Indonesia', 'qris_statis_manual')
			ON CONFLICT (kode) DO UPDATE SET nama = EXCLUDED.nama
			RETURNING id;
		`).Scan(&penyediaID)
		if err != nil {
			return fmt.Errorf("gagal inisialisasi penyedia pembayaran: %w", err)
		}

		isBadanHukum := (jenisPemilik == "badan_hukum_koperasi")

		// 2. Simpan atau perbarui rekening QRIS koperasi
		err = tx.QueryRow(ctx, `
			INSERT INTO rekening_qris_koperasi (
				koperasi_id, penyedia_id, nama_merchant, nmid,
				nama_bank_pjp, no_rekening_settlement, nama_pemilik_rekening,
				is_atas_nama_badan_hukum, jenis_pemilik, qris_payload_statis, status_aktif, created_by
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, $7,
				$8, $9, $10, true, $11
			)
			ON CONFLICT (koperasi_id, nmid) DO UPDATE SET
				nama_merchant = EXCLUDED.nama_merchant,
				nama_bank_pjp = EXCLUDED.nama_bank_pjp,
				no_rekening_settlement = EXCLUDED.no_rekening_settlement,
				nama_pemilik_rekening = EXCLUDED.nama_pemilik_rekening,
				is_atas_nama_badan_hukum = EXCLUDED.is_atas_nama_badan_hukum,
				jenis_pemilik = EXCLUDED.jenis_pemilik,
				qris_payload_statis = EXCLUDED.qris_payload_statis,
				status_aktif = true,
				updated_at = NOW()
			RETURNING id, created_at;
		`,
			claims.KoperasiID, penyediaID, req.NamaMerchant, req.NMID,
			req.NamaBankPJP, req.NoRekeningSettlement,
			req.NamaPemilikRekening, isBadanHukum, jenisPemilik, req.QRISPayloadStatis,
			claims.UserID,
		).Scan(&rekeningID, &createdAt)
		if err != nil {
			return fmt.Errorf("gagal menyimpan rekening QRIS: %w", err)
		}

		// Catat audit log khusus bila menggunakan pilot perorangan pengurus
		if jenisPemilik == "perorangan_pengurus" {
			auditData, _ := json.Marshal(map[string]interface{}{
				"aksi":          "daftar_rekening_qris_pilot_perorangan",
				"rekening_id":   rekeningID.String(),
				"nmid":          req.NMID,
				"nama_pemilik":  req.NamaPemilikRekening,
				"catatan":       "Pendaftaran rekening QRIS perorangan pengurus diizinkan via ALLOW_PERSONAL_PAYMENT_PILOT",
			})
			_, _ = tx.Exec(ctx, `
				INSERT INTO audit_log (
					koperasi_id, user_id, nama_tabel, record_id, aksi,
					data_baru, created_by
				) VALUES ($1, $2, 'rekening_qris_koperasi', $3, 'INSERT', $4::jsonb, $2);
			`, claims.KoperasiID, claims.UserID, rekeningID, auditData)
		}

		// 3. Pastikan metode pembayaran 'qris_statis' aktif untuk koperasi ini
		_, err = tx.Exec(ctx, `
			INSERT INTO metode_pembayaran (
				koperasi_id, penyedia_id, kode, nama,
				butuh_kode_unik, butuh_verifikasi_manual, batas_waktu_menit, status_aktif
			) VALUES (
				$1, $2, 'qris_statis', 'QRIS Statis Badan Hukum',
				true, true, 60, true
			)
			ON CONFLICT (koperasi_id, kode) DO UPDATE SET status_aktif = true;
		`, claims.KoperasiID, penyediaID)
		if err != nil {
			return fmt.Errorf("gagal mengaktifkan metode pembayaran QRIS: %w", err)
		}

		return nil
	})

	if err != nil {
		response.InternalServerError(w, err.Error())
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"id":                      rekeningID,
		"nama_merchant":           req.NamaMerchant,
		"nmid":                    req.NMID,
		"nama_bank_pjp":           req.NamaBankPJP,
		"no_rekening_settlement":  req.NoRekeningSettlement,
		"nama_pemilik_rekening":   req.NamaPemilikRekening,
		"is_atas_nama_badan_hukum": true,
		"status_aktif":            true,
		"pesan":                   "Rekening QRIS statis badan hukum koperasi berhasil didaftarkan.",
		"created_at":              createdAt,
	})
}

// HandleAdminListRekeningQRIS menampilkan daftar rekening QRIS terdaftar milik koperasi
func (h *QRISHandler) HandleAdminListRekeningQRIS(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Konteks tenant koperasi tidak valid")
		return
	}

	ctx := r.Context()
	var list []RekeningQRISItem

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, nama_merchant, nmid, nama_bank_pjp, no_rekening_settlement,
			       nama_pemilik_rekening, qris_payload_statis, status_aktif, created_at
			FROM rekening_qris_koperasi
			WHERE koperasi_id = $1 AND deleted_at IS NULL
			ORDER BY created_at DESC;
		`, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var it RekeningQRISItem
			if err := rows.Scan(
				&it.ID, &it.NamaMerchant, &it.NMID, &it.NamaBankPJP,
				&it.NoRekeningSettlement, &it.NamaPemilikRekening,
				&it.QRISPayloadStatis, &it.StatusAktif, &it.CreatedAt,
			); err != nil {
				return err
			}
			list = append(list, it)
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal mengambil data rekening QRIS: %v", err))
		return
	}

	if list == nil {
		list = []RekeningQRISItem{}
	}

	response.JSON(w, http.StatusOK, list)
}

// HandleAdminDeactivateRekeningQRIS menonaktifkan konfigurasi QRIS koperasi
func (h *QRISHandler) HandleAdminDeactivateRekeningQRIS(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID rekening QRIS tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Konteks tenant koperasi tidak valid")
		return
	}

	ctx := r.Context()
	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE rekening_qris_koperasi
			SET status_aktif = false, updated_at = NOW()
			WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL;
		`, targetID, claims.KoperasiID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errors.New("rekening QRIS tidak ditemukan atau sudah non-aktif")
		}
		return nil
	})

	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{
		"pesan": "Rekening QRIS berhasil dinonaktifkan.",
	})
}

// detectAnomalies memeriksa indikator anomali transaksi (di luar jam kerja, hash bukti ganda, transaksi beruntun)
func (h *QRISHandler) detectAnomalies(ctx context.Context, tx pgx.Tx, pembayaranID, koperasiID, userID uuid.UUID, waktu time.Time) []string {
	var indikator []string

	// 1. Di luar jam operasional (22:00 - 05:00 WIB / UTC+7)
	locWIB := time.FixedZone("WIB", 7*3600)
	wibTime := waktu.In(locWIB)
	hour := wibTime.Hour()
	if hour < 5 || hour >= 22 {
		indikator = append(indikator, "di_luar_jam_kerja")
	}

	// 2. Hash bukti ganda (file_hash_sha256 bukti pembayaran sama pada pembayaran berbeda)
	var duplicateProofCount int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pembayaran_bukti pb1
		JOIN pembayaran_bukti pb2 ON pb1.file_hash_sha256 = pb2.file_hash_sha256
		WHERE pb1.pembayaran_id = $1 AND pb2.pembayaran_id <> $1;
	`, pembayaranID).Scan(&duplicateProofCount)
	if err == nil && duplicateProofCount > 0 {
		indikator = append(indikator, "hash_bukti_ganda")
	}

	// 3. Transaksi beruntun (lebih dari 3 transaksi dalam 5 menit terakhir untuk user yang sama)
	var burstCount int
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pembayaran
		WHERE pembayar_user_id = $1 AND koperasi_id = $2
		  AND created_at >= (clock_timestamp() - INTERVAL '5 minutes');
	`, userID, koperasiID).Scan(&burstCount)
	if err == nil && burstCount > 3 {
		indikator = append(indikator, "transaksi_beruntun")
	}

	return indikator
}

// HandleImporMutasiBank mengimpor rekening koran/settlement CSV dan mencocokkan otomatis nominal_total
func (h *QRISHandler) HandleImporMutasiBank(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	var reader io.Reader
	file, _, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		reader = file
	} else {
		reader = r.Body
	}

	csvReader := csv.NewReader(reader)
	records, err := csvReader.ReadAll()
	if err != nil {
		response.BadRequest(w, fmt.Sprintf("Format CSV tidak valid: %v", err))
		return
	}

	if len(records) < 2 {
		response.BadRequest(w, "File CSV kosong atau tidak memiliki baris data")
		return
	}

	ctx := r.Context()
	var totalBaris, tercocokkan, anomali, belumCocok int

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		var ambangGanda int64 = 5000000
		_ = tx.QueryRow(ctx, `
			SELECT COALESCE((nilai_json->>'nominal')::bigint, 5000000)
			FROM konfigurasi_aturan
			WHERE (koperasi_id = $1 OR koperasi_id IS NULL)
			  AND kunci_aturan = 'ambang_persetujuan_ganda_rp'
			  AND status_aktif = true
			  AND deleted_at IS NULL
			ORDER BY koperasi_id NULLS LAST, versi DESC
			LIMIT 1;
		`, claims.KoperasiID).Scan(&ambangGanda)

		var periodeKasID uuid.UUID
		_ = tx.QueryRow(ctx, `
			SELECT id FROM periode_kas
			WHERE koperasi_id = $1 AND status = 'buka'
			ORDER BY created_at DESC LIMIT 1;
		`, claims.KoperasiID).Scan(&periodeKasID)
		if periodeKasID == uuid.Nil {
			_ = tx.QueryRow(ctx, `
				INSERT INTO periode_kas (koperasi_id, periode, saldo_awal, status, dibuka_oleh)
				VALUES ($1, to_char(NOW(), 'YYYY-MM'), 0, 'buka', $2)
				ON CONFLICT (koperasi_id, periode) DO UPDATE SET updated_at = NOW()
				RETURNING id;
			`, claims.KoperasiID, claims.UserID).Scan(&periodeKasID)
		}

		for i, row := range records {
			if i == 0 {
				continue // skip header
			}
			if len(row) < 4 {
				continue
			}

			tglStr := strings.TrimSpace(row[0])
			keterangan := strings.TrimSpace(row[1])
			tipe := strings.ToUpper(strings.TrimSpace(row[2]))
			nomStr := strings.TrimSpace(row[3])
			noRek := ""
			if len(row) >= 5 {
				noRek = strings.TrimSpace(row[4])
			}

			nomStr = strings.ReplaceAll(nomStr, "Rp", "")
			nomStr = strings.ReplaceAll(nomStr, " ", "")
			nomStr = strings.ReplaceAll(nomStr, ",", "")

			nomFloat, err := strconv.ParseFloat(nomStr, 64)
			if err != nil || nomFloat <= 0 {
				continue
			}
			totalBaris++

			tglMutasi, err := time.Parse("2006-01-02 15:04:05", tglStr)
			if err != nil {
				tglMutasi, err = time.Parse(time.RFC3339, tglStr)
				if err != nil {
					tglMutasi, _ = time.Parse("2006-01-02", tglStr)
				}
			}
			if tglMutasi.IsZero() {
				tglMutasi = time.Now()
			}

			mutasiID := uuid.New()
			nomInt := int64(nomFloat)

			if tipe != "CR" {
				_, _ = tx.Exec(ctx, `
					INSERT INTO mutasi_bank_impor (
						id, koperasi_id, no_rekening, tanggal_mutasi, keterangan, tipe, nominal,
						status_rekonsiliasi, created_by
					) VALUES ($1, $2, $3, $4, $5, $6, $7, 'belum_cocok', $8);
				`, mutasiID, claims.KoperasiID, noRek, tglMutasi, keterangan, tipe, nomFloat, claims.UserID)
				belumCocok++
				continue
			}

			// Cari pembayaran yang cocok pada nominal_total
			var matchedPembayaranID uuid.UUID
			var pembayarUserID uuid.UUID
			var currentStatus string
			err = tx.QueryRow(ctx, `
				SELECT id, pembayar_user_id, status
				FROM pembayaran
				WHERE koperasi_id = $1
				  AND nominal_total = $2
				  AND status IN ('menunggu_verifikasi', 'menunggu_pembayaran')
				  AND deleted_at IS NULL
				ORDER BY created_at ASC
				LIMIT 1
				FOR UPDATE;
			`, claims.KoperasiID, nomFloat).Scan(&matchedPembayaranID, &pembayarUserID, &currentStatus)

			if err != nil {
				_, _ = tx.Exec(ctx, `
					INSERT INTO mutasi_bank_impor (
						id, koperasi_id, no_rekening, tanggal_mutasi, keterangan, tipe, nominal,
						status_rekonsiliasi, created_by
					) VALUES ($1, $2, $3, $4, $5, $6, $7, 'belum_cocok', $8);
				`, mutasiID, claims.KoperasiID, noRek, tglMutasi, keterangan, tipe, nomFloat, claims.UserID)
				belumCocok++
				continue
			}

			anomalies := h.detectAnomalies(ctx, tx, matchedPembayaranID, claims.KoperasiID, pembayarUserID, tglMutasi)

			if len(anomalies) > 0 {
				anomali++
				_, _ = tx.Exec(ctx, `
					UPDATE pembayaran
					SET is_anomali = true,
						indikator_anomali = $1,
						updated_at = NOW()
					WHERE id = $2 AND koperasi_id = $3;
				`, anomalies, matchedPembayaranID, claims.KoperasiID)

				_, _ = tx.Exec(ctx, `
					INSERT INTO mutasi_bank_impor (
						id, koperasi_id, no_rekening, tanggal_mutasi, keterangan, tipe, nominal,
						status_rekonsiliasi, pembayaran_id, catatan_rekonsiliasi, created_by
					) VALUES ($1, $2, $3, $4, $5, $6, $7, 'anomali', $8, $9, $10);
				`, mutasiID, claims.KoperasiID, noRek, tglMutasi, keterangan, tipe, nomFloat, matchedPembayaranID,
					fmt.Sprintf("Anomali terdeteksi: %s", strings.Join(anomalies, ", ")), claims.UserID)
				continue
			}

			if nomInt > ambangGanda {
				tercocokkan++
				_, _ = tx.Exec(ctx, `
					UPDATE pembayaran
					SET status = 'menunggu_persetujuan_kedua',
						persetujuan_pertama_oleh = $1,
						waktu_persetujuan_pertama = NOW(),
						updated_at = NOW()
					WHERE id = $2 AND koperasi_id = $3;
				`, claims.UserID, matchedPembayaranID, claims.KoperasiID)

				_, _ = tx.Exec(ctx, `
					INSERT INTO mutasi_bank_impor (
						id, koperasi_id, no_rekening, tanggal_mutasi, keterangan, tipe, nominal,
						status_rekonsiliasi, pembayaran_id, catatan_rekonsiliasi, created_by
					) VALUES ($1, $2, $3, $4, $5, $6, $7, 'tercocokkan', $8, 'Tercocokkan (Menunggu persetujuan ganda)', $9);
				`, mutasiID, claims.KoperasiID, noRek, tglMutasi, keterangan, tipe, nomFloat, matchedPembayaranID, claims.UserID)
				continue
			}

			// Auto approve pembayaran cocok
			tercocokkan++
			_, err = tx.Exec(ctx, `
				SELECT fn_setujui_pembayaran_qris($1, $2, $3::numeric, $4, 'Pencocokan otomatis mutasi bank');
			`, matchedPembayaranID, claims.UserID, nomFloat, periodeKasID)
			if err != nil {
				return fmt.Errorf("gagal auto-approve pembayaran %s: %w", matchedPembayaranID, err)
			}

			_, _ = tx.Exec(ctx, `
				INSERT INTO mutasi_bank_impor (
					id, koperasi_id, no_rekening, tanggal_mutasi, keterangan, tipe, nominal,
					status_rekonsiliasi, pembayaran_id, catatan_rekonsiliasi, created_by
				) VALUES ($1, $2, $3, $4, $5, $6, $7, 'tercocokkan', $8, 'Tercocokkan otomatis & disetujui sistem', $9);
			`, mutasiID, claims.KoperasiID, noRek, tglMutasi, keterangan, tipe, nomFloat, matchedPembayaranID, claims.UserID)
		}
		return nil
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal memproses impor mutasi bank: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"total_baris": totalBaris,
		"tercocokkan": tercocokkan,
		"anomali":     anomali,
		"belum_cocok": belumCocok,
		"pesan":       "Impor dan pencocokan otomatis mutasi bank selesai.",
	})
}

// HandleLaporanRekonsiliasiHarian menyajikan ringkasan rekonsiliasi harian mutasi bank dan pembukuan internal
func (h *QRISHandler) HandleLaporanRekonsiliasiHarian(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	tgl := strings.TrimSpace(r.URL.Query().Get("tanggal"))
	if tgl == "" {
		tgl = time.Now().Format("2006-01-02")
	}

	ctx := r.Context()
	var (
		totalPembayaranSistemCount int
		totalPembayaranSistemNom   float64
		totalMutasiBankCRCount     int
		totalMutasiBankCRNom       float64
		totalTercocokkanCount      int
		totalTercocokkanNom        float64
	)

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		_ = tx.QueryRow(ctx, `
			SELECT COUNT(*), COALESCE(SUM(nominal_total), 0)
			FROM pembayaran
			WHERE koperasi_id = $1 AND date_trunc('day', created_at) = $2::date AND status = 'disetujui' AND deleted_at IS NULL;
		`, claims.KoperasiID, tgl).Scan(&totalPembayaranSistemCount, &totalPembayaranSistemNom)

		_ = tx.QueryRow(ctx, `
			SELECT COUNT(*), COALESCE(SUM(nominal), 0)
			FROM mutasi_bank_impor
			WHERE koperasi_id = $1 AND date_trunc('day', tanggal_mutasi) = $2::date AND tipe = 'CR';
		`, claims.KoperasiID, tgl).Scan(&totalMutasiBankCRCount, &totalMutasiBankCRNom)

		_ = tx.QueryRow(ctx, `
			SELECT COUNT(*), COALESCE(SUM(nominal), 0)
			FROM mutasi_bank_impor
			WHERE koperasi_id = $1 AND date_trunc('day', tanggal_mutasi) = $2::date AND status_rekonsiliasi = 'tercocokkan';
		`, claims.KoperasiID, tgl).Scan(&totalTercocokkanCount, &totalTercocokkanNom)

		return nil
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal menyusun laporan rekonsiliasi: %v", err))
		return
	}

	selisih := totalMutasiBankCRNom - totalPembayaranSistemNom

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"tanggal": tgl,
		"total_pembayaran_sistem": map[string]interface{}{
			"jumlah":  totalPembayaranSistemCount,
			"nominal": totalPembayaranSistemNom,
		},
		"total_mutasi_bank_cr": map[string]interface{}{
			"jumlah":  totalMutasiBankCRCount,
			"nominal": totalMutasiBankCRNom,
		},
		"total_tercocokkan": map[string]interface{}{
			"jumlah":  totalTercocokkanCount,
			"nominal": totalTercocokkanNom,
		},
		"selisih_nominal": selisih,
	})
}
