# ADR-001: Reservasi Stok Tertahan Saat Checkout dan Fallback `perlu_tindak_lanjut` (D1)

- **Status**: `DIUBAH` (sebelumnya hanya memeriksa `stok >= qty` saat checkout lalu memotong saat verifikasi bayar, tanpa menahan stok)
- **Tag Kebijakan**: **KEPUTUSAN PEMILIK** (`STOCK_HOLD_MINUTES=60` menit dapat diubah pemilik lewat `.env`)

## (a) Keputusan Saat Ini dan Lokasi Pembuatan
Pada branch `lean-revamp`, `internal/pesanan/service.go` (`CreateOrder`) memeriksa `produk.stok >= kuantitas`, tetapi tidak menahan stok selama pembeli menunaikan pembayaran QRIS/tunai. Pemotongan stok fisik (`DecrementProdukStock`) terjadi belakangan pada `internal/pembayaran/verifikasi.go` saat `admin`/`bendahara` memilih `setuju`. Akibatnya, dua atau lebih pembeli dapat melewati checkout secara bersamaan untuk stok terakhir, terlanjur membayar, lalu verifikator pertama sukses sementara verifikator kedua gagal `409 OUT_OF_STOCK` tanpa alur tindak lanjut maupun pencatatan pengembalian dana.

## (b) Opsi Alternatif (Minimal 2)
1. **Opsi 1 — Tetap Seperti Semula**: Tidak ada tahanan di checkout; verifikasi kedua langsung gagal `409`.
2. **Opsi 2 — Potong Stok Fisik Permanen Saat Checkout**: `stok` berkurang langsung saat checkout dan dikembalikan saat batal/kedaluwarsa. Bertentangan dengan arahan pemilik bahwa stok nyata toko hanya boleh berkurang setelah pembayaran terkonfirmasi/pesanan diproses, dan berisiko membuat staf toko bingung antara stok fisik gudang vs pesanan belum dibayar.
3. **Opsi 3 — Reservasi Stok Tertahan (`stok_tertahan`)**: Tambah kolom `produk.stok_tertahan`, `pesanan.stok_status` (`none|reserved|deducted|released`), dan `pesanan.stok_hold_expires_at` (`NOW() + STOCK_HOLD_MINUTES`). Tampilan tersedia bagi pembeli adalah `stok_tersedia = stok - stok_tertahan`. Checkout mengunci atomik baris `produk` (`stok - stok_tertahan >= qty`); persetujuan pembayaran mengonversi tahanan menjadi pengurangan `stok` fisik nyata (`ConfirmReservedProdukStock`); pembatalan atau worker periodik melepaskan tahanan yang kedaluwarsa. Jika pembayaran disetujui setelah masa tahan habis namun stok fisik ternyata habis dibeli orang lain, transaksi beralih dengan rapi ke status `perlu_tindak_lanjut` (tercatat + siap refund D2) bukan error `409` yang menggantung.

## (c) Kriteria dan Bobot Keputusan
| Kriteria | Bobot | Opsi 1 (Lama) | Opsi 2 (Potong Checkout) | Opsi 3 (Reservasi) |
| :--- | :---: | :---: | :---: | :---: |
| Keamanan uang & data pribadi | 30% | 2 | 4 | **5** |
| Kesederhanaan operasi (1 dev) | 20% | 5 | 4 | **4** |
| Kecocokan skala & UX warga desa | 20% | 1 | 4 | **5** |
| Keandalan & kemudahan pemulihan | 15% | 2 | 4 | **5** |
| Biaya & kemampuan dibalik | 15% | 5 | 4 | **4** |
| **Skor Tertimbang** | **100%** | **2.75** | **4.00** | **4.65** |

## (d) Bukti Terukur di Repositori
- `TestDecisionAudit_D1_StockReservationConcurrency50Buyers` (`internal/api/decision_audit_stock_test.go`): 50 goroutine pembeli serentak memesan produk berstok 5 menghasilkan tepat **5** checkout sukses (`stok=5`, `stok_tertahan=5`, `stok_tersedia=0`) dan **45** penolakan `409 OUT_OF_STOCK` (**PASS**, `-race`, `4.44s`–`5.4s`), sehingga 0 pembeli bisa lolos checkout melebihi stok nyata.
- `TestDecisionAudit_D1_ApprovalAfterExpiredHold_GracefulTindakLanjut` (`internal/api/decision_audit_stock_test.go`): setelah hold kedaluwarsa dilepas dan stok habis dibeli transaksi lain, persetujuan admin terhadap bukti lama mengembalikan HTTP `200 OK` dengan status `pembayaran.status = "perlu_tindak_lanjut"` (**PASS**, `-race`), siap ditindaklanjuti via alur refund D2.

## (e) Rekomendasi
**DIUBAH -> Pilih Opsi 3 (Reservasi Stok Tertahan + Fallback `perlu_tindak_lanjut`).**

## (f) Implementasi yang Dikerjakan
- Migrasi [000002_decision_audit_enhancements.up.sql](../../db/migrations/000002_decision_audit_enhancements.up.sql): `produk.stok_tertahan`, `pesanan.stok_status`, `pesanan.stok_hold_expires_at`, dan indeks parsial `idx_pesanan_stok_hold_aktif`.
- Config & Env: tambah `STOCK_HOLD_MINUTES` (default `60`, Set B `90`) di `internal/config/config.go`, `.env.example`, `.env.test`, `.env.test.alt`, dan `GET /api/v1/public/config`.
- Query `sqlc` & service: `ReserveProdukStock`, `ReleaseProdukStockHold`, `ConfirmReservedProdukStock`, `DecrementAvailableProdukStock`, `ReleaseExpiredStockHolds` di `internal/pesanan/`, serta fallback savepoint `tx.Begin` di `internal/pembayaran/verifikasi.go`.
- Pelepasan otomatis dijalankan berkala oleh `internal/worker` (`pesanan_expire`) dan `kopdes pesanan expire`.

## (g) Risiko Sisa
- Jika `STOCK_HOLD_MINUTES` disetel terlalu singkat di bawah SLA verifikasi bendahara di desa, lebih banyak pembayaran akan melewati masa tahan sebelum disetujui (termitigasi oleh `STOCK_HOLD_MINUTES` >= `PAYMENT_EXPIRY_DURATION` dalam menit dan fallback `perlu_tindak_lanjut` + D2 refund).
- Barang cepat laku selama jendela tahanan terkunci dari pembeli lain hingga `STOCK_HOLD_MINUTES` habis atau pesanan dibatalkan (**KEPUTUSAN PEMILIK** untuk menyesuaikan angka menit).
