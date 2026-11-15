---
name: lean-go-backend
description: Gunakan saat menulis, merefaktor, atau meninjau kode backend Go Kopdes (handler HTTP, service domain, query sqlc, CLI, middleware, dan pengujian unit/integrasi) agar tetap ramping, idiomatis, tanpa SQL mentah di handler, dan bebas bloat.
---

# Aturan Backend Go Ramping (`lean-go-backend`)

## 1. Batas Ukuran & Struktur Paket
- **Satu berkas handler maksimal 400 baris** dan **satu fungsi maksimal 60 baris**. Pecah logika panjang ke fungsi pembantu yang jelas dan tunggal tujuannya dengan pengembalian dini (*early return*).
- **Struktur per domain**: `internal/<domain>/handler.go`, `internal/<domain>/service.go`, dan `internal/<domain>/queries/<domain>.sql`. Jangan membuat lapisan abstraksi (*repository wrapper*) yang hanya meneruskan pemanggilan tanpa logika.
- **Tanpa SQL mentah di handler atau service**: Semua query database wajib ditulis di berkas `*.sql` bertanda `-- name:` dan dikompilasi memakai `sqlc`.
- **Berkas hasil `sqlc` (`internal/db/*.go`) DILARANG diedit tangan**. Wajib diverifikasi lewat `sqlc generate && git diff --exit-code`.
- **Interface minimal**: Buat interface Go HANYA jika ada minimal 2 implementasi nyata atau kebutuhan *mock/stub* yang konkret (contoh: `FileStore` untuk penyimpanan KYC terenkripsi). Jangan membuat interface satu-implementasi untuk setiap service.
- **Tidak ada kode "untuk jaga-jaga"**: Dilarang menambah tabel, endpoint, variabel `env`, helper, atau paket spekulatif yang belum dipakai alur klien.

## 2. Penanganan Error, Konteks, & Konkurensi
- **Bungkus error dengan `%w`**: Selalu kembalikan error dengan konteks jelas (`fmt.Errorf("gagal ...: %w", err)`) dan jangan pernah mengabaikan nilai balik error (`_` hanya boleh untuk aksi terbaik yang benar-benar non-kritis dan dicatat alasannya).
- **Konteks & batas waktu pada setiap I/O**: Teruskan `ctx context.Context` ke seluruh panggilan DB, HTTP eksternal, dan disk I/O; terapkan timeout eksplisit (`context.WithTimeout`).
- **Tanpa panic di jalur request**: Handler HTTP tidak boleh memanggil `panic`; tangani kesalahan secara eksplisit ke respons HTTP yang aman.
- **Goroutine terkendali**: Dilarang meluncurkan goroutine latar tanpa mekanisme penghentian deterministik (`ctx.Done()` + `sync.WaitGroup`). Operasi periodik berat (tagihan bulanan, retensi PDP, kedaluwarsa pesanan) dijalankan melalui CLI `cmd/kopdes` yang dipanggil penjadwal OS.

## 3. Pengujian & Anti-Bloat
- **Tes table-driven**: Gunakan *table-driven tests* untuk logika murni (kode unik, hitung harga/diskon, CSV formula neutralization, mesin status) dan pengujian integrasi HTTP + DB nyata untuk alur end-to-end serta regresi keamanan.
- **Uji kolom nullable (`LEFT JOIN`)**: Setiap query yang memakai `LEFT JOIN` wajib diuji dengan baris yang tidak memiliki relasi (nilai `NULL`) agar tidak gagal saat scanning Go.
- **Daftar Anti-Bloat (DILARANG)**:
  1. Helper satu baris yang hanya dipanggil sekali dan tidak memperjelas maksud.
  2. Generik Go yang tidak perlu ketika tipe konkret domain sudah jelas.
  3. Variabel konfigurasi/feature-flag baru untuk perilaku statis yang tidak berubah per lingkungan.
  4. Paket atau *worker pool* internal untuk skala 100–1.000 pengunjung di satu koperasi.
  5. Dependensi eksternal baru tanpa alasan tertulis di `docs/REVAMP_PLAN.md` (pertahankan: stdlib + `pgx/v5`, kode hasil `sqlc`, `golang.org/x/crypto`, `google/uuid`).
