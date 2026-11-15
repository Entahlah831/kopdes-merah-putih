---
name: security-baseline-kopdes
description: Gunakan setiap kali membuat, mengubah, atau mengaudit endpoint HTTP, autentikasi, upload berkas KYC, rate limiter, atau konfigurasi deployment Kopdes Merah Putih sesuai daftar periksa OWASP.
---

# Daftar Periksa Keamanan Per Endpoint (`security-baseline-kopdes`)

Setiap endpoint HTTP wajib lulus seluruh butir relevan berikut (merujuk OWASP ASVS & API Security Top 10):

1. **Autentikasi Sesi Opaque (OWASP A07)**:
   - Endpoint non-publik mewajibkan header `Authorization: Bearer <token>` dengan sesi server-side di tabel `sesi` (hash SHA-256 token di DB, idle & absolute TTL dari env).
   - Nonaktif akun, perubahan peran, atau ganti/reset password wajib mencabut seluruh sesi pengguna secara instan.
   - MFA TOTP wajib untuk peran pada `MFA_REQUIRED_ROLES` (`admin`, `bendahara`) dengan kode pemulihan ter-hash.
2. **Otorisasi Per Peran (OWASP API5 / A01)**:
   - Periksa peran di middleware dan cocokkan dengan `docs/roles_matrix.json`. Tolak peran tidak berhak dengan `403`.
3. **Kepemilikan Sumber Daya / Anti-IDOR (OWASP API1)**:
   - Anggota hanya boleh mengakses profil, tagihan, pesanan, pembayaran, dan bukti milik `user_id`/`anggota_id` sendiri. Akses milik orang lain ditolak `403`/`404`.
   - Pembuat/pengaju pembayaran tidak boleh menjadi penyetuju pembayaran yang sama.
4. **Validasi Input & Anti-Mass Assignment (OWASP API3 / A03)**:
   - Gunakan `json.NewDecoder(r.Body)` dengan `dec.DisallowUnknownFields()` pada payload JSON mutatif untuk menolak field tak dikenal (`peran`, `status`, `nominal`, dll. yang bukan hak klien).
   - Nominal harga/tagihan/ongkir wajib dihitung ulang di server dari database, bukan dipercaya dari input klien.
5. **Batas Ukuran Body & Timeout (OWASP A05)**:
   - Terapkan `BodyLimit` (`MAX_BODY_BYTES`), batas khusus unggahan KYC (`MAX_KYC_UPLOAD_BYTES`), `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, dan timeout query SQL (`DB_STATEMENT_TIMEOUT`, `DB_LOCK_TIMEOUT`).
6. **Respons Error Tanpa Detail Internal (OWASP A09)**:
   - Jangan mengembalikan error PostgreSQL mentah, nama tabel, query SQL, atau stack trace ke klien. Kembalikan kode error terstruktur + pesan ramah pengguna.
7. **Log & Audit Tanpa PII (OWASP A09)**:
   - Dilarang mencetak NIK, nomor HP utuh, email utuh, password, token sesi, DSN berpassword, atau isi rahasia `.env` ke stdout/log/audit_log.
8. **Perbandingan Rahasia Waktu Konstan (OWASP A02)**:
   - Gunakan `subtle.ConstantTimeCompare` untuk hash token/kode pemulihan/aktivasi; pertahankan verifikasi password dummy konstan saat akun login tidak ditemukan.
9. **Unggahan & Unduhan Berkas Aman (OWASP A04 / A01)**:
   - Abaikan nama berkas klien; simpan dengan ID acak di luar root web; verifikasi ukuran, *magic bytes* (PNG/JPEG), dan dimensi gambar asli (`image.DecodeConfig`).
   - Enkripsi berkas KYC (AES-256-GCM DEK per berkas dibungkus KEK dari env + `key_id`) sebelum menyentuh disk.
   - Sajikan unduhan dengan header `Content-Disposition: attachment` dan `X-Content-Type-Options: nosniff`.
   - Ekspor CSV wajib menetralkan karakter pembuka formula (`=`, `+`, `-`, `@`).
10. **Header Keamanan & CORS (OWASP A05)**:
    - Pasang `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `Cache-Control: no-store` untuk data sensitif, dan CORS allowlist ketat dari env `ALLOWED_ORIGINS` (tolak `*` di produksi).
11. **Pembatasan Laju & Anti-CGNAT Lockout (OWASP API4)**:
    - Simpan state pembatasan laju & lockout di tabel PostgreSQL (`rate_limits` / `users`), bukan RAM proses.
    - Percayai `X-Forwarded-For`/`CF-Connecting-IP` hanya dari CIDR di `TRUSTED_PROXIES`.
    - Terapkan penundaan/penguncian progresif per akun; untuk IP gunakan ambang lunak pemicu CAPTCHA (`CAPTCHA_ENABLED=true` diverifikasi server ke Turnstile), jangan memblokir keras IP bersama warga desa.
12. **Keselamatan Database & Kontainer**:
    - Koneksi runtime wajib non-superuser (`kopdes_app`); gagal start bila superuser/bypassrls. Container non-root, port DB tidak diekspos ke publik, nol pengecualian global di `gosec`.
