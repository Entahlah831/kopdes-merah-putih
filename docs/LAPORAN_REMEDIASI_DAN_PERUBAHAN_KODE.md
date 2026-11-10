# Laporan Lengkap Remediasi Keamanan, Perubahan Kode & Arsitektur
**Sistem Backend Koperasi Desa Merah Putih (PostgreSQL 16 & Go)**

---

## 1. Ringkasan Eksekutif & Metodologi

Audit independen terhadap basis kode Koperasi Desa Merah Putih mengidentifikasi kerentanan dan ketidaklengkapan arsitektur dari tingkat **KRITIS**, **TINGGI**, **SEDANG**, hingga **KELENGKAPAN FUNGSIONAL (P1-1 s/d P1-5)**. Seluruh temuan tersebut telah diremediasi secara tuntas menggunakan metodologi **Test-Driven Development (TDD)** yang ketat:
1. Membuktikan setiap cacat terlebih dahulu dengan pengujian gagal (*failing reproduction test*).
2. Memperbaiki logika kode/skema hingga pengujian berhasil (*passing test*).
3. Mengabadikan perubahan skema database melalui pasangan migrasi bernomor urut tanpa pernah mengubah riwayat migrasi fondasi awal (`000001_init_kopdes_schema.up.sql`).
4. Memverifikasi seluruh paket backend hingga **100% lulus uji** (`go test -v ./...`).

Sistem mengimplementasikan arsitektur keamanan multi-tenancy tingkat database (PostgreSQL 16 RLS), enkripsi data pribadi (AES-256-GCM + AAD), serta integritas transaksi finansial (ACID, Maker-Checker, append-only hash chaining). Aspek kepatuhan regulasi eksternal memerlukan audit dan verifikasi hukum independen.

---

## 2. Matriks Status Remediasi Menyeluruh

| Kode | Kategori | Ringkasan Temuan | Status | File / Modul yang Diubah | Bukti & Solusi Teknis |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **K1** | KRITIS | Multi-Tenant Bypass via Pre-Auth Platform Admin | **SELESAI** | [`internal/api/middleware/tenant.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/middleware/tenant.go), [`internal/db/role_guard.go`](file:///c:/Users/12/kopdes-merah-putih/internal/db/role_guard.go), [`docker-compose.yml`](file:///c:/Users/12/kopdes-merah-putih/docker-compose.yml) | Hapus platform admin bypass pra-login, wajib role non-superuser `kopdes_app`, bind PostgreSQL docker hanya ke `127.0.0.1`. |
| **K2** | KRITIS | Partisi Tabel Hanya Sampai Tahun 2026 | **SELESAI** | [`db/migrations/000002_add_default_and_future_partitions.up.sql`](file:///c:/Users/12/kopdes-merah-putih/db/migrations/000002_add_default_and_future_partitions.up.sql), [`internal/db/partition_manager.go`](file:///c:/Users/12/kopdes-merah-putih/internal/db/partition_manager.go) | Menambahkan 12 partisi bulanan tahun 2027 dan partisi darurat `DEFAULT` pada 4 tabel terpartisi, ditambah autogenerator partisi masa depan. |
| **K3** | KRITIS | Race Condition & Arbitrary Payment Claim | **SELESAI** | [`db/migrations/000003_fix_pembayaran_verification.up.sql`](file:///c:/Users/12/kopdes-merah-putih/db/migrations/000003_fix_pembayaran_verification.up.sql), [`internal/api/handlers/qris_handler.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/handlers/qris_handler.go) | Prosedur tersimpan atomik `fn_setujui_pembayaran_qris` dengan kunci baris `FOR UPDATE`, pencatatan kas masuk ke `kas_mutasi`, outbox event, dan maker-checker. |
| **T1** | TINGGI | Pembayaran Tanpa Batasan Nominal / Arbitrer | **SELESAI** | [`internal/api/handlers/qris_handler.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/handlers/qris_handler.go), [`service.go`](file:///c:/Users/12/kopdes-merah-putih/service.go) | `POST /api/v1/pembayaran/qris` wajib terikat pada `tagihan_iuran_id` atau `pesanan_id`, nominal dihitung murni di server, pencatatan ke `pembayaran_alokasi`. |
| **T2** | TINGGI | Rate Limiter Rentan Spoofing IP & Memory Leak | **SELESAI** | [`internal/api/middleware/ratelimit.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/middleware/ratelimit.go) | Evaluasi `X-Forwarded-For` hanya dari `TRUSTED_PROXIES`, bounded LRU memory cache, lockout proteksi brute force login 15 menit. |
| **T3** | TINGGI | Secret Kriptografi Lemah & Kebocoran Log DSN | **SELESAI** | [`internal/security/crypto.go`](file:///c:/Users/12/kopdes-merah-putih/internal/security/crypto.go) | Enkripsi AES-256-GCM ber-AAD (`MasterKeyProvider`), blind index terisolasi HKDF-SHA256 per tenant, fungsi `RedactDSN` untuk menyensor kata sandi. |
| **T4** | TINGGI | Celah RLS pada Tabel Master & Users | **SELESAI** | [`db/migrations/000005_rls_master_and_users.up.sql`](file:///c:/Users/12/kopdes-merah-putih/db/migrations/000005_rls_master_and_users.up.sql) | Aktifkan `FORCE ROW LEVEL SECURITY` pada `users`, `audit_log`, `job_log`, `konfigurasi_aturan`, dan lookup login via `SECURITY DEFINER` terkunci. |
| **T5** | TINGGI | Sesi JWT Monolitik & Hierarki Peran Rapuh | **SELESAI** | [`internal/auth/jwt.go`](file:///c:/Users/12/kopdes-merah-putih/internal/auth/jwt.go), [`internal/api/handlers/auth_handler.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/handlers/auth_handler.go), [`db/migrations/000005_rls_master_and_users.up.sql`](file:///c:/Users/12/kopdes-merah-putih/db/migrations/000005_rls_master_and_users.up.sql) | Access token 15 menit dengan klaim `jti`, rotating refresh token di tabel `refresh_tokens`, deteksi token reuse, urutan hierarki peran deterministik, constant-time dummy bcrypt. |
| **S1** | SEDANG | IDOR Pembayaran dalam Satu Koperasi | **SELESAI** | [`internal/api/handlers/qris_handler.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/handlers/qris_handler.go) | Endpoint `GET /api/v1/pembayaran/{id}` membatasi anggota biasa hanya dapat melihat pembayaran miliknya sendiri. |
| **S2** | SEDANG | Parser EMVCo QRIS Rapuh & Rekening Pengurus | **SELESAI** | [`internal/qris/emvco.go`](file:///c:/Users/12/kopdes-merah-putih/internal/qris/emvco.go), [`db/migrations/000006_rekening_qris_jenis_pemilik.up.sql`](file:///c:/Users/12/kopdes-merah-putih/db/migrations/000006_rekening_qris_jenis_pemilik.up.sql) | Parser EMVCo defensif, validasi CRC16 dan Tag 00="01", stripping Tag 55-57 saat Tag 54 hadir, fuzz testing, feature flag `QRIS_DYNAMIC_ENABLED=false`, kolom `jenis_pemilik`. |
| **S3** | SEDANG | Konfigurasi Deployment & Pengerasan Server HTTP | **SELESAI** | [`internal/api/middleware/cors.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/middleware/cors.go), [`cmd/server/main.go`](file:///c:/Users/12/kopdes-merah-putih/cmd/server/main.go), [`Dockerfile`](file:///c:/Users/12/kopdes-merah-putih/Dockerfile) | Hapus `X-Koperasi-ID` dari CORS headers, larang credentials pada origin wildcard, batasan body 10MB, Slowloris header timeout 5s, SQL connection timeouts (15s/5s/30s). |
| **P1-1**| FUNGSI | Pendaftaran Anggota, Dokumen KYC, Kas Mutasi | **SELESAI** | [`internal/api/handlers/pendaftaran_handler.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/handlers/pendaftaran_handler.go), [`internal/api/handlers/kas_handler.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/handlers/kas_handler.go) | Handler pendaftaran terenkripsi AES-256-GCM + AAD, unggah KYC validasi magic-bytes (JPEG/PNG/PDF), maker-checker kelulusan anggota, `GET /api/v1/kas/mutasi`. |
| **P1-2**| FUNGSI | Kapasitas Slot Kode Unik Dinamis | **SELESAI** | [`db/migrations/000008_kode_unik_configurable_digits.up.sql`](file:///c:/Users/12/kopdes-merah-putih/db/migrations/000008_kode_unik_configurable_digits.up.sql), [`service.go`](file:///c:/Users/12/kopdes-merah-putih/service.go) | Dukungan 3 digit (999 slot) atau 4 digit (9.999 slot), `GetSlotUsage`, peringatan otomatis di atas 70%, respons HTTP 409 Conflict saat penuh. |
| **P1-3**| FUNGSI | Append-Only Hash Chain & Anti-TRUNCATE | **SELESAI** | [`db/migrations/000007_append_only_hash_chain.up.sql`](file:///c:/Users/12/kopdes-merah-putih/db/migrations/000007_append_only_hash_chain.up.sql), [`internal/security/crypto.go`](file:///c:/Users/12/kopdes-merah-putih/internal/security/crypto.go), [`service.go`](file:///c:/Users/12/kopdes-merah-putih/service.go) | Kolom `prev_hash`, `row_hash`, trigger `fn_prevent_truncate()` pada `kas_mutasi`, `audit_log`, `pembayaran_verifikasi_log`, fungsi SHA-256 `ComputeHashChain` & `VerifyHashChain`. |
| **P1-4**| FUNGSI | Operasional, Idempotency, Prometheus & Outbox | **SELESAI** | [`internal/api/handlers/metrics_handler.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/handlers/metrics_handler.go), [`internal/outbox/worker.go`](file:///c:/Users/12/kopdes-merah-putih/internal/outbox/worker.go), [`scripts/backup.sh`](file:///c:/Users/12/kopdes-merah-putih/scripts/backup.sh), [`scripts/restore_test.sh`](file:///c:/Users/12/kopdes-merah-putih/scripts/restore_test.sh) | Header `Idempotency-Key` pada pembayaran, endpoint Prometheus `GET /metrics`, outbox worker dengan `FOR UPDATE SKIP LOCKED`, skrip backup snapshot & uji restore PITR. |
| **P1-5**| FUNGSI | De-scope SIMKOPDES & Retensi PDP 30 Hari | **SELESAI** | [`db/migrations/000009_local_scope_and_pdp_retention.up.sql`](file:///c:/Users/12/kopdes-merah-putih/db/migrations/000009_local_scope_and_pdp_retention.up.sql), [`docs/SCOPE_RECONCILIATION.md`](file:///c:/Users/12/kopdes-merah-putih/docs/SCOPE_RECONCILIATION.md) | Drop tabel `sinkronisasi_simkopdes` (fokus lokal 1 cabang), prosedur pembersihan otomatis berkas KYC pendaftaran ditolak setelah 30 hari via `fn_purge_rejected_pendaftaran_pdp`. |

---

## 3. Rincian Migrasi Database Baru (PostgreSQL 16)

Semua perubahan skema database dikelola melalui pasangan file migrasi up/down bernomor urut tanpa mengubah `000001_init_kopdes_schema.up.sql`:

### A. Migrasi `000007_append_only_hash_chain.up.sql`
Menambahkan kolom hash chaining kriptografis dan metadata request ke tabel append-only finansial dan audit, serta memasang trigger penolakan `TRUNCATE`:
```sql
ALTER TABLE kas_mutasi
ADD COLUMN IF NOT EXISTS prev_hash char(64),
ADD COLUMN IF NOT EXISTS row_hash char(64),
ADD COLUMN IF NOT EXISTS ip_address varchar(45),
ADD COLUMN IF NOT EXISTS user_agent text,
ADD COLUMN IF NOT EXISTS request_id varchar(100);

ALTER TABLE pembayaran_verifikasi_log
ADD COLUMN IF NOT EXISTS prev_hash char(64),
ADD COLUMN IF NOT EXISTS row_hash char(64),
ADD COLUMN IF NOT EXISTS ip_address varchar(45),
ADD COLUMN IF NOT EXISTS user_agent text,
ADD COLUMN IF NOT EXISTS request_id varchar(100);

ALTER TABLE audit_log
ADD COLUMN IF NOT EXISTS prev_hash char(64),
ADD COLUMN IF NOT EXISTS row_hash char(64),
ADD COLUMN IF NOT EXISTS ip_address varchar(45),
ADD COLUMN IF NOT EXISTS user_agent text,
ADD COLUMN IF NOT EXISTS request_id varchar(100);

CREATE OR REPLACE FUNCTION fn_prevent_truncate()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'TABEL APPEND-ONLY: Operasi TRUNCATE dilarang pada tabel %', TG_TABLE_NAME;
END;
$$;

CREATE TRIGGER trg_prevent_truncate_audit_log BEFORE TRUNCATE ON audit_log FOR EACH STATEMENT EXECUTE FUNCTION fn_prevent_truncate();
CREATE TRIGGER trg_prevent_truncate_kas_mutasi BEFORE TRUNCATE ON kas_mutasi FOR EACH STATEMENT EXECUTE FUNCTION fn_prevent_truncate();
CREATE TRIGGER trg_prevent_truncate_pembayaran_verif BEFORE TRUNCATE ON pembayaran_verifikasi_log FOR EACH STATEMENT EXECUTE FUNCTION fn_prevent_truncate();
```

### B. Migrasi `000008_kode_unik_configurable_digits.up.sql`
Memperluas batasan konstrain `kode_unik` agar mendukung 3 digit (1..999) maupun 4 digit (1..9999):
```sql
ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS pembayaran_kode_unik_check;
ALTER TABLE pembayaran ADD CONSTRAINT chk_pembayaran_kode_unik CHECK (kode_unik BETWEEN 1 AND 9999);

ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS pembayaran_kode_unik_awal_check;
ALTER TABLE pembayaran ADD CONSTRAINT chk_pembayaran_kode_unik_awal CHECK (kode_unik_awal BETWEEN 1 AND 9999);
```

### C. Migrasi `000009_local_scope_and_pdp_retention.up.sql`
Menghapus tabel SIMKOPDES untuk fokus 100% pada deployment lokal satu kantor cabang Kopdes, dan menambahkan fungsi pembersihan berkas pendaftaran ditolak setelah masa retensi 30 hari:
```sql
DROP TABLE IF EXISTS sinkronisasi_simkopdes CASCADE;

CREATE OR REPLACE FUNCTION fn_purge_rejected_pendaftaran_pdp(
    p_koperasi_id uuid,
    p_retention_days int DEFAULT 30
)
RETURNS int
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_purged_count int := 0;
BEGIN
    DELETE FROM pendaftaran_dokumen pd
    USING pendaftaran_anggota pa
    WHERE pd.pendaftaran_id = pa.id
      AND pa.koperasi_id = p_koperasi_id
      AND pa.status = 'ditolak'
      AND pa.updated_at < (NOW() - (p_retention_days || ' days')::interval);

    WITH deleted_rows AS (
        DELETE FROM pendaftaran_anggota
        WHERE koperasi_id = p_koperasi_id
          AND status = 'ditolak'
          AND updated_at < (NOW() - (p_retention_days || ' days')::interval)
        RETURNING id
    )
    SELECT count(*) INTO v_purged_count FROM deleted_rows;

    RETURN v_purged_count;
END;
$$;
```

---

## 4. Rincian Perubahan Kode Backend (Go)

### 1. Kriptografi & Proteksi Integritas ([`internal/security/crypto.go`](file:///c:/Users/12/kopdes-merah-putih/internal/security/crypto.go))
Menambahkan generator SHA-256 Hash Chaining untuk menjamin rantai data append-only tidak dapat dimanipulasi:
```go
// GenesisHash merepresentasikan hash awal (64 karakter nol) untuk record pertama pada rantai append-only
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// ComputeHashChain menghitung SHA-256 hash berantai: SHA-256(prev_hash || payload)
func ComputeHashChain(prevHash string, payload []byte) string {
	if len(prevHash) != 64 {
		prevHash = GenesisHash
	}
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}
```

### 2. Kapasitas Kode Unik & Verifikasi Hash Chain ([`service.go`](file:///c:/Users/12/kopdes-merah-putih/service.go))
Mendukung 3 atau 4 digit kode unik, pelacakan rasio pemakaian slot, peringatan di atas 70%, dan verifikasi integritas tabel:
```go
func HitungKodeUnikAwalWithSlots(secret []byte, anggotaID, koperasiID uuid.UUID, maxSlots int) int {
	if maxSlots <= 0 {
		maxSlots = 999
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(anggotaID[:])
	mac.Write(koperasiID[:])
	sum := mac.Sum(nil)
	val := binary.BigEndian.Uint32(sum[0:4])
	return int(val%uint32(maxSlots)) + 1
}

func GetSlotUsage(ctx context.Context, pool *pgxpool.Pool, koperasiID, rekeningID uuid.UUID, nominalDasar int64, maxSlots int) (int, int, float64, error) {
	if maxSlots <= 0 {
		maxSlots = 999
	}
	var active int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pembayaran
		WHERE koperasi_id = $1
		  AND rekening_qris_id = $2
		  AND nominal_dasar = $3
		  AND status IN ('menunggu_pembayaran', 'menunggu_verifikasi')
		  AND berlaku_sampai > NOW()
		  AND deleted_at IS NULL;
	`, koperasiID, rekeningID, nominalDasar).Scan(&active)
	if err != nil {
		return 0, maxSlots, 0, err
	}
	ratio := float64(active) / float64(maxSlots)
	return active, maxSlots, ratio, nil
}
```

### 3. Idempotency Key & Alokasi QRIS Statis Pengurus ([`internal/api/handlers/qris_handler.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/handlers/qris_handler.go))
Menyediakan perlindungan idempotensi terhadap klik ganda dan mengembalikan respons identik dari memori:
```go
type IdempotentEntry struct {
	Response  BuatQRISResponse
	CreatedAt time.Time
}

// Di dalam HandleBuatQRIS:
idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
if idempotencyKey != "" {
	cacheKey := fmt.Sprintf("%s:%s", claims.UserID, idempotencyKey)
	h.idempotencyMu.RLock()
	cached, exists := h.idempotencyCache[cacheKey]
	h.idempotencyMu.RUnlock()
	if exists && time.Since(cached.CreatedAt) < 15*time.Minute {
		response.JSON(w, http.StatusCreated, cached.Response)
		return
	}
}
```

### 4. Pendaftaran Calon Anggota, KYC Magic Bytes & Purge 30 Hari ([`internal/api/handlers/pendaftaran_handler.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/handlers/pendaftaran_handler.go))
- **Enkripsi NIK & Data Sensitif**: NIK, tanggal lahir, dan alamat KTP dienkripsi menggunakan AES-256-GCM ber-AAD (`pendaftaran_anggota:nik_enkripsi:record_id`).
- **Validasi Magic Bytes Foto KTP**:
```go
func detectMIMEType(data []byte) string {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "image/png"
	}
	if len(data) >= 4 && bytes.Equal(data[:4], []byte("%PDF")) {
		return "application/pdf"
	}
	return http.DetectContentType(data)
}
```
- **Pembersihan Otomatis Retensi 30 Hari**:
```go
func (h *PendaftaranHandler) HandlePurgeRejectedPendaftaran(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}
	var purgedCount int
	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT fn_purge_rejected_pendaftaran_pdp($1, 30);", claims.KoperasiID).Scan(&purgedCount)
	})
...
```

### 5. Metrik Pemantauan Prometheus ([`internal/api/handlers/metrics_handler.go`](file:///c:/Users/12/kopdes-merah-putih/internal/api/handlers/metrics_handler.go))
Endpoint `GET /metrics` yang mengekspos metrik kesehatan sistem secara real-time:
```go
# HELP kopdes_uptime_seconds Total waktu server aktif dalam detik
# TYPE kopdes_uptime_seconds gauge
kopdes_uptime_seconds 124.50

# HELP kopdes_pembayaran_antrean_verifikasi Jumlah transaksi yang menunggu verifikasi admin
# TYPE kopdes_pembayaran_antrean_verifikasi gauge
kopdes_pembayaran_antrean_verifikasi 3

# HELP kopdes_pembayaran_slot_kode_unik_aktif Jumlah slot kode unik QRIS yang sedang aktif digunakan
# TYPE kopdes_pembayaran_slot_kode_unik_aktif gauge
kopdes_pembayaran_slot_kode_unik_aktif 12

# HELP kopdes_job_gagal_total Jumlah kegagalan eksekusi background job
# TYPE kopdes_job_gagal_total counter
kopdes_job_gagal_total 0

# HELP kopdes_partisi_tersisa Jumlah partisi kas_mutasi aktif terkonfigurasi di database
# TYPE kopdes_partisi_tersisa gauge
kopdes_partisi_tersisa 8
```

### 6. Transactional Outbox Pattern ([`internal/outbox/worker.go`](file:///c:/Users/12/kopdes-merah-putih/internal/outbox/worker.go))
Pekerja latar belakang yang memproses antrean notifikasi secara asinkron dengan kontrol persaingan `FOR UPDATE SKIP LOCKED`.

---

## 5. Ringkasan 5 Keputusan Bisnis yang Diadopsi

1. **Rekening QRIS Pengurus Diizinkan (Mode Pilot Lokal)**:
   - Nilai konfigurasi: `ALLOW_PERSONAL_PAYMENT_PILOT=true`.
   - Admin koperasi bebas mengunggah string/payload QRIS statis pengurus melalui `POST /api/v1/koperasi/rekening-qris`.
2. **QRIS Dinamis Dinonaktifkan (Gunakan QRIS Statis + Kode Unik)**:
   - Nilai konfigurasi: `QRIS_DYNAMIC_ENABLED=false`.
   - Sistem mengembalikan string QRIS statis pengurus + kalkulasi `nominal_total = nominal_dasar + kode_unik`.
3. **Persetujuan Ganda & Kontrol Maker-Checker (Aktif Penuh)**:
   - Pembuat transaksi dilarang keras memverifikasi transaksinya sendiri.
4. **Masa Retensi Berkas Ditolak 30 Hari (UU PDP)**:
   - Nilai konfigurasi: `PDP_REJECTED_RETENTION_DAYS=30`.
   - Dijalankan via stored procedure `fn_purge_rejected_pendaftaran_pdp`.
5. **Fokus Lokal 1 Cabang Kopdes & De-scope SIMKOPDES**:
   - Modul sinkronisasi nasional SIMKOPDES dihapus via migrasi `000009`.
   - Database disederhanakan menjadi 64 tabel dasar + 8 partisi bulanan.

---

## 6. Bukti Verifikasi Pengujian (100% PASS)

Hasil eksekusi `go test -v ./...` di seluruh paket sistem:
```text
=== RUN   TestKonkurensiQRIS_AntiTabrakan
--- PASS: TestKonkurensiQRIS_AntiTabrakan (0.82s)
=== RUN   TestIsolasiRLS_LintasKoperasi
--- PASS: TestIsolasiRLS_LintasKoperasi (0.11s)
=== RUN   TestMakerChecker_Pembayaran
--- PASS: TestMakerChecker_Pembayaran (0.08s)
=== RUN   TestAppendOnly_ProteksiTabelLog
--- PASS: TestAppendOnly_ProteksiTabelLog (0.07s)
=== RUN   TestP1_2_Kapasitas_KodeUnik_4Digit_Dan_PeringatanSlot
--- PASS: TestP1_2_Kapasitas_KodeUnik_4Digit_Dan_PeringatanSlot (0.19s)
=== RUN   TestP1_3_AppendOnly_HashChain_Dan_ProteksiTRUNCATE
--- PASS: TestP1_3_AppendOnly_HashChain_Dan_ProteksiTRUNCATE (0.09s)
ok  	github.com/kopdes/merah-putih	1.523s

=== RUN   TestAPI_Auth_LoginAndProfile
--- PASS: TestAPI_Auth_LoginAndProfile (1.60s)
=== RUN   TestAPI_QRIS_CreateAndCheckStatus
--- PASS: TestAPI_QRIS_CreateAndCheckStatus (1.41s)
=== RUN   TestAPI_MakerChecker_HTTP_Violation
--- PASS: TestAPI_MakerChecker_HTTP_Violation (1.40s)
=== RUN   TestK3_ApprovalSuccessFlow_EndToEnd
--- PASS: TestK3_ApprovalSuccessFlow_EndToEnd (1.88s)
=== RUN   TestT1_PaymentBindingToBills
--- PASS: TestT1_PaymentBindingToBills (1.24s)
=== RUN   TestT5_AuthAndPermissions_Reproduction
--- PASS: TestT5_AuthAndPermissions_Reproduction (3.61s)
=== RUN   TestS1_IDOR_PembayaranStatus_Reproduction
--- PASS: TestS1_IDOR_PembayaranStatus_Reproduction (1.87s)
=== RUN   TestS2_QRIS_FeatureFlag_And_Pilot_Validation
--- PASS: TestS2_QRIS_FeatureFlag_And_Pilot_Validation (0.99s)
=== RUN   TestS3_Deployment_Hardening_And_CORS
--- PASS: TestS3_Deployment_Hardening_And_CORS (0.97s)
=== RUN   TestP1_1_Pendaftaran_KYC_MakerChecker_And_KasMutasi
--- PASS: TestP1_1_Pendaftaran_KYC_MakerChecker_And_KasMutasi (1.46s)
=== RUN   TestP1_1_PDP_30Day_Purge
--- PASS: TestP1_1_PDP_30Day_Purge (1.09s)
=== RUN   TestP1_4_Operasional_Metrics_And_Idempotency
--- PASS: TestP1_4_Operasional_Metrics_And_Idempotency (0.99s)
ok  	github.com/kopdes/merah-putih/internal/api	22.807s

=== RUN   TestT2_SpoofedXFF_BypassesLimit_Fixed
--- PASS: TestT2_SpoofedXFF_BypassesLimit_Fixed (0.00s)
=== RUN   TestT2_TrustedProxy_ValidXFF_Allowed
--- PASS: TestT2_TrustedProxy_ValidXFF_Allowed (0.00s)
=== RUN   TestT2_BoundedMemory_LRUEviction
--- PASS: TestT2_BoundedMemory_LRUEviction (0.00s)
=== RUN   TestT2_LoginRateLimiter_AccountAndIPLockout
--- PASS: TestT2_LoginRateLimiter_AccountAndIPLockout (0.12s)
ok  	github.com/kopdes/merah-putih/internal/api/middleware	1.105s

=== RUN   TestT4_RLS_AuditLog_And_JobLog_Isolation
--- PASS: TestT4_RLS_AuditLog_And_JobLog_Isolation (0.08s)
=== RUN   TestK2_InsertOutsidePartition_SucceedsAfterMigration
--- PASS: TestK2_InsertOutsidePartition_SucceedsAfterMigration (0.18s)
=== RUN   TestK1_SuperuserRejectedOnStartup
--- PASS: TestK1_SuperuserRejectedOnStartup (0.03s)
ok  	github.com/kopdes/merah-putih/internal/db	(cached)

=== RUN   TestOutboxWorker_ProcessBatch
--- PASS: TestOutboxWorker_ProcessBatch (0.07s)
ok  	github.com/kopdes/merah-putih/internal/outbox	(cached)

=== RUN   TestCalculateCRC16
--- PASS: TestCalculateCRC16 (0.00s)
=== RUN   TestGenerateDynamicQRIS_AutoLockNominal
--- PASS: TestGenerateDynamicQRIS_AutoLockNominal (0.00s)
=== RUN   TestEMVCo_CRC_Verification_Invalid_Rejected
--- PASS: TestEMVCo_CRC_Verification_Invalid_Rejected (0.00s)
=== RUN   FuzzParseTLV
--- PASS: FuzzParseTLV (0.00s)
ok  	github.com/kopdes/merah-putih/internal/qris	(cached)

=== RUN   TestT3_SecretValidation_RequiresStrongKey
--- PASS: TestT3_SecretValidation_RequiresStrongKey (0.00s)
=== RUN   TestT3_AESGCM_WithAAD_TamperProof
--- PASS: TestT3_AESGCM_WithAAD_TamperProof (0.00s)
=== RUN   TestT3_HKDF_TenantBlindIndex_Isolation
--- PASS: TestT3_HKDF_TenantBlindIndex_Isolation (0.00s)
=== RUN   TestT3_RedactDSN_HidesPassword
--- PASS: TestT3_RedactDSN_HidesPassword (0.00s)
ok  	github.com/kopdes/merah-putih/internal/security	0.783s
```

---

## 7. Lokasi File Dokumentasi & Skrip Terkait

- **Dokumen Markdown di Repositori**: [`docs/LAPORAN_REMEDIASI_DAN_PERUBAHAN_KODE.md`](file:///c:/Users/12/kopdes-merah-putih/docs/LAPORAN_REMEDIASI_DAN_PERUBAHAN_KODE.md)
- **Panduan Front-End Mobile-First**: [`docs/FRONTEND_GUIDE.md`](file:///c:/Users/12/kopdes-merah-putih/docs/FRONTEND_GUIDE.md)
- **Rekonsiliasi Skema**: [`docs/SCOPE_RECONCILIATION.md`](file:///c:/Users/12/kopdes-merah-putih/docs/SCOPE_RECONCILIATION.md)
- **Lembar Uji Lapangan QRIS**: [`docs/QRIS_UJI_LAPANGAN.md`](file:///c:/Users/12/kopdes-merah-putih/docs/QRIS_UJI_LAPANGAN.md)
- **Dokumen PDF Siap Cetak**: [`docs/DOKUMENTASI_LENGKAP_API_KOPDES.pdf`](file:///c:/Users/12/kopdes-merah-putih/docs/DOKUMENTASI_LENGKAP_API_KOPDES.pdf)
- **Dokumen Microsoft Word (DOCX)**: [`docs/DOKUMENTASI_LENGKAP_API_KOPDES.docx`](file:///c:/Users/12/kopdes-merah-putih/docs/DOKUMENTASI_LENGKAP_API_KOPDES.docx)
- **Skrip Backup & Uji Restore PITR**: [`scripts/backup.sh`](file:///c:/Users/12/kopdes-merah-putih/scripts/backup.sh) & [`scripts/restore_test.sh`](file:///c:/Users/12/kopdes-merah-putih/scripts/restore_test.sh)
