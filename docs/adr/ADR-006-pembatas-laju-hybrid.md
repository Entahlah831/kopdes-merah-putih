# ADR-006: Pembatas Laju Hybrid (Memori LRU Lapisan-1 + Persistensi DB Ambang Batas) & Kebersihan Goroutine (D6)

- **Status**: `DIUBAH` (mengganti tulis-DB setiap permintaan publik dengan `HybridRateLimiter` + membersihkan goroutine ticker liar di `IPRateLimiter`)

## (a) Keputusan Saat Ini dan Lokasi Pembuatan
Pada branch `lean-revamp`, rute publik pendaftaran dan unggah KYC (`internal/api/routes.go:133-134`) memanggil `DBRateLimiter.Allow` (`internal/api/middleware/ratelimit.go:349`) yang mengeksekusi `INSERT ... ON CONFLICT DO UPDATE` ke tabel PostgreSQL `rate_limits` pada **setiap permintaan HTTP** — bahkan untuk serangan IP palsu/acak satu kali hit — sehingga penyerang dapat memaksa amplifikasi penulisan disk/WAL PostgreSQL. Di samping itu, `NewIPRateLimiterWithConfig` menjalankan goroutine `time.NewTicker(10*time.Minute)` tanpa mekanisme penghentian yang berpotensi membocorkan goroutine tiap instansiasi server.

## (b) Opsi Alternatif (Minimal 2)
1. **Opsi 1 — Pure `DBRateLimiter` Setiap Permintaan + Goroutine Ticker**: Amplifikasi write DB 1:1 pada flood IP palsu dan kebocoran goroutine.
2. **Opsi 2 — `HybridRateLimiter` (LRU Memori Lapisan-1 + DB `rate_limits` Lapisan-2 Hanya Saat Melewati Batas + Warmup + Cloudflare Edge Opsional)**: Hit normal dan banjir 500 IP acak sekali hit dihitung murni di LRU memori (`0` tulis PostgreSQL). Ketika sebuah key benar-benar menembus `maxHits`, status `blocked_until` ditulis ke `rate_limits` agar bertahan lintas restart; saat konstruktor dihidupkan, blokir aktif dimuat ke LRU lewat `ListActiveRateLimitBlocks`. `IPRateLimiter` membuang ticker goroutine dan menggantinya dengan `pruneStaleTail` O(1) lazy saat `Allow()`. Login lockout memakai pasangan `(ip+account)` + `account` agar aman untuk jaringan seluler CGNAT warga desa.
3. **Opsi 3 — Redis Terpisah**: Menambah container & komponen runtime baru untuk skala 100–1.000 pengunjung di 1 VPS; ditolak karena tidak perlu dan melanggar kesederhanaan operasi 1 developer.

## (c) Kriteria dan Bobot Keputusan
| Kriteria | Bobot | Opsi 1 (Pure DB) | Opsi 2 (Hybrid LRU + DB Lock) | Opsi 3 (Redis) |
| :--- | :---: | :---: | :---: | :---: |
| Keamanan uang & data pribadi | 30% | 4 | **5** | 5 |
| Kesederhanaan operasi (1 dev) | 20% | 5 | **5** | 2 |
| Kecocokan skala & UX warga desa | 20% | 2 | **5** | 4 |
| Keandalan & kemudahan pemulihan | 15% | 4 | **5** | 3 |
| Biaya & kemampuan dibalik | 15% | 4 | **5** | 3 |
| **Skor Tertimbang** | **100%** | **3.70** | **5.00** | **3.60** |

## (d) Bukti Terukur di Repositori
- `TestDecisionAudit_D6_Hybrid500FakeIPsZeroDBAmplification` (`internal/api/middleware/ratelimit_audit_test.go`, **PASS**, `-race`):
  - Banjir **500 IP palsu** berbeda ke `DBRateLimiter` murni menghasilkan **`500` tulis baris ke DB** dalam **`196.4832ms`**.
  - Skenario yang sama persis ke `HybridRateLimiter` menghasilkan **`0` tulis baris ke DB** dalam **`1.9949ms`** (~98x lebih cepat, reduksi penulisan DB 100% untuk IP sekali lewat).
- `TestDecisionAudit_D6_PersistOnThresholdAndCGNATIsolation`: IP pelanggar (`hit 4 > maxHits 3`) ditolak `429` dan tetap diblokir ketika instance `HybridRateLimiter` baru dibuat ulang dari DB, sementara IP tetangga dan akun CGNAT jujur pada IP bersama `100.64.12.34` tetap diizinkan (**PASS**).
- `TestDecisionAudit_D6_NoGoroutineLeakAndBoundedLRU`: pembuatan 8 instance `IPRateLimiter` tidak menambah goroutine (`before` == `after`) (**PASS**).

## (e) Rekomendasi
**DIUBAH -> Pilih Opsi 2 (`HybridRateLimiter` Memori + DB Threshold Persistence, Tanpa Goroutine Ticker Liar & Aman CGNAT; tambah rule edge Cloudflare saat deploy produksi).**

## (f) Implementasi yang Dikerjakan
- `internal/api/middleware/ratelimit_hybrid.go` (`HybridRateLimiter`, `warmUpActiveBlocks`, `evaluateInMemory`, `persistBlock`).
- `internal/api/middleware/ratelimit.go`: hapus background goroutine di `NewIPRateLimiterWithConfig`, tambah `pruneStaleTail`.
- `internal/auth/queries/auth.sql`: `ListActiveRateLimitBlocks`, `CleanupExpiredRateLimits` (dibersihkan berkala oleh worker D8).
- `internal/api/routes.go`: pindahkan `/api/v1/public/pendaftaran` & `/dokumen` memakai `hybridLimit`.

## (g) Risiko Sisa
- Hitungan jendela aktif sebelum menembus batas (`1..maxHits`) reset saat restart API (hanya yang sudah melanggar `blocked_until` bertahan di DB); masih memadai dan disarankan diperkuat rule WAF/Rate Limit Cloudflare di layer edge untuk serangan DDoS volumetrik (**KEPUTUSAN PEMILIK** untuk konfigurasi Cloudflare DNS/WAF).
