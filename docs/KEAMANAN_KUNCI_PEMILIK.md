# Tata Kelola, Backup, & Rotasi Kunci Kriptografi (Model 85% / 15% Inti Pemilik)

Dokumen ini menjelaskan batas tanggung jawab kriptografi **Kopdes Merah Putih** agar sistem tetap aman tanpa mengorbankan hak kendali pemilik atas rahasia paling kritis.

---

## 1. Pembagian Tanggung Jawab: 85% Skrip Otomatis vs 15% Inti Pemilik

| Porsi | Ruang Lingkup | Pelaksana / Perkakas | Alasan Keamanan |
| :---: | :--- | :--- | :--- |
| **85%** | Parsing aman `.env` tanpa mengeksekusi shell, validasi format panjang kunci, pembuatan arsip terenkripsi `age` (`backup_keys.sh`), penyusunan rantai `AES_PREVIOUS_KEYS` (`rotate_keys.sh`), verifikasi manifest SHA-256 + uji tamper 1-byte (`restore_keys.sh --drill`), serta re-enkripsi amplop file (`kopdes storage migrate-files`). | Otomasi skrip bash + biner Go `kopdes` + CI Linux (`--drill`). | Mencegah kesalahan salin-tempel manusia, kebocoran izin file (`umask 077` / `chmod 600`), atau kegagalan dekripsi data lama saat rotasi. |
| **15%** *(Inti)* | **(1)** Membangkitkan entropi akar sendiri (`AES_MASTER_KEY`, `BLIND_INDEX_SECRET`, pasangan kunci `age` brankas kunci, dan pasangan kunci `age` backup harian) di perangkat tepercaya **di luar VPS/Git**.<br>**(2)** Memilih skema ID versi kunci (`v1:k1=...` -> `v2:k1=...`) dan mengisi langsung ke `.env` VPS.<br>**(3)** Memegang **private key `age` brankas (`VAULT_AGE_IDENTITY_FILE`)** & **private key backup harian** di minimal **2 brankas offline terpisah**.<br>**(4)** Mengesahkan momen pemensiunan kunci lama dari `AES_PREVIOUS_KEYS` serta keputusan darurat `KP-10`. | **Pemilik Sistem / Pengurus Inti secara langsung.** | Agen AI, developer luar, maupun penyusup yang mencuri snapshot disk VPS **tidak pernah** menguasai entropi penciptaan awal maupun kunci pembuka brankas offline. |

> **Prinsip Pemisahan Kunci Brankas vs Kunci Backup Data:**
> Jangan samakan kunci penerima backup harian (`BACKUP_AGE_RECIPIENT` untuk `.tar.age` database + KYC/Bukti di [scripts/backup.sh](../scripts/backup.sh)) dengan kunci brankas kunci kriptografi (`VAULT_AGE_RECIPIENT` di [scripts/backup_keys.sh](../scripts/backup_keys.sh)). Jika satu perangkat backup dicuri bersama arsip data harian, penyerang tetap tidak bisa membuka kunci brankas jika `VAULT_AGE_IDENTITY_FILE` disimpan terpisah!

---

## 2. Daftar 15% Bagian Inti yang Wajib Dibuat & Diisi Sendiri oleh Pemilik

Jalankan perintah berikut **di komputer pribadi/perangkat offline tepercaya milik Anda** (bukan di repo Git dan tidak di-log ke chat mana pun):

### A. Pasangan Kunci `age` (2 Pasang Terpisah)
```bash
# 1. Pasangan kunci untuk backup data harian (database + storage KYC/Bukti)
age-keygen -o ~/offline-vault/kopdes-daily-backup-private.key

# 2. Pasangan kunci khusus brankas kunci enkripsi (VAULT_AGE_RECIPIENT)
age-keygen -o ~/offline-vault/kopdes-keyvault-private.key
chmod 600 ~/offline-vault/*.key
```
- Catat kedua *public key* (`age1...`) masing-masing:
  - `BACKUP_AGE_RECIPIENT=age1...` -> dimasukkan pemilik ke `.env` VPS.
  - `VAULT_AGE_RECIPIENT=age1...` -> dipakai saat menjalankan `scripts/backup_keys.sh`.
- **Simpan kedua `*-private.key` di minimal 2 media offline terpisah** (mis. USB terenkripsi Veracrypt/LUKS + brankas fisik cetak QR/kertas di kantor koperasi). **Dilarang menaruh private key di dalam VPS maupun repositori Git!**

### B. Entropi Akar Enkripsi & Blind Index Aplikasi (`.env` VPS)
```bash
# 1. Buat ID versi + kunci AES-256-GCM utama (contoh ID v1:k1)
echo "v1:k1=$(openssl rand -hex 32)"

# 2. Buat rahasia root HKDF-SHA256 blind index NIK/email/HP
openssl rand -hex 32
```
Isikan hasilnya secara manual oleh pemilik ke baris `.env` VPS:
- `AES_MASTER_KEY=v1:k1=<64-hex-buatan-anda>`
- `BLIND_INDEX_SECRET=<64-hex-buatan-anda>`
- `DB_PASSWORD=<sandi-unik-kopdes_app>`
- `POSTGRES_PASSWORD=<sandi-unik-bootstrap>`

Verifikasi tanpa membocorkan nilai:
```bash
go -C backend run ./cmd/envcheck --strict --ports --no-listen --env-file ../.env
```

---

## 3. Cara Menjalankan Otomasi Brankas Kunci (`backup_keys.sh` & `restore_keys.sh`)

### A. Mencadangkan Brankas Kunci
Selalu arahkan `--out-dir` ke partisi terpisah di luar repositori Git (mis. `/var/kopdes-keyvault` atau mount disk eksternal pemilik):
```bash
ENV_FILE=/opt/kopdes/.env \
KEY_VAULT_DIR=/var/kopdes-keyvault \
VAULT_AGE_RECIPIENT=age1_publik_brankas_anda \
bash scripts/backup_keys.sh --include-db
```
Skrip otomatis:
1. Menolak menulis ke dalam pohon repositori Git.
2. Mengekstrak kunci enkripsi (`AES_MASTER_KEY`, `AES_PREVIOUS_KEYS`, `BLIND_INDEX_SECRET`, `BACKUP_AGE_RECIPIENT`, plus sandi DB bila `--include-db`), menyertakan `metadata.txt` (fingerprint 12-hex) & `manifest.sha256`.
3. Mengenkripsi arsip `.tar.age` (`chmod 600`) tanpa mencetak satu pun nilai plaintext rahasia.

### B. Latihan Pemulihan & Uji Integritas (`--drill`)
Bawalah sementara salinan `VAULT_AGE_IDENTITY_FILE` hanya ke sesi staging/pemulihan terisolasi (hapus berkas identitas setelah selesai):
```bash
VAULT_AGE_IDENTITY_FILE=/mnt/usb/kopdes-keyvault-private.key \
bash scripts/restore_keys.sh \
  --vault /var/kopdes-keyvault/kopdes_keyvault_YYYYMMDD_HHMMSS.tar.age \
  --drill
```
Skrip menguji: **integritas SHA-256 arsip -> penolakan tamper 1-byte ciphertext -> dekripsi `age` -> verifikasi `manifest.sha256` -> kelengkapan kunci (disamarkan `***REDACTED***`)**.

Untuk menulis ulang kunci ke `.env` baru saat bencana server:
```bash
VAULT_AGE_IDENTITY_FILE=/mnt/usb/kopdes-keyvault-private.key \
bash scripts/restore_keys.sh \
  --vault /var/kopdes-keyvault/kopdes_keyvault_YYYYMMDD_HHMMSS.tar.age \
  --apply-env /opt/kopdes/.env.restored
```

---

## 4. Prosedur Rotasi Kunci Tanpa Downtime (`rotate_keys.sh`)

Disarankan merotasi `AES_MASTER_KEY` setiap **6–12 bulan** atau **segera** bila diduga ada kebocoran host/staff keluar:

1. **Pemilik membuat kunci versi baru (15% inti)** di mesin pribadi:
   ```bash
   umask 077
   printf 'v2:k1=%s\n' "$(openssl rand -hex 32)" > /tmp/owner_new_aes.txt
   ```
2. **Pratinjau & terapkan rotasi otomatis (85%)** agar kunci lama (`v1:k1=...`) otomatis dipindahkan ke `AES_PREVIOUS_KEYS`:
   ```bash
   bash scripts/rotate_keys.sh --env-file /opt/kopdes/.env --new-key-file /tmp/owner_new_aes.txt --dry-run
   bash scripts/rotate_keys.sh --env-file /opt/kopdes/.env --new-key-file /tmp/owner_new_aes.txt
   shred -u /tmp/owner_new_aes.txt || rm -f /tmp/owner_new_aes.txt
   ```
   *(Atau jalankan tanpa `--new-key-file` untuk mengetik/tempel secara interaktif tanpa tampil di layar/riwayat bash.)*
3. **Cadangkan brankas kunci terbaru**, restart layanan, dan migrasikan amplop DEK berkas KYC/bukti:
   ```bash
   ENV_FILE=/opt/kopdes/.env KEY_VAULT_DIR=/var/kopdes-keyvault VAULT_AGE_RECIPIENT=age1... bash scripts/backup_keys.sh --include-db
   docker compose --env-file /opt/kopdes/.env up -d api worker
   docker compose --env-file /opt/kopdes/.env exec -T api /app/kopdes --env-file /app/.env storage migrate-files
   ```
4. **Catatan penting mengenai `BLIND_INDEX_SECRET`**: Nilai `BLIND_INDEX_SECRET` digunakan untuk pencocokan deterministik NIK/email/HP di PostgreSQL (`nik_hash`, `email_hash`, `hp_hash`). Jangan mengubah `BLIND_INDEX_SECRET` secara mendadak tanpa jendela migrasi re-hashing kolom DB secara khusus (perlu skrip re-index terencana bila terjadi insiden kebocoran blind-index).
5. **Pensiun kunci lama (`KP-10`)**: Setelah seluruh berkas `.enc` selesai dimigrasi dan kolom DB terverifikasi (atau setelah periode retensi tercapai), baru hapus ID kunci lama dari `AES_PREVIOUS_KEYS`.
