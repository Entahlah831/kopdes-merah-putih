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

type TokoHandler struct {
	pool *pgxpool.Pool
}

func NewTokoHandler(pool *pgxpool.Pool) *TokoHandler {
	return &TokoHandler{pool: pool}
}

type AddToCartRequest struct {
	ProdukID uuid.UUID `json:"produk_id"`
	Qty      float64   `json:"qty"`
}

// HandleAddToCart menambahkan atau memperbarui kuantitas produk dalam keranjang belanja
func (h *TokoHandler) HandleAddToCart(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	var req AddToCartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.ProdukID == uuid.Nil || req.Qty <= 0 {
		response.BadRequest(w, "produk_id dan qty (> 0) wajib diisi")
		return
	}

	ctx := r.Context()
	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// Cari atau buat keranjang pengguna
		var keranjangID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT id FROM keranjang WHERE user_id = $1 AND koperasi_id = $2;
		`, claims.UserID, claims.KoperasiID).Scan(&keranjangID)
		if err != nil {
			var anggotaID *uuid.UUID
			var aID uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM anggota WHERE user_id = $1 AND koperasi_id = $2 LIMIT 1;`, claims.UserID, claims.KoperasiID).Scan(&aID); err == nil {
				anggotaID = &aID
			}

			err = tx.QueryRow(ctx, `
				INSERT INTO keranjang (koperasi_id, user_id, anggota_id)
				VALUES ($1, $2, $3)
				RETURNING id;
			`, claims.KoperasiID, claims.UserID, anggotaID).Scan(&keranjangID)
			if err != nil {
				return fmt.Errorf("gagal membuat keranjang: %w", err)
			}
		}

		// Upsert item keranjang
		_, err = tx.Exec(ctx, `
			INSERT INTO keranjang_item (koperasi_id, keranjang_id, produk_id, qty)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (keranjang_id, produk_id, varian_id)
			DO UPDATE SET qty = EXCLUDED.qty, updated_at = NOW();
		`, claims.KoperasiID, keranjangID, req.ProdukID, req.Qty)
		return err
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal menambahkan ke keranjang: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message":   "Item berhasil ditambahkan ke keranjang belanja",
		"produk_id": req.ProdukID,
		"qty":       req.Qty,
	})
}

// HandleGetKeranjang mengambil daftar item di keranjang pengguna
func (h *TokoHandler) HandleGetKeranjang(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var items []map[string]interface{}

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT ki.id, ki.produk_id, p.nama, p.sku, ki.qty,
			       COALESCE(ph.harga_umum, 0)::bigint, COALESCE(ph.harga_anggota, 0)::bigint
			FROM keranjang k
			JOIN keranjang_item ki ON ki.keranjang_id = k.id
			JOIN produk p ON p.id = ki.produk_id
			LEFT JOIN LATERAL (
				SELECT harga_umum, harga_anggota
				FROM produk_harga
				WHERE produk_id = p.id AND status_aktif = true AND deleted_at IS NULL
				ORDER BY berlaku_mulai DESC LIMIT 1
			) ph ON true
			WHERE k.user_id = $1 AND k.koperasi_id = $2;
		`, claims.UserID, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				itemID, pID    uuid.UUID
				nama, kode     string
				qty            float64
				hUmum, hAnggot int64
			)
			if err := rows.Scan(&itemID, &pID, &nama, &kode, &qty, &hUmum, &hAnggot); err != nil {
				return err
			}
			items = append(items, map[string]interface{}{
				"item_id":       itemID,
				"produk_id":     pID,
				"nama_produk":   nama,
				"sku":           kode,
				"qty":           qty,
				"harga_umum":    hUmum,
				"harga_anggota": hAnggot,
			})
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, "Gagal mengambil keranjang")
		return
	}

	if items == nil {
		items = []map[string]interface{}{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"total": len(items),
	})
}

// HandleRemoveFromCart menghapus item dari keranjang
func (h *TokoHandler) HandleRemoveFromCart(w http.ResponseWriter, r *http.Request) {
	produkIDStr := r.PathValue("produk_id")
	produkID, err := uuid.Parse(produkIDStr)
	if err != nil {
		response.BadRequest(w, "ID produk tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			DELETE FROM keranjang_item
			WHERE produk_id = $1 AND keranjang_id IN (
				SELECT id FROM keranjang WHERE user_id = $2 AND koperasi_id = $3
			);
		`, produkID, claims.UserID, claims.KoperasiID)
		return err
	})

	if err != nil {
		response.InternalServerError(w, "Gagal menghapus item dari keranjang")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"message": "Item berhasil dihapus dari keranjang"})
}

type CheckoutRequest struct {
	MetodeSerahTerima string `json:"metode_serah_terima"` // 'ambil_di_koperasi' | 'kirim_alamat'
	Catatan           string `json:"catatan"`
	KodePromo         string `json:"kode_promo,omitempty"`
}

// HandleCheckout memproses checkout seluruh item keranjang menjadi pesanan aktif
func (h *TokoHandler) HandleCheckout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	var req CheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}
	if req.MetodeSerahTerima == "" {
		req.MetodeSerahTerima = "ambil_di_koperasi"
	}

	ctx := r.Context()
	var (
		pesananID  uuid.UUID
		noPesanan  string
		totalAkhir int64
	)

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		// Cari keranjang
		var keranjangID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT id FROM keranjang WHERE user_id = $1 AND koperasi_id = $2;
		`, claims.UserID, claims.KoperasiID).Scan(&keranjangID)
		if err != nil {
			return errors.New("keranjang belanja kosong")
		}

		// Ambil item keranjang beserta harga
		type CartRow struct {
			ProdukID     uuid.UUID
			NamaProduk   string
			SKU          string
			UnitUsahaID  uuid.UUID
			HargaID      uuid.UUID
			Qty          float64
			HargaUmum    int64
			HargaAnggota int64
		}
		var cartItems []CartRow

		rows, err := tx.Query(ctx, `
			SELECT ki.produk_id, p.nama, p.sku, p.unit_usaha_id, ph.id, ki.qty,
			       ph.harga_umum::bigint, ph.harga_anggota::bigint
			FROM keranjang_item ki
			JOIN produk p ON p.id = ki.produk_id
			JOIN LATERAL (
				SELECT id, harga_umum, harga_anggota
				FROM produk_harga
				WHERE produk_id = p.id AND status_aktif = true AND deleted_at IS NULL
				ORDER BY berlaku_mulai DESC LIMIT 1
			) ph ON true
			WHERE ki.keranjang_id = $1 AND ki.koperasi_id = $2;
		`, keranjangID, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var cr CartRow
			if err := rows.Scan(&cr.ProdukID, &cr.NamaProduk, &cr.SKU, &cr.UnitUsahaID, &cr.HargaID, &cr.Qty, &cr.HargaUmum, &cr.HargaAnggota); err != nil {
				return err
			}
			cartItems = append(cartItems, cr)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		if len(cartItems) == 0 {
			return errors.New("keranjang belanja kosong, tidak ada item untuk checkout")
		}

		// 1. Periksa ketersediaan stok produk (menolak jika stok tidak mencukupi atau minus)
		type StockCheck struct {
			SaldoID     uuid.UUID
			GudangID    uuid.UUID
			QtyTersedia float64
		}
		stockUpdates := make(map[uuid.UUID]StockCheck)

		for _, it := range cartItems {
			var sc StockCheck
			err := tx.QueryRow(ctx, `
				SELECT id, gudang_id, qty_tersedia
				FROM stok_saldo
				WHERE produk_id = $1 AND koperasi_id = $2
				FOR UPDATE;
			`, it.ProdukID, claims.KoperasiID).Scan(&sc.SaldoID, &sc.GudangID, &sc.QtyTersedia)
			if err != nil {
				return fmt.Errorf("stok produk '%s' tidak ditemukan atau belum diinisialisasi", it.NamaProduk)
			}
			if sc.QtyTersedia < it.Qty {
				return fmt.Errorf("stok produk '%s' tidak mencukupi (tersedia: %v, diminta: %v)", it.NamaProduk, sc.QtyTersedia, it.Qty)
			}
			stockUpdates[it.ProdukID] = sc
		}

		// Tentukan apakah pembeli anggota aktif
		var anggotaID *uuid.UUID
		var aID uuid.UUID
		isAnggota := false
		if err := tx.QueryRow(ctx, `SELECT id FROM anggota WHERE user_id = $1 AND koperasi_id = $2 AND status = 'aktif' AND deleted_at IS NULL LIMIT 1;`, claims.UserID, claims.KoperasiID).Scan(&aID); err == nil {
			anggotaID = &aID
			isAnggota = true
		}

		var subtotalUmum, subtotalAnggota int64
		for _, it := range cartItems {
			subtotalUmum += int64(float64(it.HargaUmum) * it.Qty)
			subtotalAnggota += int64(float64(it.HargaAnggota) * it.Qty)
		}

		if isAnggota {
			totalAkhir = subtotalAnggota
		} else {
			totalAkhir = subtotalUmum
		}

		// 2. Terapkan kode promo aktif bila disertakan
		var promoPotongan int64
		var promoID *uuid.UUID
		if req.KodePromo != "" {
			var (
				pID           uuid.UUID
				tipePromo     string
				nilaiDiskon   float64
				cakupan       string
				minBelanja    float64
				maksPotongan  *float64
				kuotaTotal    *int
				kuotaTerpakai int
				berlakuMulai  time.Time
				berlakuSampai time.Time
			)
			err := tx.QueryRow(ctx, `
				SELECT id, tipe_promo::text, nilai_diskon, cakupan::text, minimal_belanja, maksimal_potongan, kuota_total, kuota_terpakai, berlaku_mulai, berlaku_sampai
				FROM promo
				WHERE kode = $1 AND koperasi_id = $2
				FOR UPDATE;
			`, req.KodePromo, claims.KoperasiID).Scan(&pID, &tipePromo, &nilaiDiskon, &cakupan, &minBelanja, &maksPotongan, &kuotaTotal, &kuotaTerpakai, &berlakuMulai, &berlakuSampai)
			if err != nil {
				return errors.New("kode promo tidak ditemukan")
			}
			now := time.Now()
			if now.Before(berlakuMulai) || now.After(berlakuSampai) {
				return errors.New("kode promo telah kedaluwarsa atau belum berlaku")
			}
			if kuotaTotal != nil && kuotaTerpakai >= *kuotaTotal {
				return errors.New("kuota promo telah habis")
			}
			if float64(totalAkhir) < minBelanja {
				return fmt.Errorf("minimal belanja untuk promo ini adalah Rp %.0f", minBelanja)
			}

			if tipePromo == "persen" {
				promoPotongan = int64(float64(totalAkhir) * (nilaiDiskon / 100.0))
				if maksPotongan != nil && *maksPotongan > 0 && float64(promoPotongan) > *maksPotongan {
					promoPotongan = int64(*maksPotongan)
				}
			} else if tipePromo == "nominal" {
				promoPotongan = int64(nilaiDiskon)
			}
			if promoPotongan > totalAkhir {
				promoPotongan = totalAkhir
			}
			totalAkhir -= promoPotongan
			promoID = &pID

			_, _ = tx.Exec(ctx, `UPDATE promo SET kuota_terpakai = kuota_terpakai + 1 WHERE id = $1;`, pID)
		}

		unitUsahaID := cartItems[0].UnitUsahaID
		noPesanan = fmt.Sprintf("ORD-%s-%04d", uuid.New().String()[:8], time.Now().UnixNano()%10000)

		// Buat pesanan
		err = tx.QueryRow(ctx, `
			INSERT INTO pesanan (
				koperasi_id, unit_usaha_id, no_pesanan, pembeli_user_id, anggota_id,
				is_harga_anggota, status, subtotal_harga_umum, subtotal_harga_anggota,
				total_akhir, metode_serah_terima, catatan_pembeli, batas_waktu_bayar, created_by
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, 'draft', $7, $8,
				$9, $10, $11, NOW() + INTERVAL '24 hours', $4
			)
			RETURNING id;
		`, claims.KoperasiID, unitUsahaID, noPesanan, claims.UserID, anggotaID,
			isAnggota, subtotalUmum, subtotalAnggota, totalAkhir, req.MetodeSerahTerima, req.Catatan,
		).Scan(&pesananID)
		if err != nil {
			return fmt.Errorf("gagal membuat pesanan: %w", err)
		}

		// Masukkan pesanan_item
		for _, it := range cartItems {
			hargaDipakai := it.HargaUmum
			if isAnggota {
				hargaDipakai = it.HargaAnggota
			}
			subItem := int64(float64(hargaDipakai) * it.Qty)

			_, err = tx.Exec(ctx, `
				INSERT INTO pesanan_item (
					koperasi_id, pesanan_id, produk_id, produk_harga_id,
					nama_produk_snapshot, sku_snapshot, qty,
					snapshot_harga_umum, snapshot_harga_anggota, snapshot_harga_dipakai,
					snapshot_harga_akhir_satuan, subtotal_akhir
				) VALUES (
					$1, $2, $3, $4,
					$5, $6, $7,
					$8, $9, $10,
					$10, $11
				);
			`, claims.KoperasiID, pesananID, it.ProdukID, it.HargaID,
				it.NamaProduk, it.SKU, it.Qty,
				it.HargaUmum, it.HargaAnggota, hargaDipakai, subItem,
			)
			if err != nil {
				return fmt.Errorf("gagal membuat item pesanan: %w", err)
			}
		}

		// 3. Potong stok lewat ledger append-only stok_mutasi (trigger trg_stok_mutasi_update_saldo otomatis mengupdate stok_saldo & cegah minus)
		for _, it := range cartItems {
			sc := stockUpdates[it.ProdukID]
			newSaldo := sc.QtyTersedia - it.Qty

			_, err = tx.Exec(ctx, `
				INSERT INTO stok_mutasi (
					id, koperasi_id, gudang_id, produk_id, jenis_mutasi,
					qty_perubahan, qty_saldo_setelah, referensi_tipe, referensi_id,
					catatan, created_by, created_at
				) VALUES (
					uuid_generate_v7(), $1, $2, $3, 'keluar_penjualan',
					$4, $5, 'pesanan', $6,
					'Pengurangan stok checkout pesanan', $7, clock_timestamp()
				);
			`, claims.KoperasiID, sc.GudangID, it.ProdukID, -it.Qty, newSaldo, pesananID, claims.UserID)
			if err != nil {
				return fmt.Errorf("gagal mencatat mutasi stok: %w", err)
			}
		}

		// Catat riwayat promo_penggunaan jika promo digunakan
		if promoID != nil && isAnggota && anggotaID != nil && promoPotongan > 0 {
			_, _ = tx.Exec(ctx, `
				INSERT INTO promo_penggunaan (
					id, koperasi_id, promo_id, anggota_id, pesanan_id,
					nominal_potongan, status
				) VALUES (
					uuid_generate_v7(), $1, $2, $3, $4,
					$5, 'dipesan'
				);
			`, claims.KoperasiID, *promoID, *anggotaID, pesananID, promoPotongan)
		}

		// Kosongkan keranjang belanja
		_, err = tx.Exec(ctx, `DELETE FROM keranjang_item WHERE keranjang_id = $1;`, keranjangID)
		return err
	})

	if err != nil {
		response.BadRequest(w, fmt.Sprintf("Checkout gagal: %v", err))
		return
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"pesanan_id":  pesananID,
		"no_pesanan":  noPesanan,
		"total_akhir": totalAkhir,
		"status":      "draft",
		"message":     "Pesanan berhasil dibuat. Lanjutkan ke pembayaran QRIS.",
	})
}

// HandleListPesanan menampilkan daftar pesanan pengguna
func (h *TokoHandler) HandleListPesanan(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var pesananList []map[string]interface{}

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		var rows pgx.Rows
		var err error

		if claims.Role == "anggota" {
			rows, err = tx.Query(ctx, `
				SELECT id, no_pesanan, status::text, total_akhir::bigint, metode_serah_terima, created_at
				FROM pesanan
				WHERE pembeli_user_id = $1 AND koperasi_id = $2 AND deleted_at IS NULL
				ORDER BY created_at DESC;
			`, claims.UserID, claims.KoperasiID)
		} else {
			rows, err = tx.Query(ctx, `
				SELECT id, no_pesanan, status::text, total_akhir::bigint, metode_serah_terima, created_at
				FROM pesanan
				WHERE koperasi_id = $1 AND deleted_at IS NULL
				ORDER BY created_at DESC LIMIT 50;
			`, claims.KoperasiID)
		}
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				id         uuid.UUID
				noP, st    string
				total      int64
				metode     string
				createdAt  time.Time
			)
			if err := rows.Scan(&id, &noP, &st, &total, &metode, &createdAt); err != nil {
				return err
			}
			pesananList = append(pesananList, map[string]interface{}{
				"id":                  id,
				"no_pesanan":          noP,
				"status":              st,
				"total_akhir":         total,
				"metode_serah_terima": metode,
				"created_at":          createdAt.Format(time.RFC3339),
			})
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, "Gagal mengambil daftar pesanan")
		return
	}

	if pesananList == nil {
		pesananList = []map[string]interface{}{}
	}

	response.JSON(w, http.StatusOK, pesananList)
}
