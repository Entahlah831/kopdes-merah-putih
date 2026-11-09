package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

	// Generate QRIS Dinamis dengan Tag 54 Auto-Lock Nominal & Tag 62 Nomor Invoice
	dynamicPayload, dynErr := qris.GenerateDynamicQRIS(qrisPayload, res.NominalTotalRp, noPembayaran)
	finalPayload := qrisPayload
	isAutoLock := false
	if dynErr == nil {
		finalPayload = dynamicPayload
		isAutoLock = true
	}

	response.JSON(w, http.StatusCreated, BuatQRISResponse{
		PembayaranID:       res.PembayaranID,
		NoPembayaran:       noPembayaran,
		KodeUnik:           res.KodeUnikTerpilih,
		NominalDasarRp:     req.NominalDasarRp,
		NominalTotalRp:     res.NominalTotalRp,
		QRISPayload:        finalPayload,
		QRISPayloadDinamis: dynamicPayload,
		QRISPayloadStatis:  qrisPayload,
		IsAutoLockNominal:  isAutoLock,
		NamaMerchant:       namaMerchant,
		NMID:               nmid,
		BerlakuSampai:      res.BerlakuSampai,
		SisaWaktuDetik:     sisaDetik,
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

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// 1. Kunci baris pembayaran untuk verifikasi konsistensi
		var pembayarUserID uuid.UUID
		var currentStatus string
		var nominalTotal string
		err := tx.QueryRow(ctx, `
			SELECT pembayar_user_id, status, nominal_total::text
			FROM pembayaran
			WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL
			FOR UPDATE;
		`, pembayaranID, claims.KoperasiID).Scan(&pembayarUserID, &currentStatus, &nominalTotal)
		if err != nil {
			return err
		}

		// Aturan Maker-Checker: Verifikator dilarang sama dengan pembuat transaksi
		if claims.UserID == pembayarUserID {
			return errors.New("MAKER_CHECKER_VIOLATION: Pembuat transaksi dilarang memverifikasi pembayarannya sendiri")
		}

		if currentStatus != "menunggu_verifikasi" {
			return fmt.Errorf("transaksi tidak dalam status menunggu verifikasi (status saat ini: %s)", currentStatus)
		}

		if req.Disetujui {
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
		if strings.Contains(err.Error(), "tidak dalam status menunggu verifikasi") {
			response.BadRequest(w, "Pembayaran belum mengajukan bukti klaim atau statusnya tidak valid untuk diverifikasi.")
			return
		}
		if strings.Contains(err.Error(), "kurang dari total pembayaran") {
			response.BadRequest(w, "Nominal pembayaran aktual kurang dari total tagihan.")
			return
		}
		response.BadRequest(w, "Gagal memproses verifikasi pembayaran")
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
	QRISPayloadStatis    string `json:"qris_payload_statis"`
}

type RekeningQRISItem struct {
	ID                   uuid.UUID `json:"id"`
	NamaMerchant         string    `json:"nama_merchant"`
	NMID                 string    `json:"nmid"`
	NamaBankPJP          string    `json:"nama_bank_pjp"`
	NoRekeningSettlement string    `json:"no_rekening_settlement"`
	NamaPemilikRekening  string    `json:"nama_pemilik_rekening"`
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
	if !req.IsAtasNamaBadanHukum {
		response.BadRequest(w, "Sesuai regulasi PADG BI 21/18/PADG/2019, rekening merchant QRIS wajib atas nama badan hukum koperasi")
		return
	}
	if req.QRISPayloadStatis == "" {
		response.BadRequest(w, "Payload QRIS statis (string teks EMVCo) wajib diisi")
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

		// 2. Simpan atau perbarui rekening QRIS koperasi
		err = tx.QueryRow(ctx, `
			INSERT INTO rekening_qris_koperasi (
				koperasi_id, penyedia_id, nama_merchant, nmid,
				nama_bank_pjp, no_rekening_settlement, nama_pemilik_rekening,
				is_atas_nama_badan_hukum, qris_payload_statis, status_aktif, created_by
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, $7,
				true, $8, true, $9
			)
			ON CONFLICT (koperasi_id, nmid) DO UPDATE SET
				nama_merchant = EXCLUDED.nama_merchant,
				nama_bank_pjp = EXCLUDED.nama_bank_pjp,
				no_rekening_settlement = EXCLUDED.no_rekening_settlement,
				nama_pemilik_rekening = EXCLUDED.nama_pemilik_rekening,
				qris_payload_statis = EXCLUDED.qris_payload_statis,
				status_aktif = true,
				updated_at = NOW()
			RETURNING id, created_at;
		`,
			claims.KoperasiID, penyediaID, req.NamaMerchant, req.NMID,
			req.NamaBankPJP, req.NoRekeningSettlement,
			req.NamaPemilikRekening, req.QRISPayloadStatis,
			claims.UserID,
		).Scan(&rekeningID, &createdAt)
		if err != nil {
			return fmt.Errorf("gagal menyimpan rekening QRIS: %w", err)
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
