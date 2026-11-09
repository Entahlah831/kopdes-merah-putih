# Workspace Rules: Kopdes Merah Putih (PostgreSQL 16 & Go)

## 1. Aturan Bisnis & Integritas Data Inti
1. **Multi-Tenant & RLS**: Setiap transaksi pgx wajib menjalankan `SELECT set_config('app.current_koperasi_id', $1, true)` sebelum query bisnis. Nilai `koperasi_id` wajib diekstrak murni dari token JWT terverifikasi, DILARANG mengambil dari parameter URL atau body JSON klien (anti-BOLA OWASP API1:2023).
2. **Tipe Uang & PK**: Wajib gunakan `NUMERIC(18,2)` di SQL (dilarang `float`) dan `uuid_generate_v7()` untuk Primary Key. Di Go, gunakan `int64` (sen/Rupiah utuh) atau `string` desimal.
3. **Tabel Append-Only**: Dilarang membuat query `UPDATE` atau `DELETE` pada `audit_log`, `pendaftaran_status_log`, `anggota_status_log`, `kas_mutasi`, `stok_mutasi`, dan `pembayaran_verifikasi_log`. Koreksi wajib melalui entri reversal.
4. **QRIS Statis & Maker-Checker**: Kode unik QRIS (1..999) dihitung dengan HMAC-SHA256 deterministik + linear probing. Verifikator pembayaran (`diverifikasi_oleh`) tidak boleh sama dengan pembuat pembayaran (`pembayar_user_id`).
5. **Data Pribadi (UU 27/2022 PDP)**: Kolom NIK, tanggal lahir, alamat, email, dan nomor HP wajib disimpan dalam pasangan `*_enkripsi` (AES-256-GCM) dan `*_hash` (HMAC-SHA256).

## 2. Guardrails Arsitektur Hibrida & Batasan Agen AI
1. **Kunci Folder Fondasi Keamanan (Security Core Locked)**: Agen DILARANG MENGUBAH file di dalam `db/migrations/`, `internal/security/`, dan `internal/auth/` tanpa instruksi eksplisit dari pemilik proyek. Agen wajib menggunakan wrapper `middleware.WithTenantTx` yang sudah disediakan.
2. **Mode Eksekusi Pustaka Pihak Ketiga**: Jangan memasang dependensi Go eksternal baru (`go get`) tanpa persetujuan eksplisit pengguna.
3. **Pertahanan Berlapis di PostgreSQL 16 (Defense-in-Depth)**: Mengandalkan `FORCE ROW LEVEL SECURITY`, trigger append-only, dan konstrain CHECK di PostgreSQL sebagai jaring pengaman terakhir atas potensi kesalahan logika API.
4. **Verifikasi & Pemindaian Otomatis**: Setiap pembuatan atau modifikasi endpoint API baru wajib diverifikasi dengan `go test ./...` dan pemindaian celah keamanan `govulncheck ./...`.
