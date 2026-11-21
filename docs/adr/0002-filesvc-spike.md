# ADR-0002: Eksperimen Microservice Terukur `filesvc` (`spike/filesvc`)

- **Status:** Ditolak Sementara *(Eksperimen Terukur Selesai; Cetak Biru & Bukti Disimpan)*
- **Tanggal:** 2026-11-22
- **Keputusan Terkait:** ADR-0001 (Modular Monolith), Prompt v9 Fase 3 (`spike/filesvc`)
- **Artefak Data:** [filesvc_metrics.json](../../reports/spike/filesvc_metrics.json), [fault_isolation.json](../../reports/spike/fault_isolation.json), [test_output.txt](../../reports/spike/test_output.txt)

---

## 1. Konteks & Tujuan Eksperimen

Sesuai ADR-0001, alih-alih memecah 8 domain menjadi banyak microservice sejak awal, tim memilih **satu kandidat paling berisiko rendah dan bernilai isolasi keamanan paling jelas**: mengekstrak sub-sistem penyimpanan & kriptografi berkas terenkripsi (`filestore.FileStore` — dokumen KYC anggota & bukti pembayaran QRIS) menjadi proses terpisah bernama `kopdes filesvc` melalui branch eksperimen `spike/filesvc`.

Eksperimen ini menguji hipotesis secara terukur:
1. Apakah pemisahan `filesvc` di belakang antarmuka `filestore.FileStore` (`RemoteFileStore` <-> HTTP S2S `Server`) menambah latensi p95 di bawah batas `< 50 ms` pada loopback?
2. Apakah saat `filesvc` mati, API utama tetap melayani endpoint non-berkas (`/healthz`, `/public/config`, `/katalog/produk`) dengan HTTP `200`, sementara endpoint berkas turun terkontrol ke HTTP `503`, lalu pulih otomatis ke `200` ketika `filesvc` menyala kembali?
3. Apakah proses API dapat beroperasi dengan **0 akses kunci dekripsi berkas**, dan apakah biaya operasionalnya sepadan untuk target **1 developer, 1 VPS, <= 1.000 pengunjung**?

---

## 2. Arsitektur Spike yang Diuji

Pada branch `spike/filesvc` (diukur terisolasi, tidak digabung permanen):
- Ditambahkan 3 variabel ke konfigurasi tunggal `.env`: `FILESVC_ENABLED`, `FILESVC_PORT`, `FILESVC_TOKEN` (`51 -> 54` variabel).
- Subperintah baru `kopdes filesvc` menjalankan server HTTP `internal/filestore/server.go` (`GET /healthz`, `PUT/GET/HEAD/DELETE /v1/objects/{ns}/{key}`) dengan autentikasi bearer token konstan-waktu (`FILESVC_TOKEN` minimal 16 karakter) dan penyimpanan disk terenkripsi `LocalFileStore`.
- Saat `FILESVC_ENABLED=true`, `internal/api/routes.go` men-`nil`-kan `kycKP`, `buktiKP`, `masterKP` sebelum inisialisasi modul domain dan menyuntikkan `filestore.RemoteFileStore` melalui kontrak yang sama (`filestore.FileStore`).

---

## 3. Hasil Pengukuran Nyata (`reports/spike/`)

### 3.1 Tolok Ukur Latensi Lokal vs HTTP S2S Loopback (`100` iterasi, payload `16.384` byte PNG)

Sumber: [filesvc_metrics.json](../../reports/spike/filesvc_metrics.json)

| Operasi | Lokal Mean (ms) | Lokal p95 (ms) | Remote `filesvc` Mean (ms) | Remote `filesvc` p95 (ms) | Delta p95 (ms) | Syarat (`< 50 ms`) |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| `SaveEncrypted` | 1.383 | 2.005 | 2.232 | 3.018 | **+1.013** | **LOLOS** |
| `LoadDecrypted` | 7.670 | 9.595 | 7.553 | 8.995 | **-0.599** | **LOLOS** |

### 3.2 Isolasi Gangguan & Pemulihan Otomatis (`TestFilesvcSpike_FaultIsolationAndZeroAPIFileKeys`)

Sumber: [fault_isolation.json](../../reports/spike/fault_isolation.json) & [test_output.txt](../../reports/spike/test_output.txt)

| Fase Pengujian | Aksi / Endpoint | Hasil Nyata Terukur | Keterangan |
| :--- | :--- | :---: | :--- |
| **Fase A (`filesvc` UP)** | `POST /api/v1/public/pendaftaran/{id}/dokumen` (KTP PNG) | **HTTP 201** | API menulis via `RemoteFileStore`; direktori `STORAGE_KYC_DIR` API tetap kosong (`0` berkas). |
| **Fase A (`filesvc` UP)** | `GET /api/v1/pendaftaran/{id}/dokumen/ktp` (Admin MFA) | **HTTP 200** | Byte dekripsi identik 1:1 dengan PNG unggahan awal. |
| **Fase A (Pemisahan Kunci)** | Dekripsi berkas `.enc` milik `filesvc` memakai `AES_MASTER_KEY` API | **Ditolak / Gagal** | `phase_a_api_key_rejects_enc = true`; API memegang 0 kunci dekripsi berkas. |
| **Fase B (`filesvc` DOWN)** | `GET /healthz`, `/api/v1/public/config`, `/api/v1/katalog/produk` | **HTTP 200 / 200 / 200** | Endpoint non-berkas tidak terdampak saat `filesvc` dimatikan paksa. |
| **Fase B (`filesvc` DOWN)** | `GET .../dokumen/ktp` & `POST .../dokumen` (`selfie_ktp`) | **HTTP 503 / 503** | Error domain `STORAGE_UNAVAILABLE` terkontrol tanpa panic atau crash proses API. |
| **Fase C (`filesvc` RESTART)** | `GET .../dokumen/ktp` di alamat `filesvc` semula (tanpa restart API) | **HTTP 200** | Pulih otomatis dan byte dekripsi tetap cocok (`phase_c_byte_exact_match = true`). |

### 3.3 Keamanan S2S & Analisis Statis Spike

- Permintaan `PUT /v1/objects/...` tanpa token / token salah -> **HTTP 401** (`FILESVC_UNAUTHORIZED`), `RemoteFileStore` bertoken salah -> `ErrStorageUnavailable` (`503`).
- Path traversal (`..%2Fevil.enc`) dan namespace ilegal (`badns`) -> **HTTP 400**.
- `envcheck --strict --ports --no-listen` Set A & Set B -> **PASS (`54` variabel)**, `--compare` -> **54 kunci identik**.
- `staticcheck ./...` -> **0 temuan**; `gosec ./...` (88 berkas, 20.384 baris) -> **0 temuan**.

---

## 4. Evaluasi Gerbang Keputusan 6 Kriteria

| No | Kriteria Adopsi | Hasil Evaluasi | Penilaian |
| :---: | :--- | :--- | :---: |
| 1 | Overhead latensi `p95` S2S `< 50 ms` | Delta Save `+1.013 ms`, Load `-0.599 ms` | Memenuhi |
| 2 | Degradasi anggun (`503`) + pemulihan otomatis + endpoint inti tetap `200` | Terbukti di Fase A/B/C | Memenuhi |
| 3 | Isolasi kunci & token S2S (`401`) | API tidak menyentuh kunci enkripsi berkas; traversal ditolak | Memenuhi |
| 4 | Kepatuhan anggaran "ramping & satu `.env`" | Menambah `51 -> 54` variabel env (`FILESVC_ENABLED`, `FILESVC_PORT`, `FILESVC_TOKEN`), +1 secret S2S yang wajib dirotasi, +1 pemeriksaan konflik port | **Beban Tambahan** |
| 5 | Kompleksitas operasi untuk **1 developer di 1 VPS** | Menambah 1 proses/daemon tambahan (`kopdes filesvc`), systemd/restart unit & healthcheck ekstra, serta kebutuhan memisahkan kunci host apabila isolasi kriptografi nyata ingin ditegakkan penuh | **Beban Tambahan** |
| 6 | Nilai guna pada skala `<= 1.000` pengguna vs pemicu ADR-0001 | Pada **1 VPS dengan 1 berkas `.env`**, `kopdes serve` dan `kopdes filesvc` masih berbagi host/OS dan pembacaan konfigurasi yang sama sehingga isolasi privilege penuh belum tercapai tanpa VM/secret store terpisah | **Belum Terjustifikasi** |

---

## 5. Keputusan Akhir

**Keputusan: DITOLAK SEMENTARA untuk deployment aktif (`modular-and-wireframe`), pertahankan Modular Monolith (`51` variabel `.env`, `kopdes serve` + `kopdes worker`).**

Alasan ringkas:
1. Secara teknis (`p95 < 50 ms`, `503` isolasi, `401` S2S), pola pemisahan via `filestore.FileStore` **terbukti siap pakai tanpa mengubah satu pun logika domain** (`anggota`, `pembayaran`, `auth`, `pesanan`, `iuran`, `katalog`, `laporan`, `pengaturan`).
2. Namun, menjalankan daemon `filesvc` ekstra di **1 VPS yang sama oleh 1 developer** untuk beban <= 1.000 pengunjung melanggar prinsip *"ramping"* dan *"semua dari satu `.env`"* karena menambah daemon, port, dan rahasia S2S yang belum memberi isolasi host/KMS fisik selama keduanya berada pada VPS dan berkas `.env` yang sama.
3. Oleh karena itu, branch kode eksperimen `spike/filesvc` tidak digabung ke branch utama (`modular-and-wireframe`), sementara hasil pengukuran dan ADR ini disimpan permanen sebagai bukti eksperimen terukur.

---

## 6. Pemicu Ekstraksi Kembali & Cetak Biru Re-Aktivasi

Aktifkan kembali pemisahan `filesvc` di masa depan **hanya jika** salah satu kondisi berikut terpenuhi:
1. Berkas terenkripsi (`STORAGE_KYC_DIR` & `STORAGE_BUKTI_DIR`) dipindahkan ke VPS/VM storage terpisah atau di belakang KMS/HSM terpisah yang tidak boleh diakses proses API publik.
2. Audit kepatuhan/keamanan (**PERLU VERIFIKASI HUKUM** untuk PDP KYC berskala lebih besar) mewajibkan isolasi proses OS/jaringan antara endpoint publik dan pemegang kunci dekripsi.
3. I/O enkripsi/dekripsi berkas mulai mengganggu latensi CPU worker/API secara terukur di metrik p95 produksi.

Jika pemicu terpenuhi, antarmuka `filestore.FileStore` (`SaveEncrypted`, `LoadDecrypted`, `Delete`, `Exists`, `HealthCheck`) sudah berdiri di belakang kontainer abstrak sehingga `RemoteFileStore` + `filestore.Server` dapat dihidupkan ulang tanpa mengubah handler atau service domain mana pun.
