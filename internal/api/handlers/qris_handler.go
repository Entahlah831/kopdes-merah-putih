package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

type QRISHandler struct {
	pool       *pgxpool.Pool
	qrisSecret []byte
}

func NewQRISHandler(pool *pgxpool.Pool, qrisSecret []byte) *QRISHandler {
	return &QRISHandler{
		pool:       pool,
		qrisSecret: qrisSecret,
	}
}

type BuatQRISRequest struct {
	NominalDasarRp     int64     `json:"nominal_dasar"`
	JenisTransaksi     string    `json:"jenis_transaksi"`
	AnggotaID          uuid.UUID `json:"anggota_id,omitempty"`
	MetodePembayaranID uuid.UUID `json:"metode_pembayaran_id,omitempty"`
	RekeningQRISID     uuid.UUID `json:"rekening_qris_id,omitempty"`
}

type BuatQRISResponse struct {
	PembayaranID     uuid.UUID `json:"pembayaran_id"`
	NoPembayaran     string    `json:"no_pembayaran"`
	KodeUnik         int       `json:"kode_unik"`
	NominalDasarRp   int64     `json:"nominal_dasar"`
	NominalTotalRp   int64     `json:"nominal_total"`
	QRISPayload      string    `json:"qris_payload"`
	NamaMerchant     string    `json:"nama_merchant"`
	NMID             string    `json:"nmid"`
	BerlakuSampai    time.Time `json:"berlaku_sampai"`
	SisaWaktuDetik   int64     `json:"sisa_waktu_detik"`
}

// HandleBuatQRIS menghasilkan alokasi transaksi QRIS statis 3-digit anti-tabrakan
func (h *QRISHandler) HandleBuatQRIS(w http.ResponseWriter, r *http.Request) {
	var req BuatQRISRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.NominalDasarRp <= 0 {
		response.BadRequest(w, "Nominal dasar wajib lebih besar dari 0")
		return
	}

	if req.JenisTransaksi == "" {
		req.JenisTransaksi = "iuran_bulanan"
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Konteks tenant koperasi tidak valid")
		return
	}

	ctx := r.Context()

	// 1. Cari default rekening QRIS dan metode pembayaran jika tidak disertakan dalam request
	var (
		rekeningID     = req.RekeningQRISID
		metodeID       = req.MetodePembayaranID
		qrisPayload    string
		namaMerchant   string
		nmid           string
	)

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
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
		AnggotaID:          req.AnggotaID,
		MetodePembayaranID: metodeID,
		RekeningQRISID:     rekeningID,
		JenisTransaksi:     req.JenisTransaksi,
		NoPembayaran:       noPembayaran,
		NominalDasarRp:     req.NominalDasarRp,
		TTL:                1 * time.Hour,
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

	sisaDetik := int64(time.Until(res.BerlakuSampai).Seconds())
	if sisaDetik < 0 {
		sisaDetik = 0
	}

	response.JSON(w, http.StatusCreated, BuatQRISResponse{
		PembayaranID:   res.PembayaranID,
		NoPembayaran:   noPembayaran,
		KodeUnik:       res.KodeUnikTerpilih,
		NominalDasarRp: req.NominalDasarRp,
		NominalTotalRp: res.NominalTotalRp,
		QRISPayload:    qrisPayload,
		NamaMerchant:   namaMerchant,
		NMID:           nmid,
		BerlakuSampai:  res.BerlakuSampai,
		SisaWaktuDetik: sisaDetik,
	})
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

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT status, no_pembayaran, nominal_total::text, berlaku_sampai, referensi_rrn
			FROM pembayaran
			WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL;
		`, pembayaranID, claims.KoperasiID).Scan(&status, &noPembayaran, &nominalTotal, &berlakuSampai, &referensiRRN)
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
	AlasanTolak   string `json:"alasan_tolak,omitempty"`
	ReferensiRRN  string `json:"referensi_rrn,omitempty"`
	Catatan       string `json:"catatan,omitempty"`
}

// HandleVerifikasiPembayaran memvalidasi maker-checker (Aturan 4) dan memperbarui status pembayaran
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

	if !req.Disetujui && req.AlasanTolak == "" {
		response.BadRequest(w, "Alasan penolakan wajib disertakan jika pembayaran ditolak")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// 1. Ambil data pembayar untuk cek Maker-Checker
		var pembayarUserID uuid.UUID
		var currentStatus string
		err := tx.QueryRow(ctx, `
			SELECT pembayar_user_id, status
			FROM pembayaran
			WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL;
		`, pembayaranID, claims.KoperasiID).Scan(&pembayarUserID, &currentStatus)
		if err != nil {
			return err
		}

		// Aturan 4: Maker-Checker — Verifikator tidak boleh sama dengan pembuat pembayaran
		if claims.UserID == pembayarUserID {
			return errors.New("MAKER_CHECKER_VIOLATION: Pembuat transaksi dilarang memverifikasi pembayarannya sendiri")
		}

		if currentStatus != "menunggu_pembayaran" && currentStatus != "menunggu_verifikasi" {
			return fmt.Errorf("transaksi tidak dalam status antrean verifikasi (status saat ini: %s)", currentStatus)
		}

		statusBaru := "disetujui"
		if !req.Disetujui {
			statusBaru = "ditolak"
		}

		// 2. Update status pembayaran
		_, err = tx.Exec(ctx, `
			UPDATE pembayaran
			SET status = $1,
			    diverifikasi_oleh = $2,
			    waktu_verifikasi = NOW(),
			    alasan_ditolak = $3,
			    referensi_rrn = COALESCE($4, referensi_rrn),
			    updated_at = NOW()
			WHERE id = $5 AND koperasi_id = $6;
		`, statusBaru, claims.UserID, req.AlasanTolak, req.ReferensiRRN, pembayaranID, claims.KoperasiID)
		if err != nil {
			return err
		}

		// 3. Catat ke tabel append-only pembayaran_verifikasi_log (Aturan 3)
		_, err = tx.Exec(ctx, `
			INSERT INTO pembayaran_verifikasi_log (
				koperasi_id, pembayaran_id, verifikator_id,
				keputusan, alasan, catatan
			) VALUES (
				$1, $2, $3, $4, $5, $6
			);
		`, claims.KoperasiID, pembayaranID, claims.UserID, statusBaru, req.AlasanTolak, req.Catatan)
		return err
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, "Pembayaran tidak ditemukan")
			return
		}
		if err.Error() == "MAKER_CHECKER_VIOLATION: Pembuat transaksi dilarang memverifikasi pembayarannya sendiri" {
			response.Forbidden(w, "Pelanggaran Maker-Checker: Pembuat pembayaran dilarang memverifikasi pembayarannya sendiri.")
			return
		}
		response.BadRequest(w, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"pembayaran_id": pembayaranID,
		"status":        "berhasil_diverifikasi",
		"pesan":         "Verifikasi pembayaran berhasil dicatat ke jurnal append-only.",
	})
}
