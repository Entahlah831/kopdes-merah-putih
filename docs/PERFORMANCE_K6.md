# Laporan Uji Beban k6 (`200 VU` Campuran Baca/Tulis per Endpoint)

Dokumen ini mendokumentasikan skenario, konfigurasi beban, rasio tulis, dan hasil pengukuran per endpoint untuk **Kopdes Merah Putih** pada infrastruktur CI PostgreSQL 16 (Set A), menggunakan skrip [`tests/perf/k6_mixed.js`](../tests/perf/k6_mixed.js) dan agregator [`tests/perf/summarize_k6.py`](../tests/perf/summarize_k6.py).

> **Batasan Target Uji:** Pengujian k6 ini dijalankan **hanya** terhadap stack lokal Set A dengan data sintetis dan akun uji seeder (`--env-file .env.test`), tidak pernah diarahkan ke host pihak ketiga atau produksi.

---

## 1. Spesifikasi Skenario & Parameter Beban

Target operasional koperasi tunggal adalah **100–300 pengunjung/pengguna aktif** dalam satu VPS. Konfigurasi CI menjalankan dua fase berurutan pada satu instance biner `kopdes serve` dan PostgreSQL 16:

| Parameter | `smoke` (Pemanasan & Kewarasan) | `load` (Beban Target SLO) |
| :--- | :--- | :--- |
| **Tujuan** | Verifikasi fungsional penuh seluruh alur baca & tulis sebelum beban tinggi | Mengukur `p50`/`p95`/`p99` tiap endpoint di bawah beban **200 VU** serentak (`100–300` pengguna aktif) |
| **Jumlah VU Baca** | `5` VU konstan (`executor: constant-vus`, fungsi `runRead`) | `20 -> 101 -> 180 -> 0` VU (`ramping-vus`, puncak **180 VU** baca) |
| **Jumlah VU Tulis** | `2` VU konstan (`executor: constant-vus`, fungsi `runWrite`) | `2 -> 10 -> 20 -> 0` VU (`ramping-vus`, puncak **20 VU** tulis serentak pada **30 slot** akun pembeli rotasi) |
| **Total VU Puncak** | **`7` VU** | **`200` VU** (`K6_LOAD_READ_VUS=180`, `K6_LOAD_WRITE_VUS=20`) |
| **Durasi** | `15s` | `10s` ramp awal + `35s` beban stabil/puncak + `5s` ramp-down = **`50s`** |
| **Batas Rate Limiter Uji** | Default Set A (`30 rps/IP`, VU mensimulasikan IP warga terpisah `10.10.x.y`/`10.20.x.y`) | Sama dengan Set A (`30 rps/IP`, `TRUSTED_PROXIES=127.0.0.1/32`) |
| **Ambang SLO (`thresholds`)** | `kopdes_read_duration p(95) < 300ms`, `kopdes_write_duration p(95) < 800ms`, `kopdes_unexpected_error < 0.5%` | Sama |

*(Skenario tambahan `stress` hingga `450+20` VU, `spike` hingga `230+20` VU, dan `soak` `2h` juga tersedia melalui variabel `SCENARIO` di [`tests/perf/k6_mixed.js`](../tests/perf/k6_mixed.js).)*

---

## 2. Campuran Endpoint & Persentase Tulis Nyata

Setiap iterasi `runRead` mengeksekusi **6 permintaan baca** lalu jeda `sleep(2.5)`, sedangkan setiap iterasi `runWrite` mengeksekusi **4 permintaan tulis transaksional berurutan** lalu jeda `sleep(0.8)`:

1. **Kelompok Baca (6 Endpoint — Akun `anggota` + `bendahara` untuk laporan)**:
   - `GET /api/v1/public/config` (`ep_public_config`)
   - `GET /api/v1/katalog/produk` (`ep_katalog_produk`)
   - `GET /api/v1/iuran/tagihan` (`ep_iuran_tagihan`)
   - `GET /api/v1/pesanan` (`ep_pesanan_list`)
   - `GET /api/v1/pembayaran` (`ep_pembayaran_list`)
   - `GET /api/v1/laporan?jenis={anggota|tunggakan|penjualan|pembayaran}` (`ep_laporan`)
2. **Kelompok Tulis Transaksional Penuh (4 Endpoint — 30 Akun Pembeli + Staf `bendahara` ber-MFA)**:
   - `POST /api/v1/toko/checkout` (`ep_checkout`) dengan `Idempotency-Key` unik -> menahan stok atomik (`stok_tertahan`)
   - `POST /api/v1/pembayaran` (`ep_pembayaran_create`, JSON) -> menghitung nominal server + kode unik QRIS
   - `POST /api/v1/pembayaran` (`ep_bukti_upload`, `multipart/form-data`) -> mengunggah PNG `1x1` dengan chunk `tEXt` + CRC32 unik setiap iterasi agar lolos `ValidateImage` dan anti-duplikat SHA-256, lalu dienkripsi ke `STORAGE_BUKTI_DIR`
   - `POST /api/v1/pembayaran/{id}/verifikasi` (`ep_pembayaran_verify`) oleh `bendahara` yang sudah menyelesaikan MFA TOTP (`mfa=verified`) -> menyetujui pembayaran & mengonversi `stok_tertahan` -> pengurangan `stok`

**Komposisi Beban Aktual (Run [`44996362750`](https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/44996362750)):**
- Total permintaan HTTP (`http_reqs`): **`17.827`**
- Permintaan aplikasi (`read + write`): **`14.754` baca + `3.012` tulis = `17.766`** (`61` permintaan sisanya adalah `setup()` untuk inisialisasi produk & 30 akun pembeli).
- Persentase tulis (`write_reqs / (read_reqs + write_reqs)`): **`17.0%`** (`3.012` transaksi tulis dibanding `14.754` pembacaan — sedikit di atas skenario tipikal `10%–15%`).

---

## 3. Hasil Pengukuran Verbatim CI Run `44996362750` (Commit `b6bdcaa`)

Sumber bukti langsung: job [`k6 200-VU Per-Endpoint, Schemathesis, & Toxiproxy Chaos (PG16)`](https://github.com/Entahlah831/kopdes-merah-putih/actions/runs/44996362750/job/137128137133), artifak `ci-perf-fuzz-chaos-reports/k6_per_endpoint.md`:

```text
total_reqs=17827 read_reqs=14754 write_reqs=3012 write_pct=17.0% overall=(p50=1.2ms p95=5.3ms p99=14.4ms) read_slo=(p50=1.1ms p95=2.3ms p99=4.8ms) write_slo=(p50=4.0ms p95=10.7ms p99=39.4ms) http_err=0.17% unexp_err=0.00%
```

| Jenis | Endpoint | p50 (ms) | p95 (ms) | p99 (ms) | Ambang SLO (`p95`) | Status |
| :--- | :--- | ---: | ---: | ---: | :---: | :---: |
| BACA | `GET /api/v1/public/config` | 0.9 | 1.5 | 4.2 | `< 300 ms` | **PASS** |
| BACA | `GET /api/v1/katalog/produk` | 1.2 | 2.4 | 5.8 | `< 300 ms` | **PASS** |
| BACA | `GET /api/v1/iuran/tagihan` | 1.3 | 2.7 | 5.9 | `< 300 ms` | **PASS** |
| BACA | `GET /api/v1/pesanan` | 1.0 | 2.0 | 3.4 | `< 300 ms` | **PASS** |
| BACA | `GET /api/v1/pembayaran` | 1.0 | 2.0 | 4.2 | `< 300 ms` | **PASS** |
| BACA | `GET /api/v1/laporan` | 1.2 | 2.5 | 4.3 | `< 300 ms` | **PASS** |
| TULIS | `POST /api/v1/toko/checkout` | 4.1 | 12.6 | 157.4 | `< 800 ms` | **PASS** |
| TULIS | `POST /api/v1/pembayaran (JSON)` | 4.6 | 9.7 | 23.6 | `< 800 ms` | **PASS** |
| TULIS | `POST /api/v1/pembayaran (Multipart)` | 2.7 | 5.7 | 14.7 | `< 800 ms` | **PASS** |
| TULIS | `POST /api/v1/pembayaran/{id}/verifikasi` | 4.6 | 12.5 | 30.4 | `< 800 ms` | **PASS** |

### Catatan Kejujuran Angka `http_err=0.17%` vs `unexp_err=0.00%`
- `unexp_err=0.00%` (`kopdes_unexpected_error`) menunjukkan **0 kegagalan** dari seluruh `14.754` pembacaan dan `3.012` permintaan tulis checkout/pembayaran/upload bukti/verifikasi bendahara.
- `http_err=0.17%` (`http_req_failed`) berasal dari `30` respons **`409 USER_EXISTS`** yang memang **diharapkan** (`if (uRes.status !== 201 && uRes.status !== 409) throw ...`) pada fungsi idempoten `ensureBuyerSessions` di tahap `setup()` karena fase `load` dijalankan tepat sesudah fase `smoke` selesai membuat ke-30 akun pembeli uji `k6buyer01..30` pada instance database yang sama (`30 / 17.827 = 0.168%`).

---

## 4. Uji Fuzzing & Ketahanan Chaos Terkait pada Job yang Sama

- **Schemathesis OpenAPI Fuzzing:** `PASS` (`0` status `500` server error pada seluruh rute `docs/openapi.yaml` dengan autentikasi sesi + header `X-Requested-With`).
- **Toxiproxy Chaos DB:** latensi buatan `+120ms ± 20ms` `/readyz -> HTTP 200`, putus TCP PostgreSQL `/readyz -> HTTP 503`, pemulihan koneksi `/readyz -> HTTP 200`.
