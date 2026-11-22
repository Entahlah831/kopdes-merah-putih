# ADR-002: Pencatatan Pengembalian Dana (Refund) Manual Dual-Control (D2)

- **Status**: `DIUBAH` (fitur pencatatan pengembalian dana manual belum ada sebelumnya)
- **Tag Kebijakan**: **KEPUTUSAN PEMILIK** (kebijakan pengembalian dana tunai/transfer manual di luar sistem)

## (a) Keputusan Saat Ini dan Lokasi Pembuatan
Pada branch `lean-revamp`, belum ada status pembayaran `dikembalikan`, tabel/endpoint pengembalian dana, maupun mekanisme membatalkan pesanan lunas beserta pengembalian stoknya jika stok habis atau pesanan dibatalkan sesudah pembayaran disetujui.

## (b) Opsi Alternatif (Minimal 2)
1. **Opsi 1 — Tidak Ada Fitur Refund di Aplikasi**: Semua pengembalian dicatat hanya di buku tulis manual; laporan aplikasi tetap menghitung omzet/kas masuk lunas meski uang dikembalikan.
2. **Opsi 2 — Pencatatan Refund Manual Dual-Control + Bukti Terenkripsi Opsional**: Tambah status pembayaran `dikembalikan` dan tabel #20 `pengembalian_dana`, satu endpoint `POST /api/v1/pembayaran/{id}/refund` (`aksi: ajukan|setujui|tolak`), metode manual (`tunai|transfer`), alasan wajib, unggahan bukti transfer refund terenkripsi opsional (`STORAGE_BUKTI_DIR`), aturan dual-control (pemohon != pelaksana), pengembalian stok & kuota promo jika pesanan dibatalkan, pencatatan di `audit_log`, serta kolom `total_dikembalikan` di laporan pembayaran/penjualan.
3. **Opsi 3 — Integrasi Otomatis Payment Gateway Disbursal**: Menghubungkan API transfer bank/aggregator otomatis. Ditolak karena di luar lingkup koperasi tunggal, berbiaya gateway, dan menambah risiko finansial besar bila kunci API bocor.

## (c) Kriteria dan Bobot Keputusan
| Kriteria | Bobot | Opsi 1 (Buku Manual) | Opsi 2 (Manual Dual-Control) | Opsi 3 (Gateway) |
| :--- | :---: | :---: | :---: | :---: |
| Keamanan uang & data pribadi | 30% | 2 | **5** | 2 |
| Kesederhanaan operasi (1 dev) | 20% | 4 | **5** | 1 |
| Kecocokan skala & UX warga desa | 20% | 2 | **5** | 2 |
| Keandalan & kemudahan pemulihan | 15% | 2 | **5** | 3 |
| Biaya & kemampuan dibalik | 15% | 4 | **5** | 2 |
| **Skor Tertimbang** | **100%** | **2.70** | **5.00** | **2.00** |

## (d) Bukti Terukur di Repositori
- `TestDecisionAudit_D2_ManualRefundFlowAndReports` (`internal/api/decision_audit_refund_breakglass_test.go`):
  1. `bendahara` mengajukan refund (`201 diajukan`), ditolak `403 DUAL_CONTROL_VIOLATION` ketika mencoba menyetujui sendiri.
  2. `admin` menyetujui beserta unggah bukti transfer PNG (`200 selesai`).
  3. Status `pembayaran` menjadi `dikembalikan`, pesanan dibatalkan dan `produk.stok` pulih `3 -> 5` (`stok_tertahan=0`).
  4. Berkas `refund_<id>.enc` tersimpan di `STORAGE_BUKTI_DIR` tanpa header PNG plaintext dan terdekripsi utuh lewat `GET /api/v1/pembayaran?refund_bukti_id=...`.
  5. Laporan penjualan turun kembali senilai transaksi batal, dan `GET /api/v1/laporan?jenis=pembayaran` memuat status `dikembalikan` & `total_dikembalikan` (**PASS**, `-race`).

## (e) Rekomendasi
**DIUBAH -> Pilih Opsi 2 (Pencatatan Refund Manual Dual-Control Tanpa Payment Gateway).**

## (f) Implementasi yang Dikerjakan
- Migrasi `000002`: status pembayaran `dikembalikan` & tabel `pengembalian_dana` (+ `GRANT` `kopdes_app`).
- `internal/pembayaran/refund.go`, `handler.go`, `queries/pembayaran.sql`, dan endpoint #36 `POST /api/v1/pembayaran/{id}/refund` (`bendahara|admin`).
- `internal/laporan/queries/laporan.sql` & `service.go` menambah agregasi `total_dikembalikan` pada laporan JSON & CSV.

## (g) Risiko Sisa
- Keterlambatan transfer riil di bank vs pencatatan di sistem bergantung pada disiplin bendahara/admin mengunggah bukti transfer yang valid (**KEPUTUSAN PEMILIK** untuk SOP kas fisik & mutasi rekening bank).
