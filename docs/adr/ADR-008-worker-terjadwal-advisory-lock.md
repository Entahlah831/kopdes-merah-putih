# ADR-008: Layanan `worker` Bawaan di Compose dengan `pg_try_advisory_lock` & Telemetri `/readyz` (D8)

- **Status**: `DIUBAH` (beralih dari ketergantungan cron host + `docker compose exec` eksternal ke layanan `worker` terintegrasi di `docker-compose*.yml`)

## (a) Keputusan Saat Ini dan Lokasi Pembuatan
Pada branch `lean-revamp`, penjadwalan `pesanan expire`, `iuran generate`, dan `pdp purge` bergantung sepenuhnya pada `crontab` host di luar repo yang memanggil `docker compose exec api /app/kopdes ...`. Pola ini sulit dipindahkan antar-VPS, tidak memiliki pemantauan kesehatan tugas di `/readyz`, belum memiliki penguncian tunggal jika kontainer ganda berjalan bersamaan, serta gagal diam-diam tanpa terlihat aplikasi.

## (b) Opsi Alternatif (Minimal 2)
1. **Opsi 1 — Tetap Cron di Host Tanpa Status**: Konfigurasi terpecah di luar Compose/repo dan kegagalan tidak terpantau dari API.
2. **Opsi 2 — Layanan `worker` Bawaan di Compose (`kopdes worker --tick 60s`) + PostgreSQL Session Advisory Lock + Tabel `worker_runs` + `/readyz`**: Proses yang sama (image Dockerfile sama, `restart: unless-stopped`) menjalankan siklus 5 tugas (`pesanan_expire`, `iuran_generate`, `pdp_purge`, `bukti_purge`, `rate_limits_cleanup`). Setiap tugas memperoleh 1 koneksi `*pgxpool.Conn` persisten untuk `pg_try_advisory_lock`/`pg_advisory_unlock` via `sqlc`, menulis telemetry ke `worker_runs`, dan diekspos ke endpoint `/readyz` (`worker_jobs`, `worker_degraded`).
3. **Opsi 3 — Cron Host + Script Monitoring Eksternal**: Tetap tersebar di luar container Compose dan masih membutuhkan instalasi wrapper pemantau di host VPS.

## (c) Kriteria dan Bobot Keputusan
| Kriteria | Bobot | Opsi 1 (Cron Host) | Opsi 2 (Compose `worker` + Lock) | Opsi 3 (Cron + Monitor) |
| :--- | :---: | :---: | :---: | :---: |
| Keamanan uang & data pribadi | 30% | 3 | **5** | 4 |
| Kesederhanaan operasi (1 dev) | 20% | 3 | **5** | 3 |
| Kecocokan skala & UX warga desa | 20% | 4 | **5** | 4 |
| Keandalan & kemudahan pemulihan | 15% | 2 | **5** | 3 |
| Biaya & kemampuan dibalik | 15% | 5 | **5** | 4 |
| **Skor Tertimbang** | **100%** | **3.35** | **5.00** | **3.65** |

## (d) Bukti Terukur di Repositori
- `TestDecisionAudit_D8_TwoConcurrentWorkersNoDuplicateBills` (`internal/worker/worker_test.go`, **PASS**, `-race`, `1.08s`): menjalankan 2 instance `Worker.RunOnce` secara serentak terhadap DB nyata menghasilkan 0 duplikasi `tagihan_iuran` (`COUNT(*) == COUNT(DISTINCT ...)`).
- `TestDecisionAudit_D8_CrashRestartCatchUpAndReadyz` (`internal/worker/worker_test.go`, **PASS**, `-race`, `1.07s`): menghentikan paksa worker #1 di tengah loop, menghidupkan worker #2 yang otomatis mengejar ketertinggalan (*catch-up*) melepaskan `stok_tertahan` kedaluwarsa (`2 -> 0`, `stok_status -> released`), mengisi ke-5 baris `worker_runs`, dan memverifikasi `/readyz` menampilkan `worker_jobs` serta mendeteksi `worker_degraded: true` saat ada job berstatus `error`.

## (e) Rekomendasi
**DIUBAH -> Pilih Opsi 2 (Layanan `worker` Bawaan di Compose dengan Single-Conn Advisory Lock & Telemetri `/readyz`).**

## (f) Implementasi yang Dikerjakan
- Migrasi `000002`: tabel #21 `worker_runs` + trigger `updated_at`.
- `internal/worker/queries/worker.sql` (`TryAdvisoryLock`, `AdvisoryUnlock`, `UpsertWorkerRunStart`, `UpsertWorkerRunFinish`, `ListWorkerRuns`) + `sqlc generate`.
- `internal/worker/worker.go`, subperintah `kopdes worker [--tick 60s] [--once]` di `cmd/kopdes/main.go`.
- `internal/api/handlers/health_handler.go` (`/readyz` memuat `worker_jobs` & `worker_degraded`) dan service `worker` di `docker-compose.yml` & `docker-compose.test.yml`.

## (g) Risiko Sisa
- Backup harian (`scripts/backup.sh` memakai `pg_dump` host/container dan `age`) tetap dijalankan terpisah melalui cron/timer host VPS karena menyangkut jadwal transfer off-server dan menjaga isolasi container aplikasi.
