# ADR-007: Kendali Ganda Ketat + Break-Glass Admin Terkontrol & TOTP Segar (D7)

- **Status**: `DIUBAH` (kendali ganda tetap wajib default; tambah jalur darurat *break-glass* khusus `admin` di balik flag `.env`)
- **Tag Kebijakan**: **KEPUTUSAN PEMILIK** (`DUAL_CONTROL_BREAKGLASS_ENABLED` default `false` — pemilik menentukan apakah darurat staf 1 orang diizinkan)

## (a) Keputusan Saat Ini dan Lokasi Pembuatan
Pada branch `lean-revamp`, `internal/pembayaran/verifikasi.go` selalu memblokir `dibuat_oleh == claims.UserID` (`403 DUAL_CONTROL_VIOLATION`) tanpa pengecualian apa pun. Di koperasi desa dengan jumlah staf kecil (sering hanya 1 `admin` + 1 `bendahara`), bila salah satu cuti/sakit atau ada setoran tunai saat staf kedua absen, seluruh persetujuan pembayaran bisa terhenti (*deadlock* operasional).

## (b) Opsi Alternatif (Minimal 2)
1. **Opsi 1 — Ketat Tanpa Pengecualian**: Aman terhadap kolusi/self-approval, tetapi buntu total bila bendahara berhalangan dan admin mencatat tunai sendiri.
2. **Opsi 2 — Tetap Ketat Default (`false`), Tambah Jalur Break-Glass Khusus `admin` di Balik `DUAL_CONTROL_BREAKGLASS_ENABLED`**: Secara default aturan dual-control tetap mutlak (termasuk pembuat QRIS, pembuat pembayaran tunai, dan pemohon refund). Jika pemilik secara eksplisit mengaktifkan `DUAL_CONTROL_BREAKGLASS_ENABLED=true`, hanya peran **`admin`** yang boleh melewati `dibuat_oleh == claims.UserID` apabila memenuhi **keempat syarat**: (1) `breakglass: true`, (2) `alasan` minimal `10` karakter, (3) kode TOTP `mfa_code` **baru & anti-replay** diverifikasi terhadap DB saat itu (`UpdateUserMFALastStep`), dan (4) tercatat khusus dengan aksi `pembayaran.verify_breakglass` / `pembayaran.refund_breakglass` di `audit_log` imutabel. Peran `bendahara` **tidak pernah** boleh break-glass.
3. **Opsi 3 — Dual Control Hanya di Atas Ambang Nominal**: Melemahkan kontrol harian terhadap pemecahan transaksi (*structuring*) di bawah ambang nominal.

## (c) Kriteria dan Bobot Keputusan
| Kriteria | Bobot | Opsi 1 (Ketat Kaku) | Opsi 2 (Dual Control + Break-Glass Admin) | Opsi 3 (Ambang Nominal) |
| :--- | :---: | :---: | :---: | :---: |
| Keamanan uang & data pribadi | 30% | 5 | **5** | 3 |
| Kesederhanaan operasi (1 dev) | 20% | 5 | **4** | 4 |
| Kecocokan skala & UX warga desa | 20% | 2 | **5** | 4 |
| Keandalan & kemudahan pemulihan | 15% | 3 | **5** | 3 |
| Biaya & kemampuan dibalik | 15% | 5 | **5** | 4 |
| **Skor Tertimbang** | **100%** | **4.05** | **4.80** | **3.55** |

## (d) Bukti Terukur di Repositori
- `TestDecisionAudit_D7_DualControlNormalAndBreakglass` (`internal/api/decision_audit_refund_breakglass_test.go`, **PASS**, `-race`, `5.44s`):
  1. Saat `DUAL_CONTROL_BREAKGLASS_ENABLED=false` (default): `admin` mengirim `breakglass: true` + `mfa_code` sah tetap ditolak **`403`**.
  2. Saat `DUAL_CONTROL_BREAKGLASS_ENABLED=true`:
     - `bendahara` memakai `breakglass: true` + `mfa_code` sah **ditolak `403`** (tetap terlarang).
     - `admin` dengan `alasan` < 10 karakter **ditolak `400`**.
     - `admin` dengan TOTP salah **ditolak `401`**.
     - `admin` dengan alasan jelas (>=10 karakter) + TOTP segar sukses **`200 lunas`**, dan tepat 1 baris `audit_log` dengan `aksi='pembayaran.verify_breakglass'` tercatat.

## (e) Rekomendasi
**DIUBAH -> Pilih Opsi 2 (Kendali Ganda Wajib Default + Break-Glass Admin Terkontrol di Balik Flag `DUAL_CONTROL_BREAKGLASS_ENABLED`).**

## (f) Implementasi yang Dikerjakan
- Variabel env `DUAL_CONTROL_BREAKGLASS_ENABLED` (default `false` pada `.env.example`, `.env.test`, `.env.test.alt`).
- `internal/pembayaran/breakglass.go` (`authorizeDualControlOrBreakglassTx`, `verifyAdminBreakglassTOTPTx` anti-replay), terintegrasi di `verifikasi.go` dan `refund.go`.

## (g) Risiko Sisa
- Apabila di koperasi hanya ada 1 akun `admin` dan ia menyalahgunakan `DUAL_CONTROL_BREAKGLASS_ENABLED=true` dengan TOTP-nya sendiri, fraud masih dapat dilakukan namun jejaknya tercatat permanen pada `audit_log` imutabel; pemilik disarankan tetap mengangkat cadangan staf/pengawas dan membiarkan `DUAL_CONTROL_BREAKGLASS_ENABLED=false` kecuali darurat (**KEPUTUSAN PEMILIK**).
