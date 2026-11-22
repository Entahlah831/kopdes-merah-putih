# ADR-005: Backup Terautentikasi `age` dengan Arsip DB + KYC/Bukti & Kunci Off-Server (D5)

- **Status**: `DIUBAH` (beralih dari `openssl aes-256-cbc` + kunci simetris di `.env` server ke enkripsi kunci publik terautentikasi `age` + arsip DB/KYC/Bukti + CI uji restore)

## (a) Keputusan Saat Ini dan Lokasi Pembuatan
Pada branch `lean-revamp`, `scripts/backup.sh` hanya mencadangkan `database.dump` (tidak mencadangkan berkas `.enc` di disk KYC/bukti), mengenkripsi dengan `openssl enc -aes-256-cbc -pbkdf2` yang tidak memiliki tag autentikasi (AEAD) terhadap modifikasi ciphertext, serta menyimpan kunci simetris `BACKUP_ENCRYPTION_KEY` di berkas `.env` yang sama di server. Jika server dibobol, penyerang memperoleh backup sekaligus kunci dekripsinya.

## (b) Opsi Alternatif (Minimal 2)
1. **Opsi 1 — Tetap `openssl aes-256-cbc` dengan `BACKUP_ENCRYPTION_KEY` di Server**: Rentan tampering ciphertext tak terdeteksi & kunci bocor bersamaan saat server diretas, serta berkas KYC/bukti tidak ikut dibackup.
2. **Opsi 2 — Arsip Terpadu DB + KYC + Bukti + `manifest.sha256` Dienkripsi `age` Kunci Publik (`BACKUP_AGE_RECIPIENT`)**: Server hanya menyimpan **public key** `age1...` di `.env`. Enkripsi memakai `age` (AEAD ChaCha20-Poly1305 per 64KiB chunk + X25519); **private key** (`AGE-SECRET-KEY-...`) **dilarang keras** ada di server produksi dan disimpan off-server oleh pemilik. Skrip gagal tertutup (*fail-closed*) di produksi bila `BACKUP_AGE_RECIPIENT` kosong atau biner `age` tidak ada. CI menguji full round-trip + penolakan ciphertext termodifikasi 1 byte.
3. **Opsi 3 — WAL Continuous Archiving + Point-in-Time Recovery (PITR)**: Menambah kompleksitas operasi storage S3/WAL daemon bagi 1 VPS & 1 developer. Tidak diperlukan pada skala saat ini kecuali pemilik meminta eksplisit RPO mendekati nol.

## (c) Kriteria dan Bobot Keputusan
| Kriteria | Bobot | Opsi 1 (OpenSSL CBC) | Opsi 2 (`age` Publik + Arsip) | Opsi 3 (WAL/PITR) |
| :--- | :---: | :---: | :---: | :---: |
| Keamanan uang & data pribadi | 30% | 1 | **5** | 4 |
| Kesederhanaan operasi (1 dev) | 20% | 4 | **5** | 2 |
| Kecocokan skala & UX warga desa | 20% | 3 | **5** | 3 |
| Keandalan & kemudahan pemulihan | 15% | 2 | **5** | 5 |
| Biaya & kemampuan dibalik | 15% | 5 | **5** | 3 |
| **Skor Tertimbang** | **100%** | **2.65** | **5.00** | **3.40** |

## (d) Bukti Terukur di Repositori
- `scripts/backup.sh` menggabungkan `database.dump`, `kyc_storage.tar.gz`, `bukti_storage.tar.gz`, `metadata.txt`, dan `manifest.sha256`, mengenkripsi dengan `age -r "${BACKUP_AGE_RECIPIENT}"`, lalu memverifikasi `.sha256` terluar.
- `scripts/restore_drill.sh` mencakup uji penolakan manipulasi ciphertext (`dd` flip 1 byte di `.tar.age` wajib ditolak oleh `age -d`) serta memverifikasi `>= 21` tabel, marker `audit_log`, izin non-superuser `kopdes_app`, dan ekstraksi arsip KYC/Bukti.
- `.github/workflows/ci.yml` menginstal `postgresql-client` + `age` dan menjalankan `scripts/backup.sh`, `scripts/restore_test.sh`, dan `scripts/restore_drill.sh` penuh pada Linux CI; pada host Windows lokal yang tidak memasang `age`/`docker`, paritas `.env` (`BACKUP_AGE_RECIPIENT`) terverifikasi oleh `TestRepoAudit_EnvCatalogConsistency_AndEnvReferenceFresh` (**PASS**).

## (e) Rekomendasi
**DIUBAH -> Pilih Opsi 2 (Enkripsi Kunci Publik `age` Terautentikasi + Arsip DB/KYC/Bukti + Private Key Off-Server, RPO <= 24 Jam).**

## (f) Implementasi yang Dikerjakan
- Mengganti `BACKUP_ENCRYPTION_KEY` -> `BACKUP_AGE_RECIPIENT` pada `internal/config/config.go`, `.env.example`, `.env.test`, `.env.test.alt`, `docker-compose*.yml`, dan `docs/ENV_REFERENCE.md`.
- Memperbarui `scripts/backup.sh`, `scripts/restore_drill.sh`, `scripts/restore_test.sh` (hapus total `openssl aes-256-cbc`).
- Memperbarui `.github/workflows/ci.yml` agar menjalankan PostgreSQL 16 (21 tabel penuh tanpa `-short`) dan latihan backup/tamper/restore `age` otomatis.

## (g) Risiko Sisa
- **RISIKO KEHILANGAN DATA PERMANEN**: Bila pemilik menghilangkan *private key* `AGE-SECRET-KEY-...` yang disimpan di luar server, seluruh arsip backup `.tar.age` tidak dapat didekripsi selamanya (**KEPUTUSAN PEMILIK** untuk menyalin kunci ke minimal 2 lokasi aman terpisah).
- RPO tetap <= 24 jam sesuai jadwal backup harian dan ketahanan off-server tetap bergantung pada sinkronisasi salinan ke remote storage (mis. `rclone copy` / `restic`).
