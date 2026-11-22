# ADR-004: Pemisahan Subkunci Kriptografi HKDF-SHA256 & Isolasi Storage KYC vs Bukti (D4)

- **Status**: `DIUBAH` (pemisahan domain kriptografi HKDF-SHA256, amplop DEK seragam untuk KYC & bukti, serta direktori/volume & retensi terpisah)

## (a) Keputusan Saat Ini dan Lokasi Pembuatan
Pada branch `lean-revamp`, satu kunci mentah `AES_MASTER_KEY` (`internal/security/` & `internal/pembayaran/verifikasi.go`) dipakai langsung sekaligus untuk enkripsi kolom DB, membungkus DEK KYC, dan mengenkripsi langsung file bukti bayar. KYC memakai amplop `DEK+KEK` sedangkan bukti memakai kunci master langsung (tidak seragam), serta "pisah storage" baru berupa subdirektori `/kyc` dan `/bukti` dalam satu path `STORAGE_KYC_DIR`.

## (b) Opsi Alternatif (Minimal 2)
1. **Opsi 1 — Satu Kunci Mentah untuk Semua & Satu Path Storage**: Risiko blast radius bila satu komponen bocor dan pencadangan/retensi berkas bercampur.
2. **Opsi 2 — Satu Root `AES_MASTER_KEY` dengan Derivasi Subkunci HKDF-SHA256 per Tujuan + Amplop Seragam + `STORAGE_KYC_DIR` & `STORAGE_BUKTI_DIR` Terpisah**: Menurunkan 4 subkunci independen via HKDF-SHA256 berlabel (`kopdes:v1:db-columns`, `kopdes:v1:kyc-files`, `kopdes:v1:bukti-files`, `kopdes:v1:backup`) dengan dukungan penuh rotasi `AES_PREVIOUS_KEYS`. Seluruh berkas KYC, bukti bayar, dan bukti refund memakai skema amplop `EncryptEnvelopeFile`/`DecryptEnvelopeFile` (DEK 32-byte acak per berkas + `key_id` + `dek_wrapped`) dan disimpan pada path & volume Docker terpisah (`STORAGE_KYC_DIR`, `STORAGE_BUKTI_DIR`), dengan retensi terpisah (`PDP_REJECTED_RETENTION_DAYS` vs `BUKTI_RETENTION_DAYS`) dan perintah migrasi idempoten `kopdes storage migrate-files`.
3. **Opsi 3 — Empat Variabel Master Key Terpisah di `.env`**: Menambah kerumitan rotasi & manajemen 4 rahasia panjang bagi 1 developer/pengurus desa tanpa keuntungan kriptografis tambahan dibanding HKDF dari 1 CSPRNG root 256-bit.

## (c) Kriteria dan Bobot Keputusan
| Kriteria | Bobot | Opsi 1 (Lama) | Opsi 2 (HKDF + Storage Terpisah) | Opsi 3 (4 Root Keys) |
| :--- | :---: | :---: | :---: | :---: |
| Keamanan uang & data pribadi | 30% | 3 | **5** | 5 |
| Kesederhanaan operasi (1 dev) | 20% | 5 | **5** | 2 |
| Kecocokan skala & UX warga desa | 20% | 4 | **5** | 4 |
| Keandalan & kemudahan pemulihan | 15% | 3 | **5** | 3 |
| Biaya & kemampuan dibalik | 15% | 4 | **4** | 3 |
| **Skor Tertimbang** | **100%** | **3.70** | **4.85** | **3.60** |

## (d) Bukti Terukur di Repositori
- `TestPurposeKeyDerivationAndCrossPurposeIsolation` (`internal/security/kdf_test.go`, **PASS**, `-race`): subkunci `DB`, `KYC`, `Bukti`, `Backup` berbeda secara deterministik, mendukung rotasi `AES_PREVIOUS_KEYS`, dan ciphertext dari subkunci `KYC` **ditolak (`error`)** ketika coba didekripsi memakai subkunci `Bukti` atau `DB` dan sebaliknya.
- `TestProofEnvelopeAndSeparateStorageIsolation` (`internal/pembayaran/pembayaran_test.go` + `proof_storage_test.go`, **PASS**, `-race`): bukti bayar terenkripsi amplop di `STORAGE_BUKTI_DIR` terpisah dari `STORAGE_KYC_DIR` dan tanpa header PNG plaintext di disk.
- `TestRelocateLegacySubdirs_Idempotent` (`cmd/kopdes/main_test.go`, **PASS**, `-race`): pemindahan berkas subdirektori lama & re-enkripsi amplop aman dijalankan ulang (panggilan kedua `moved=0`).

## (e) Rekomendasi
**DIUBAH -> Pilih Opsi 2 (Derivasi HKDF-SHA256 4 Domain + Amplop DEK Seragam + Storage & Retensi Terpisah).**

## (f) Implementasi yang Dikerjakan
- `internal/security/kdf.go` (`PurposeDBColumns`, `PurposeKYCFiles`, `PurposeBuktiFiles`, `PurposeBackup`, `NewPurposeKeySet`).
- Variabel env baru `STORAGE_BUKTI_DIR` & `BUKTI_RETENTION_DAYS` (+ volume terpisah `bukti_storage` di `docker-compose*.yml`).
- Migrasi `000002`: kolom `pembayaran.bukti_key_id`, `pembayaran.bukti_dek_wrapped`.
- `cmd/kopdes/storage_ops.go`: `kopdes storage migrate-files` (idempoten) & `kopdes bukti purge`.

## (g) Risiko Sisa
- Seluruh subkunci tetap bergantung pada keamanan `AES_MASTER_KEY` di `.env` server; jika `.env` server diretas bersamaan dengan akses DB + disk, attacker tetap dapat membaca isi hingga kunci dirotasi dan berkas lawas di-rewrap (**KEPUTUSAN PEMILIK** untuk kebijakan rotasi kunci tahunan & pembatasan akses root VPS).
