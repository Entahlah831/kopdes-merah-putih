# Aturan Workspace: Kopdes Merah Putih (Backend Ramping)

1. **Lingkup Koperasi Tunggal**: Backend ini hanya untuk **1 koperasi di 1 lokasi** (anggota, pendaftaran + KYC terenkripsi, iuran bulanan QRIS statis/tunai, e-katalog + checkout tanpa keranjang server, pengantaran daftar antar/label, pengaturan, laporan). Dilarang membuat fitur multi-tenant/RLS, SIMKOPDES, simpanan/pinjaman/SHU, double-entry, impor CSV mutasi bank, atau QRIS dinamis EMVCo.
2. **Aturan Berkas Lingkungan (`.env`)**:
   - Dilarang keras membaca, mencetak, mengubah, atau mengomit `.env`, `.env.local`, `.env.production`, `.env.live`, atau folder `secrets/`.
   - Gunakan hanya `.env.example` (template kosong), `.env.test` (Set A), `.env.test.alt` (Set B), dan utilitas `cmd/envcheck`.
   - Tidak ada literal port atau secret di kode Go, HTML/JS, Docker, skrip, atau dokumentasi.
3. **Standar Kode & Database**:
   - Satu berkas handler maksimal 400 baris, satu fungsi maksimal 60 baris, struktur per domain (`internal/<domain>/`).
   - Semua query SQL lewat `sqlc`; jangan tulis SQL mentah di handler dan jangan mengedit `internal/db/*.go` secara manual.
   - Koneksi database aplikasi wajib role non-superuser (`kopdes_app`).
4. **Perintah Verifikasi Harian**:
   - Generate SQL: `sqlc generate && git diff --exit-code`
   - Validasi Env: `go run ./cmd/envcheck --strict --ports --no-listen --env-file .env.test`
   - Build & Vet: `go build ./... && go vet ./...`
   - Tes: `go test -count=1 -race ./...` (atau `-short` jika DB pengujian lokal belum berjalan)
   - Keamanan: `staticcheck ./...`, `gosec ./...`, `govulncheck ./...`, `gitleaks git --config .gitleaks.toml --verbose --redact`
5. **Aturan Commit & Bukti**:
   - Satu kelompok perubahan = satu commit kecil dengan pesan jelas di branch `lean-revamp`. Jangan force push.
   - Tes gagal dulu untuk fitur/bug baru, lalu perbaiki dan tempel output asli perintah.
6. **Skill Wajib Dibaca Sebelum Menulis Kode**:
   - [lean-go-backend](.agents/skills/lean-go-backend/SKILL.md)
   - [kopdes-domain-rules](.agents/skills/kopdes-domain-rules/SKILL.md)
   - [security-baseline-kopdes](.agents/skills/security-baseline-kopdes/SKILL.md)
