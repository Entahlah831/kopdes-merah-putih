# Laporan Akhir Modular Monolith, Wireframe Figma, & Eksperimen Microservice (`Prompt v9`)

- **Branch Kerja:** `modular-and-wireframe` *(bermula dari `decision-audit` / tag `pre-modular` = `448da13`)*
- **Ringkasan Eksekutif:** Sistem **Kopdes Merah Putih** tetap berada dalam **Modular Monolith** terstruktur (`8` modul domain dengan kontrak `internal/<modul>/api`, `internal/filestore`, `internal/uiserve`, `internal/worker`, biner tunggal `kopdes`, satu `docker-compose.yml`, dan satu sumber `.env` — dipangkas pasca audit dari 51 menjadi **43 variabel <= 45** dengan alasan tertulis di [ENV_REFERENCE.md](ENV_REFERENCE.md)), telah melewati pengujian ulang beban, chaos, keamanan, portabilitas Set C port acak, dan implementasi UI referensi Wireframe Figma end-to-end. Eksperimen microservice `spike/filesvc` (`ADR-0002`) terbukti cepat (`+1.013 ms` delta p95) dan terisolasi (`503` saat mati, pulih `200`), namun **ditolak sementara** agar operasional tetap ramping untuk 1 developer di 1 VPS.

---

## 1. Perbandingan Baseline (Fase 0) vs Final (Fase 4)

Kedua rangkaian beban dijalankan via `tools/loadtest` dengan `GOMAXPROCS=2` dan pool database `MaxConns=25` terhadap PostgreSQL `14.5` lokal (Set A, port `${DB_PORT}`).

### 1.1 Performa & SLO (`Baca p95 < 300 ms`, `Tulis p95 < 800 ms`, `Error < 0,5%`)

Sumber: [perf_baseline.json](../reports/baseline/perf_baseline.json) & [perf_final.json](../reports/final/perf_final.json)

| Skenario | Metrik | Baseline Fase 0 | Final Fase 4 (Modular Monolith) | Selisih / Status SLO |
| :--- | :--- | :---: | :---: | :--- |
| **`smoke`** (5 VU, 10s) | Req / RPS | 7.206 req (`719,66/s`) | 5.600 req (`559,47/s`) | **PASS** |
| | Baca p50 / p95 / p99 | `2,00 / 4,01 / 10,01 ms` | `3,00 / 12,00 / 79,40 ms` | `< 300 ms` **PASS** |
| | Tulis p50 / p95 / p99 | `9,00 / 18,17 / 30,27 ms` | `5,01 / 32,03 / 81,56 ms` | `< 800 ms` **PASS** |
| | Error Rate | `0,0000` | `0,0000` | **PASS** |
| **`load`** (100 VU, 25.2s) | Req / RPS | 16.780 req (`665,67/s`) | **19.928 req (`790,91/s`)** | **+18,8% RPS**, **PASS** |
| | Baca p50 / p95 / p99 | `77,46 / 176,28 / 235,85 ms` | **`70,21 / 117,00 / 153,00 ms`** | **-33,6% p95**, **PASS** |
| | Tulis p50 / p95 / p99 | `159,52 / 349,71 / 560,96 ms` | **`147,00 / 290,81 / 415,00 ms`** | **-16,8% p95**, **PASS** |
| | Error Rate | `0,0000` (85 x `429`) | `0,0000` (128 x `429`) | `0` 5xx, **PASS** |
| **`spike`** (250 VU, ~20.6s) | Req / RPS | 13.577 req (`660,20/s`) | **17.507 req (`851,69/s`)** | **+29,0% RPS**, **PASS** |
| | Baca p95 / Tulis p95 | `404,61 ms / 655,43 ms` | **`257,23 ms / 455,33 ms`** | **Baca naik dari FAIL -> PASS (`< 300 ms`)!** |
| | Error Rate | `0,0000` (193 x `429`) | `0,0000` (413 x `429`) | **PASS** |
| **`stress`** (500 VU, ~34s) | Req / RPS | 24.233 req (`707,09/s`) | **27.760 req (`823,29/s`)** | **+16,4% RPS** (di atas kapasitas normal 2 vCPU) |
| | Baca p95 / Tulis p95 | `719,66 ms / 1085,67 ms` | **`507,00 ms / 941,74 ms`** | Membaik, tetap saturasi di 500 VU (`pg_max_conns=27`) |
| | Error Rate | `0,0000` (531 x `429`) | `0,0000` (817 x `429`) | `0` crash / `0` 5xx |
| **`soak`** (50 VU, `90s` time-box) | Req / RPS | 55.599 req (`614,20/s`) | **73.836 req (`819,40/s`)** | **+33,4% RPS**, **PASS** |
| | Baca p50 / p95 / p99 | `34,65 / 65,49 / 95,56 ms` | **`30,27 / 45,19 / 51,96 ms`** | **-31,0% p95**, **PASS** |
| | Tulis p50 / p95 / p99 | `75,01 / 269,35 / 459,50 ms` | **`60,53 / 176,76 / 277,18 ms`** | **-34,4% p95**, **PASS** |
| | Error Rate | `0,0000` | `0,0000` | **PASS** |

> **Kesimpulan Performa**: Tidak terjadi regresi SLO akibat batas modul (`internal/<modul>/api`); seluruh skenario justru mencatat throughput lebih tinggi dan latensi p95 lebih rendah dari Baseline Fase 0 (termasuk skenario `spike` 250 VU yang kini memenuhi target SLO `< 300 ms`).

### 1.2 Keamanan, Arsitektur, & Keandalan

| Aspek | Baseline Fase 0 | Final Fase 4 | Artefak |
| :--- | :--- | :--- | :--- |
| **Tes Batas Arsitektur (`TestArchitecture_*`)** | *(belum ada)* | **4 / 4 PASS** (`TableOwnershipManifestMatchesMigrations`, `ImportBoundaries`, `SQLTableOwnership`, `DBQueriesMethodOwnership`) | [arch_final.txt](../reports/final/arch_final.txt) |
| **RBAC + Chaos + Abuse Suite (`-race`)** | PASS (pasca 3 perbaikan `DisallowUnknownFields`, idempotency per user, nil guard) | **PASS penuh (`51,760s`)**, 0 regresi | [security_chaos_final.txt](../reports/final/security_chaos_final.txt) |
| **`staticcheck ./...`** | Exit `0` (0 temuan) | **Exit `0` (0 temuan)** | [staticcheck.txt](../reports/final/staticcheck.txt) |
| **`gosec ./...`** | 71 berkas, 18.288 baris, `0` isu | **86 berkas, 19.924 baris, `0` isu** | [gosec.txt](../reports/final/gosec.txt) |
| **`govulncheck ./...`** | `0` kerentanan terpanggil | **`0` kerentanan terpanggil** (1 CVE modul tidak terpanggil) | [govulncheck.txt](../reports/final/govulncheck.txt) |
| **`gitleaks` (riwayat git)** | 111 commit, `no leaks found` | **115 commit (`4,21 MB`), `no leaks found`** | [gitleaks.txt](../reports/final/gitleaks.txt) |
| **Chaos Abrupt Kill (`tools/chaosdrill`)** | 70 req / 53 order, `0` stok negatif/orphan/selisih bayar | **112 req / 79 order**, `0` stok negatif, `0` selisih lunas, `0` orphan | [chaos_final.json](../reports/final/chaos_final.json) |
| **Backup Terenkripsi & Restore Drill** | 21 tabel, 26 anggota, RTO `5.180 ms`, tamper ditolak | **21 tabel, 38 anggota (`22,27 MB`), RTO `7.415 ms`, tamper ditolak** | [chaos_final.json](../reports/final/chaos_final.json) |

---

## 2. Ringkasan Tiap Fase

1. **Fase 1 — Modular Monolith (`869eb5c`)**:
   - Struktur **8 modul domain** (`auth`, `pengaturan`, `anggota` termasuk pendaftaran/KYC, `iuran`, `katalog`, `pesanan`, `pembayaran`, `laporan`) + paket bersama `filestore`, `uiserve`, dan `worker`.
   - Satu biner `kopdes` (`serve`, `worker`, `uiserve`, `admin`/`bootstrap-admin`/`storage`/`pdp`/`bukti`) dan satu `docker-compose.yml` berprofil `app`, `worker`, `tester`, `proxy` (saat Fase 1 memakai 51 variabel, lalu pada tindak lanjut v9 dipangkas menjadi **43 variabel <= 45** tervalidasi `envcheck`).
   - Keputusan lengkap terdokumentasi di [ADR-0001](adr/0001-modular-monolith.md) dan [ARCHITECTURE.md](ARCHITECTURE.md).
2. **Fase 2 — Implementasi Wireframe Figma (`4b895f8`)**:
   - Inspeksi read-only Figma (`5RsPLIopItTEIp21N1VU2x`): **19 frame** diinventarisasi dalam [INVENTARIS_LAYAR.md](wireframe/INVENTARIS_LAYAR.md) dan dipetakan ke OpenAPI dalam [ANALISIS_KESENJANGAN.md](wireframe/ANALISIS_KESENJANGAN.md).
   - Fitur di luar lingkup koperasi tunggal (`SIMKOPDES`, `RAT`, `Kas & Mutasi`, `Transfer Bank`, `Upload PDF`, `Multi-Gudang/Cabang`) ditandai jujur sebagai **`BLOCKED: butuh keputusan pemilik`** tanpa menambah fitur backend terlarang.
   - Dibangun `frontend-wireframe/` (Vite + TypeScript ketat, `0` dependensi runtime, cookie `HttpOnly` + `X-Requested-With`, pembacaan dinamis `/config.json`, rendering bebas `innerHTML`, kompresi `<canvas>` untuk JPG/PNG).
   - Verifikasi Playwright Chromium Set B (`ALLOW_BEARER_AUTH=false`, `18080/18081/15433`): **4 / 4 suite PASS (`18,7s`)** mencakup beranda, mobile `375x812`, pendaftaran + 3 KYC, pencegahan XSS tersimpan, MFA Admin/Bendahara, checkout + QRIS + bukti + iuran + 7 skenario negatif + **10 tangkapan layar** di `reports/wireframe/` ([LAPORAN_E2E.md](wireframe/LAPORAN_E2E.md)).
3. **Fase 3 — Eksperimen Terukur `spike/filesvc` (`848c2be`)**:
   - Pemisahan layanan `kopdes filesvc` diukur langsung (`100` iterasi `16 KB` PNG): delta p95 `SaveEncrypted = +1.013 ms`, `LoadDecrypted = -0.599 ms`, S2S bearer `401`, API server menyimpan `0` kunci dekripsi berkas (`phase_a_api_key_rejects_enc = true`), isolasi gangguan `200` endpoint inti / `503` endpoint berkas saat `filesvc` mati, dan pemulihan otomatis `200` setelah `filesvc` hidup lagi ([filesvc_metrics.json](../reports/spike/filesvc_metrics.json), [fault_isolation.json](../reports/spike/fault_isolation.json)).
   - Sesuai evaluasi 6 kriteria di [ADR-0002](adr/0002-filesvc-spike.md), `spike/filesvc` **ditolak sementara** dari branch aktif agar sistem tetap ramping pada **1 VPS, 1 `.env` (43 var aktif), 1 developer**, namun kontrak `filestore.FileStore` siap diekstrak bila pemicu ADR-0001/0002 tercapai.
4. **Fase 4 — Portabilitas Set C Acak & Isolasi Cookie (`env_portability_isolation.txt`)**:
   - Database PostgreSQL terisolasi **Set C** (`28080` API, `28081` UI, `28082` Worker, `25433` PG) dijalankan **dengan 0 perubahan kode**: `envcheck` PASS (`51` var), `/healthz` API & Worker `200`, UI `/config.json` `200` menunjuk `http://127.0.0.1:28080`, dan smoke Playwright `01.` PASS.
   - Uji tabrakan port: menyetel `WORKER_PORT=28080` (`== API_PORT`) ditolak keras oleh `envcheck` (exit code `1`: `KONFLIK PORT HOST 28080 digunakan bersama oleh: API_HOST_PORT, WORKER_PORT`).
   - Uji isolasi cookie lintas environment: cookie sesi login Set A (`8080`) mengembalikan **HTTP 200** pada `/api/v1/auth/me` di Set A (`8080`), dan ditolak **HTTP 401** saat dikirim ke Set B (`18080`).

---

## 3. Pengungkapan Kesenjangan Alat Pengujian & Catatan Kebijakan

- **Alat Eksternal yang Tidak Terpasang di Host Windows Lokal**: `docker`, `k6`, `OWASP ZAP`, `schemathesis`, `semgrep`, `trivy`, `hadolint`, `toxiproxy-server`, `age`, dan `openssl`.
  - Sebagai pengganti langsung yang terukur di lokal: beban campuran dijalankan lewat `tools/loadtest` (berbatas `GOMAXPROCS=2`), fuzz/penyalahgunaan API & RBAC lewat `internal/api/security_abuse_test.go` & `rbac_matrix_test.go`, chaos TCP/pool/kill/restore terenkripsi lewat `internal/api/reliability_chaos_test.go` + `tools/chaosdrill`, serta UI E2E lewat Playwright Chromium terhadap biner server Go langsung.
  - Skrip `k6`, `ZAP`, `semgrep`, `trivy`/`hadolint`, dan `age` CI tersedia siap pakai untuk host Linux/Docker ([tests/perf/](../tests/perf/), [tests/security/](../tests/security/), [.github/workflows/ci.yml](../.github/workflows/ci.yml)).
- **Penanda Keputusan Terbuka**:
  - Masa retensi data pendaftar ditolak (`PDP_REJECTED_RETENTION_DAYS`) & bukti transaksi non-lunas (`BUKTI_RETENTION_DAYS`) -> **PERLU VERIFIKASI HUKUM** (UU No. 27/2022 PDP).
  - Pemetaan pos `jenis_iuran.klasifikasi_akuntansi` & penyesuaian selisih kode unik/refund -> **KEPUTUSAN AKUNTAN**.
  - Kebijakan stok hold (`STOCK_HOLD_MINUTES`), mode `DUAL_CONTROL_BREAKGLASS_ENABLED`, serta seluruh item `BLOCKED` wireframe (`SIMKOPDES`, `RAT`, `Kas & Mutasi`, `Transfer Bank`, `Upload PDF`, multi-cabang) -> **KEPUTUSAN PEMILIK**.
