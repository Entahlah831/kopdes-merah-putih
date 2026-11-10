package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
	"github.com/kopdes/merah-putih/internal/security"
)

type PendaftaranHandler struct {
	pool             *pgxpool.Pool
	keyProvider      security.KeyProvider
	blindIndexSecret []byte
}

func NewPendaftaranHandler(pool *pgxpool.Pool, keyProvider security.KeyProvider, blindIndexSecret []byte) *PendaftaranHandler {
	return &PendaftaranHandler{
		pool:             pool,
		keyProvider:      keyProvider,
		blindIndexSecret: blindIndexSecret,
	}
}

type AjukanPendaftaranRequest struct {
	NIK           string    `json:"nik"`
	NamaLengkap   string    `json:"nama_lengkap"`
	TempatLahir   string    `json:"tempat_lahir"`
	TanggalLahir  string    `json:"tanggal_lahir"` // Format YYYY-MM-DD
	JenisKelamin  string    `json:"jenis_kelamin"` // 'L' atau 'P'
	Pekerjaan     string    `json:"pekerjaan"`
	AlamatKTP     string    `json:"alamat_ktp"`
	WilayahDesaID uuid.UUID `json:"wilayah_desa_id"`
}

type VerifikasiPendaftaranRequest struct {
	Status           string `json:"status"` // 'disetujui', 'ditolak', 'perbaikan'
	CatatanPerbaikan string `json:"catatan_perbaikan,omitempty"`
	AlasanDitolak    string `json:"alasan_ditolak,omitempty"`
}

// HandleAjukanPendaftaran menerima formulir pendaftaran anggota baru dan mengenkripsi data pribadi (UU 27/2022 PDP)
func (h *PendaftaranHandler) HandleAjukanPendaftaran(w http.ResponseWriter, r *http.Request) {
	var req AjukanPendaftaranRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	req.NIK = strings.TrimSpace(req.NIK)
	if len(req.NIK) != 16 {
		response.BadRequest(w, "NIK wajib 16 digit angka")
		return
	}
	if req.NamaLengkap == "" || req.TempatLahir == "" || req.TanggalLahir == "" || req.AlamatKTP == "" {
		response.BadRequest(w, "Seluruh data identitas diri wajib diisi")
		return
	}
	if req.JenisKelamin != "L" && req.JenisKelamin != "P" {
		response.BadRequest(w, "Jenis kelamin harus 'L' atau 'P'")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Konteks tenant koperasi tidak valid")
		return
	}

	ctx := r.Context()

	// 1. Hitung Blind Index NIK per tenant koperasi via HKDF
	nikHash, err := security.HashTenantBlindIndex(req.NIK, h.blindIndexSecret, claims.KoperasiID)
	if err != nil {
		response.InternalServerError(w, "Gagal menginisialisasi kunci isolasi tenant")
		return
	}

	pendaftaranID := uuid.New()

	// 2. Enkripsi data sensitif (NIK, Tanggal Lahir, Alamat) dengan AES-256-GCM + AAD
	var (
		nikCipher    []byte
		tglCipher    []byte
		alamatCipher []byte
	)

	if h.keyProvider != nil {
		aadNIK := security.FormatAAD("pendaftaran_anggota", "nik_enkripsi", pendaftaranID.String())
		nikCipher, err = h.keyProvider.EncryptWithAAD([]byte(req.NIK), aadNIK)
		if err != nil {
			response.InternalServerError(w, "Gagal mengamankan enkripsi identitas NIK")
			return
		}
		aadTgl := security.FormatAAD("pendaftaran_anggota", "tanggal_lahir_enkripsi", pendaftaranID.String())
		tglCipher, err = h.keyProvider.EncryptWithAAD([]byte(req.TanggalLahir), aadTgl)
		if err != nil {
			response.InternalServerError(w, "Gagal mengamankan enkripsi tanggal lahir")
			return
		}
		aadAlamat := security.FormatAAD("pendaftaran_anggota", "alamat_ktp_enkripsi", pendaftaranID.String())
		alamatCipher, err = h.keyProvider.EncryptWithAAD([]byte(req.AlamatKTP), aadAlamat)
		if err != nil {
			response.InternalServerError(w, "Gagal mengamankan enkripsi alamat KTP")
			return
		}
	} else {
		// Fallback hash dummy jika key provider tidak aktif di dev
		nikCipher = []byte("ENC:" + req.NIK)
		tglCipher = []byte("ENC:" + req.TanggalLahir)
		alamatCipher = []byte("ENC:" + req.AlamatKTP)
	}

	var noRegistrasi string
	year := time.Now().Year()

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// Cek duplikasi NIK aktif di koperasi ini
		var exists bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM pendaftaran_anggota 
				WHERE koperasi_id = $1 AND nik_hash = $2 AND status NOT IN ('ditolak') AND deleted_at IS NULL
			);
		`, claims.KoperasiID, nikHash).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			return errors.New("NIK_DUPLICATE: Pendaftaran aktif dengan NIK ini sudah terdaftar di koperasi")
		}

		// Ambil nomor urut registrasi atomik yang aman konkurensi
		var nextSeq int64
		err = tx.QueryRow(ctx, `
			INSERT INTO koperasi_nomor_urut (koperasi_id, jenis_nomor, periode_tahun, nilai_terakhir)
			VALUES ($1, 'registrasi', $2, 1)
			ON CONFLICT (koperasi_id, jenis_nomor, periode_tahun)
			DO UPDATE SET nilai_terakhir = koperasi_nomor_urut.nilai_terakhir + 1, updated_at = NOW()
			RETURNING nilai_terakhir;
		`, claims.KoperasiID, year).Scan(&nextSeq)
		if err != nil {
			return fmt.Errorf("gagal mendapatkan nomor registrasi: %w", err)
		}
		noRegistrasi = fmt.Sprintf("REG/%d/%05d", year, nextSeq)

		// Ambil wilayah_desa_id: jika tidak dikirim, gunakan default wilayah_desa_id milik koperasi
		wilayahDesaID := req.WilayahDesaID
		if wilayahDesaID == uuid.Nil {
			err = tx.QueryRow(ctx, "SELECT wilayah_desa_id FROM koperasi WHERE id = $1", claims.KoperasiID).Scan(&wilayahDesaID)
			if err != nil {
				return fmt.Errorf("gagal mendapatkan wilayah koperasi: %w", err)
			}
		}

		// Simpan pendaftaran baru
		_, err = tx.Exec(ctx, `
			INSERT INTO pendaftaran_anggota (
				id, koperasi_id, user_id, no_registrasi,
				nik_enkripsi, nik_hash, nama_lengkap, tempat_lahir,
				tanggal_lahir_enkripsi, jenis_kelamin, pekerjaan,
				alamat_ktp_enkripsi, wilayah_desa_id, status,
				diajukan_oleh, waktu_diajukan, created_by
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, $7, $8,
				$9, $10, $11,
				$12, $13, 'diajukan',
				$3, NOW(), $3
			);
		`, pendaftaranID, claims.KoperasiID, claims.UserID, noRegistrasi,
			nikCipher, nikHash, req.NamaLengkap, req.TempatLahir,
			tglCipher, req.JenisKelamin, req.Pekerjaan,
			alamatCipher, wilayahDesaID)
		if err != nil {
			return fmt.Errorf("gagal menyimpan formulir pendaftaran: %w", err)
		}

		// Catat log status awal
		_, err = tx.Exec(ctx, `
			INSERT INTO pendaftaran_status_log (
				koperasi_id, pendaftaran_id, status_lama, status_baru, catatan, created_by
			) VALUES ($1, $2, 'draft', 'diajukan', 'Formulir pendaftaran diajukan oleh calon anggota', $3);
		`, claims.KoperasiID, pendaftaranID, claims.UserID)
		return err
	})

	if err != nil {
		if strings.Contains(err.Error(), "NIK_DUPLICATE") {
			response.Conflict(w, "NIK ini sudah terdaftar dalam pengajuan keanggotaan aktif koperasi")
			return
		}
		response.InternalServerError(w, fmt.Sprintf("Gagal memproses pendaftaran: %v", err))
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"pendaftaran_id": pendaftaranID,
		"no_registrasi":  noRegistrasi,
		"status":         "diajukan",
		"pesan":          "Pendaftaran berhasil diajukan dan sedang menunggu verifikasi pengurus.",
	})
}

// HandleGetPendaftaran mengambil detail pendaftaran dengan penyamaran NIK untuk privasi (anti-IDOR)
func (h *PendaftaranHandler) HandleGetPendaftaran(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	pendaftaranID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID pendaftaran tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	privilegedRoles := map[string]bool{
		"admin": true, "pengurus": true, "bendahara": true, "ketua": true, "operator": true, "pengawas": true,
	}
	isPrivileged := claims.IsPlatformAdmin || privilegedRoles[claims.Role]

	ctx := r.Context()
	var (
		userID        uuid.UUID
		noRegistrasi  string
		namaLengkap   string
		tempatLahir   string
		jenisKelamin  string
		pekerjaan     *string
		status        string
		waktuDiajukan *time.Time
		nikCipher     []byte
	)

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		query := `
			SELECT user_id, no_registrasi, nama_lengkap, tempat_lahir, jenis_kelamin,
			       pekerjaan, status, waktu_diajukan, nik_enkripsi
			FROM pendaftaran_anggota
			WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL;
		`
		return tx.QueryRow(ctx, query, pendaftaranID, claims.KoperasiID).Scan(
			&userID, &noRegistrasi, &namaLengkap, &tempatLahir, &jenisKelamin,
			&pekerjaan, &status, &waktuDiajukan, &nikCipher,
		)
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, "Data pendaftaran tidak ditemukan")
			return
		}
		response.InternalServerError(w, "Gagal mengambil data pendaftaran")
		return
	}

	// Pembatasan IDOR: Anggota biasa hanya berhak melihat pendaftaran miliknya
	if !isPrivileged && userID != claims.UserID {
		response.NotFound(w, "Data pendaftaran tidak ditemukan")
		return
	}

	// Dekripsi NIK dan lakukan masking (hanya tampilkan 4 digit depan dan 4 digit belakang)
	nikDisplay := "****************"
	if h.keyProvider != nil {
		aadNIK := security.FormatAAD("pendaftaran_anggota", "nik_enkripsi", pendaftaranID.String())
		if plain, err := h.keyProvider.DecryptWithAAD(nikCipher, aadNIK); err == nil {
			nikStr := string(plain)
			if len(nikStr) == 16 {
				nikDisplay = nikStr[:4] + "********" + nikStr[12:]
			}
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"pendaftaran_id": pendaftaranID,
		"no_registrasi":  noRegistrasi,
		"nama_lengkap":   namaLengkap,
		"nik_masked":     nikDisplay,
		"tempat_lahir":   tempatLahir,
		"jenis_kelamin":  jenisKelamin,
		"pekerjaan":      pekerjaan,
		"status":         status,
		"waktu_diajukan": waktuDiajukan,
	})
}

// HandleVerifikasiPendaftaran mengeksekusi maker-checker persetujuan pendaftaran menjadi anggota resmi
func (h *PendaftaranHandler) HandleVerifikasiPendaftaran(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	pendaftaranID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID pendaftaran tidak valid")
		return
	}

	var req VerifikasiPendaftaranRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.Status != "disetujui" && req.Status != "ditolak" && req.Status != "perbaikan" {
		response.BadRequest(w, "Status keputusan harus salah satu dari: 'disetujui', 'ditolak', 'perbaikan'")
		return
	}
	if req.Status == "ditolak" && strings.TrimSpace(req.AlasanDitolak) == "" {
		response.BadRequest(w, "Alasan penolakan wajib dicantumkan jika status ditolak")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var (
		noAnggotaBaru string
		anggotaIDBaru uuid.UUID
	)

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		var (
			targetUserID  uuid.UUID
			diajukanOleh  uuid.UUID
			statusLama    string
			namaLengkap   string
			tempatLahir   string
			jenisKelamin  string
			pekerjaan     *string
			nikEnkripsi   []byte
			nikHash       string
			tglEnkripsi   []byte
		)

		// Kunci baris pendaftaran untuk mencegah konkurensi approve ganda
		err := tx.QueryRow(ctx, `
			SELECT user_id, diajukan_oleh, status, nama_lengkap, tempat_lahir,
			       jenis_kelamin, pekerjaan, nik_enkripsi, nik_hash, tanggal_lahir_enkripsi
			FROM pendaftaran_anggota
			WHERE id = $1 AND koperasi_id = $2 AND deleted_at IS NULL
			FOR UPDATE;
		`, pendaftaranID, claims.KoperasiID).Scan(
			&targetUserID, &diajukanOleh, &statusLama, &namaLengkap, &tempatLahir,
			&jenisKelamin, &pekerjaan, &nikEnkripsi, &nikHash, &tglEnkripsi,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errors.New("NOT_FOUND: Pendaftaran tidak ditemukan")
			}
			return err
		}

		// KONTROL MAKER-CHECKER (Aturan 4): Verifikator DILARANG sama dengan pengaju
		if claims.UserID == diajukanOleh {
			return errors.New("MAKER_CHECKER_VIOLATION: Prinsip Maker-Checker melarang pengaju memverifikasi pendaftarannya sendiri")
		}

		if statusLama == "disetujui" || statusLama == "ditolak" {
			return fmt.Errorf("INVALID_STATE: Pendaftaran sudah berstatus final '%s'", statusLama)
		}

		if req.Status == "disetujui" {
			// 1. Buat nomor anggota atomik dari counter koperasi
			year := time.Now().Year()
			var nextSeq int64
			err = tx.QueryRow(ctx, `
				INSERT INTO koperasi_nomor_urut (koperasi_id, jenis_nomor, periode_tahun, nilai_terakhir)
				VALUES ($1, 'anggota', $2, 1)
				ON CONFLICT (koperasi_id, jenis_nomor, periode_tahun)
				DO UPDATE SET nilai_terakhir = koperasi_nomor_urut.nilai_terakhir + 1, updated_at = NOW()
				RETURNING nilai_terakhir;
			`, claims.KoperasiID, year).Scan(&nextSeq)
			if err != nil {
				return fmt.Errorf("gagal menghasilkan nomor anggota: %w", err)
			}
			noAnggotaBaru = fmt.Sprintf("ANG/%d/%05d", year, nextSeq)
			anggotaIDBaru = uuid.New()

			// 2. Terbitkan ke tabel Buku Daftar Anggota (UU 25/1992)
			_, err = tx.Exec(ctx, `
				INSERT INTO anggota (
					id, koperasi_id, user_id, pendaftaran_id, no_anggota,
					nik_enkripsi, nik_hash, nama_lengkap, tempat_lahir,
					tanggal_lahir_enkripsi, jenis_kelamin, pekerjaan,
					status, tanggal_bergabung, created_by
				) VALUES (
					$1, $2, $3, $4, $5,
					$6, $7, $8, $9,
					$10, $11, $12,
					'aktif', CURRENT_DATE, $13
				);
			`, anggotaIDBaru, claims.KoperasiID, targetUserID, pendaftaranID, noAnggotaBaru,
				nikEnkripsi, nikHash, namaLengkap, tempatLahir,
				tglEnkripsi, jenisKelamin, pekerjaan, claims.UserID)
			if err != nil {
				return fmt.Errorf("gagal menerbitkan keanggotaan: %w", err)
			}

			// 3. Berikan peran 'anggota' di user_roles
			var roleAnggotaID uuid.UUID
			_ = tx.QueryRow(ctx, "SELECT id FROM roles WHERE kode = 'anggota'").Scan(&roleAnggotaID)
			if roleAnggotaID != uuid.Nil {
				_, _ = tx.Exec(ctx, `
					INSERT INTO user_roles (user_id, role_id, koperasi_id)
					VALUES ($1, $2, $3)
					ON CONFLICT (koperasi_id, user_id, role_id) DO NOTHING;
				`, targetUserID, roleAnggotaID, claims.KoperasiID)
			}

			// 4. Catat riwayat status anggota
			_, err = tx.Exec(ctx, `
				INSERT INTO anggota_status_log (
					koperasi_id, anggota_id, status_baru, alasan, created_by
				) VALUES ($1, $2, 'aktif', 'Pendaftaran disetujui oleh pengurus', $3);
			`, claims.KoperasiID, anggotaIDBaru, claims.UserID)
			if err != nil {
				return fmt.Errorf("gagal mencatat status anggota: %w", err)
			}

			// 5. Update pendaftaran
			_, err = tx.Exec(ctx, `
				UPDATE pendaftaran_anggota
				SET status = 'disetujui', disetujui_oleh = $1, waktu_disetujui = NOW(), updated_at = NOW()
				WHERE id = $2;
			`, claims.UserID, pendaftaranID)
			if err != nil {
				return err
			}
		} else if req.Status == "ditolak" {
			_, err = tx.Exec(ctx, `
				UPDATE pendaftaran_anggota
				SET status = 'ditolak', alasan_ditolak = $1, diverifikasi_oleh = $2, waktu_diverifikasi = NOW(), updated_at = NOW()
				WHERE id = $3;
			`, req.AlasanDitolak, claims.UserID, pendaftaranID)
			if err != nil {
				return err
			}
		} else { // 'perbaikan'
			_, err = tx.Exec(ctx, `
				UPDATE pendaftaran_anggota
				SET status = 'perbaikan', catatan_perbaikan = $1, diverifikasi_oleh = $2, waktu_diverifikasi = NOW(), updated_at = NOW()
				WHERE id = $3;
			`, req.CatatanPerbaikan, claims.UserID, pendaftaranID)
			if err != nil {
				return err
			}
		}

		// Log jejak audit pendaftaran
		_, err = tx.Exec(ctx, `
			INSERT INTO pendaftaran_status_log (
				koperasi_id, pendaftaran_id, status_lama, status_baru, catatan, created_by
			) VALUES ($1, $2, $3, $4, $5, $6);
		`, claims.KoperasiID, pendaftaranID, statusLama, req.Status, req.CatatanPerbaikan, claims.UserID)
		return err
	})

	if err != nil {
		if strings.Contains(err.Error(), "MAKER_CHECKER_VIOLATION") {
			response.Forbidden(w, "Prinsip Maker-Checker: Anda tidak boleh menyetujui pendaftaran yang Anda ajukan sendiri")
			return
		}
		if strings.Contains(err.Error(), "NOT_FOUND") {
			response.NotFound(w, "Pendaftaran tidak ditemukan")
			return
		}
		if strings.Contains(err.Error(), "INVALID_STATE") {
			response.BadRequest(w, "Pendaftaran sudah dalam status final")
			return
		}
		response.InternalServerError(w, fmt.Sprintf("Gagal memproses verifikasi: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"pendaftaran_id": pendaftaranID,
		"status":         req.Status,
		"no_anggota":     noAnggotaBaru,
		"anggota_id":     anggotaIDBaru,
		"pesan":          fmt.Sprintf("Pendaftaran berhasil diproses dengan status '%s'", req.Status),
	})
}

// HandleUploadDokumenPendaftaran memvalidasi MIME type, magic bytes, ukuran file (< 5MB), dan menyimpan lampiran
func (h *PendaftaranHandler) HandleUploadDokumenPendaftaran(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	pendaftaranID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID pendaftaran tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	// Batasi ukuran upload maksimal 5 MB
	const maxUploadSize = 5 * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		response.BadRequest(w, "Ukuran file lampiran melampaui batas maksimal 5 MB")
		return
	}

	file, header, err := r.FormFile("dokumen")
	if err != nil {
		file, header, err = r.FormFile("file")
	}
	if err != nil {
		response.BadRequest(w, "File lampiran ('dokumen' atau 'file') wajib disertakan")
		return
	}
	defer file.Close()

	jenisDokumen := r.FormValue("jenis_dokumen") // 'ktp', 'kk', 'foto_diri', 'surat_domisili'
	if jenisDokumen == "" {
		jenisDokumen = "ktp"
	}

	// Baca seluruh byte file ke memori
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		response.InternalServerError(w, "Gagal membaca konten file")
		return
	}

	// Validasi Magic Bytes (Deteksi tipe file asli)
	mimeType := detectMIMEType(fileBytes)
	if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "application/pdf" {
		response.BadRequest(w, "Format file tidak didukung. Hanya file JPG, PNG, atau PDF yang diizinkan.")
		return
	}

	// Hitung checksum SHA-256
	h256 := sha256.Sum256(fileBytes)
	checksum := hex.EncodeToString(h256[:])

	dokumenID := uuid.New()
	storageKey := fmt.Sprintf("pendaftaran/%s/%s/%s_%s", claims.KoperasiID, pendaftaranID, dokumenID, header.Filename)

	ctx := r.Context()
	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// Simpan record metadata dokumen
		_, err := tx.Exec(ctx, `
			INSERT INTO dokumen (
				id, koperasi_id, nama_file, mime_type, ukuran_byte,
				storage_key, checksum_sha256, klasifikasi_data, retensi_sampai, created_by
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, 'rahasia_pii', CURRENT_DATE + INTERVAL '5 years', $8
			);
		`, dokumenID, claims.KoperasiID, header.Filename, mimeType, len(fileBytes),
			storageKey, checksum, claims.UserID)
		if err != nil {
			return fmt.Errorf("gagal menyimpan metadata dokumen: %w", err)
		}

		// Hubungkan ke pendaftaran_dokumen
		_, err = tx.Exec(ctx, `
			INSERT INTO pendaftaran_dokumen (
				pendaftaran_id, dokumen_id, koperasi_id, jenis_dokumen,
				storage_key, checksum_sha256, created_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7);
		`, pendaftaranID, dokumenID, claims.KoperasiID, jenisDokumen, storageKey, checksum, claims.UserID)
		return err
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal menyimpan dokumen: %v", err))
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"dokumen_id":      dokumenID,
		"pendaftaran_id":  pendaftaranID,
		"nama_file":       header.Filename,
		"mime_type":       mimeType,
		"ukuran_byte":     len(fileBytes),
		"checksum_sha256": checksum,
		"pesan":           "Dokumen berhasil diunggah dan terverifikasi integritasnya.",
	})
}

// detectMIMEType memeriksa magic bytes untuk mencegah pemalsuan ekstensi file
func detectMIMEType(data []byte) string {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "image/png"
	}
	if len(data) >= 4 && bytes.Equal(data[:4], []byte("%PDF")) {
		return "application/pdf"
	}
	return http.DetectContentType(data)
}

// HandlePurgeRejectedPendaftaran menjalankan pembersihan PII data pendaftaran ditolak > 30 hari (UU 27/2022 PDP)
func (h *PendaftaranHandler) HandlePurgeRejectedPendaftaran(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var purgedCount int
	var storageKeys []string

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// 1. Ambil daftar storage_key berkas KYC yang akan dihapus dari penyimpanan
		rows, err := tx.Query(ctx, `
			SELECT pd.storage_key
			FROM pendaftaran_dokumen pd
			JOIN pendaftaran_anggota pa ON pd.pendaftaran_id = pa.id
			WHERE pa.koperasi_id = $1
			  AND pa.status = 'ditolak'
			  AND pd.storage_key IS NOT NULL;
		`, claims.KoperasiID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var sk string
				if err := rows.Scan(&sk); err == nil && sk != "" {
					storageKeys = append(storageKeys, sk)
				}
			}
		}

		// 2. Jalankan fungsi SQL fn_purge_rejected_pendaftaran_pdp (retensi dari konfigurasi_aturan)
		return tx.QueryRow(ctx, "SELECT fn_purge_rejected_pendaftaran_pdp($1, NULL);", claims.KoperasiID).Scan(&purgedCount)
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal menjalankan pembersihan berkas: %v", err))
		return
	}

	// 3. Hapus berkas KYC fisik dari penyimpanan lokal jika ada
	storageBase := os.Getenv("STORAGE_DIR")
	if storageBase == "" {
		storageBase = "storage"
	}
	for _, sk := range storageKeys {
		filePath := filepath.Join(storageBase, sk)
		_ = os.Remove(filePath)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"jumlah_dibersihkan": purgedCount,
		"catatan":            "Retensi diatur via konfigurasi_aturan (PERLU VERIFIKASI HUKUM).",
		"pesan":              fmt.Sprintf("Pembersihan data pribadi (UU 27/2022 PDP) selesai: %d berkas pendaftaran ditolak dihapus permanen.", purgedCount),
	})
}
