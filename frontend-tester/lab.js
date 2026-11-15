(function () {
  "use strict";

  function el(id) {
    return document.getElementById(id);
  }

  function tester() {
    return window.kopdesTester || null;
  }

  // --- Katalog Lab Belajar per Bab (00 s.d. 19) dengan bagian A..F ---
  const CHAPTERS = [
    {
      id: "00",
      title: "00. Mulai Cepat 5 Menit",
      purpose: "A. Tujuan & Peran: Memastikan API sehat dan memuat batas runtime untuk semua peran sebelum login.",
      flow: "B. Alur & Status: GET /healthz -> GET /readyz -> GET /api/v1/public/config [Normal | ModePemeliharaan].",
      liveMethod: "GET",
      livePath: "/healthz",
      liveRole: "unauthenticated",
      fieldNotes: "service='kopdes-api', status='UP', version=versi API aktif.",
      errors: "F. Kesalahan Umum: 503 DATABASE_UNAVAILABLE (DB belum pulih), gagal koneksi/CORS bila ALLOWED_ORIGINS belum cocok.",
      tsCode: "const cfg = await fetchPublicConfig(import.meta.env.VITE_API_BASE_URL);\nconst client = createClient({ baseUrl: import.meta.env.VITE_API_BASE_URL, publicConfig: cfg });",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/healthz`);\nconst json = await res.json();",
      curlCode: "curl -s \"${API_BASE_URL}/healthz\""
    },
    {
      id: "01",
      title: "01. Konfigurasi Frontend Item demi Item",
      purpose: "A. Tujuan & Peran: Mengambil 16 metadata konfigurasi aman tanpa hardcode port/secret di klien.",
      flow: "B. Alur & Status: GET /api/v1/public/config (Cache-Control max-age=60). Hitung clockSkewMs = server_unix_ms - Date.now().",
      liveMethod: "GET",
      livePath: "/api/v1/public/config",
      liveRole: "unauthenticated",
      fieldNotes: "upload_limits, kyc_required_document_types, payment_expiry_seconds, polling_interval_ms, features, captcha (site_key='' saat disabled), pagination.",
      errors: "F. Kesalahan Umum: Tidak menyegarkan config dengan cache:'no-store' saat menerima 503 READ_ONLY_MODE / FEATURE_DISABLED.",
      tsCode: "const cfg = await fetchPublicConfig(baseUrl, { cache: 'no-store' });\nconst skewMs = computeClockSkewMs(cfg.server_unix_ms);",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/public/config`);\nconst { data } = await res.json();",
      curlCode: "curl -s \"${API_BASE_URL}/api/v1/public/config\""
    },
    {
      id: "02",
      title: "02. Autentikasi, MFA Wajib & Manajemen Sesi",
      purpose: "A. Tujuan & Peran: Login 3 cabang (anggota normal, MFA aktif, admin/bendahara/ketua wajib 403 setup_token).",
      flow: "B. Alur & Status: Anonim -> [403 MFA_MANDATORY_SETUP_REQUIRED | 200 mfa_required | 200 access_token] -> Refresh Single-Flight -> Revoke.",
      liveMethod: "POST",
      livePath: "/api/v1/auth/login",
      liveRole: "unauthenticated_login_probe",
      fieldNotes: "access_token (TTL ~900s, memori saja), refresh_token (sekali pakai rotasi), role, koperasi_id.",
      errors: "F. Kesalahan Umum: Memanggil 3x /auth/refresh paralel saat 401 memicu Token Reuse Revocation; 429 MFA_SESSION_LOCKED tidak boleh di-retry.",
      tsCode: "const out = await client.auth.login(email, password);\nif (out.status === 'mfa_setup_required') { await client.auth.setupMFA(); }",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/auth/login`, {\n  method: 'POST', headers: { 'Content-Type': 'application/json' },\n  body: JSON.stringify({ email, password })\n});",
      curlCode: "curl -s -X POST \"${API_BASE_URL}/api/v1/auth/login\" -H \"Content-Type: application/json\" -d '{\"email\":\"anggota@kopdes.example\",\"password\":\"<PASS>\"}'"
    },
    {
      id: "03",
      title: "03. Peran, Izin & Matriks Otorisasi",
      purpose: "A. Tujuan & Peran: Membedakan hak anggota, operator/pengawas (read-only), bendahara, pengurus, ketua, admin.",
      flow: "B. Alur & Status: Pemisahan tugas Maker-Checker + isolasi multi-tenant FORCE RLS per koperasi_id di JWT.",
      liveMethod: "GET",
      livePath: "/api/v1/auth/me",
      liveRole: "anggota_a",
      fieldNotes: "role, koperasi_id, is_platform_admin — UI sembunyikan menu terlarang; server selalu penentu sah.",
      errors: "F. Kesalahan Umum: Mengirim header X-Koperasi-ID (diblokir CORS) atau mengira pengawas bisa memutasi data.",
      tsCode: "if (canVerifyPembayaran(session.role)) { /* tampilkan tombol verifikasi */ }",
      fetchCode: "const me = await fetch(`${API_BASE_URL}/api/v1/auth/me`, { headers: { Authorization: `Bearer ${token}` } });",
      curlCode: "curl -s -H \"Authorization: Bearer ${TOKEN}\" \"${API_BASE_URL}/api/v1/auth/me\""
    },
    {
      id: "04",
      title: "04. Pendaftaran Anggota & Unggah KYC",
      purpose: "A. Tujuan & Peran: Pengajuan anggota baru + foto KTP (<5MB JPEG/PNG/PDF), tampil nik_masked.",
      flow: "B. Alur & Status: diajukan -> perbaikan <-> diajukan -> disetujui (no_anggota) | ditolak -> purge (PERLU VERIFIKASI HUKUM).",
      liveMethod: "POST",
      livePath: "/api/v1/pendaftaran",
      liveRole: "anggota_a_sample_validation",
      fieldNotes: "Respons mengembalikan pendaftaran_id (BUKAN id!), nik_masked ('3201********0001').",
      errors: "F. Kesalahan Umum: Membaca reg.data.id bukan reg.data.pendaftaran_id (DISC-12), mengirim multipart dengan header Content-Type manual.",
      tsCode: "const reg = await client.request('/api/v1/pendaftaran', { method: 'POST', body: form });\nawait client.uploadMultipart(`/api/v1/pendaftaran/${reg.pendaftaran_id}/dokumen`, fd);",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/pendaftaran/${id}/dokumen`, {\n  method: 'POST', headers: { Authorization: `Bearer ${token}` }, body: formData\n});",
      curlCode: "curl -s -X POST \"${API_BASE_URL}/api/v1/pendaftaran/${PENDAFTARAN_ID}/dokumen\" -H \"Authorization: Bearer ${TOKEN}\" -F \"dokumen=@ktp.jpg\" -F \"jenis_dokumen=ktp\""
    },
    {
      id: "05",
      title: "05. Profil Saya, Anggota & Pengguna",
      purpose: "A. Tujuan & Peran: Profil aktif (/auth/me), daftar/status anggota, dan ubah peran pengguna anti-eskalasi.",
      flow: "B. Alur & Status: Anggota: aktif -> nonaktif/keluar. Ubah role: dicek target != self & role_baru <= rank_aktor.",
      liveMethod: "GET",
      livePath: "/api/v1/anggota",
      liveRole: "admin_a",
      fieldNotes: "Gunakan query ?page=1&limit=default_limit sesuai batas paginasi GET /api/v1/public/config.",
      errors: "F. Kesalahan Umum: Mencoba ubah peran akun sendiri atau lintas koperasi (ditolak 403 / 404).",
      tsCode: "const list = await client.request('/api/v1/anggota', { query: { page: 1, limit: cfg.pagination.default_limit } });",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/anggota?page=1&limit=20`, { headers: { Authorization: `Bearer ${token}` } });",
      curlCode: "curl -s -H \"Authorization: Bearer ${ADMIN_TOKEN}\" \"${API_BASE_URL}/api/v1/anggota?page=1&limit=20\""
    },
    {
      id: "06",
      title: "06. Iuran Anggota (KEPUTUSAN AKUNTAN)",
      purpose: "A. Tujuan & Peran: Daftar jenis iuran, tagihan saya, generate periode bulanan, & daftar tunggakan.",
      flow: "B. Alur & Status: Tagihan: belum_lunas -> dibayar_sebagian -> lunas. Label akuntansi: KEPUTUSAN AKUNTAN.",
      liveMethod: "GET",
      livePath: "/api/v1/iuran/tagihan/saya",
      liveRole: "anggota_a",
      fieldNotes: "Admin/non-anggota menerima [] (200 OK, DISC-09). Buat jenis iuran memakai field 'nominal_standar'.",
      errors: "F. Kesalahan Umum: Mengirim 'nominal' alih-alih 'nominal_standar' (DISC-13) atau ketidaksesuaian periode YYYY-MM.",
      tsCode: "const bills = await client.request('/api/v1/iuran/tagihan/saya');",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/iuran/tagihan/saya`, { headers: { Authorization: `Bearer ${token}` } });",
      curlCode: "curl -s -H \"Authorization: Bearer ${TOKEN}\" \"${API_BASE_URL}/api/v1/iuran/tagihan/saya\""
    },
    {
      id: "07",
      title: "07. Katalog Produk, Promo & Keranjang",
      purpose: "A. Tujuan & Peran: Katalog harga umum vs harga anggota, cek harga akhir, kode promo, keranjang.",
      flow: "B. Alur & Status: GET /katalog/produk -> GET /katalog/harga?produk_id=&anggota_id= -> POST /toko/keranjang.",
      liveMethod: "GET",
      livePath: "/api/v1/katalog/produk",
      liveRole: "anggota_a",
      fieldNotes: "harga_umum, harga_anggota (int64). GET /katalog/harga mewajibkan kedua UUID produk_id dan anggota_id (DISC-07).",
      errors: "F. Kesalahan Umum: Memanggil /katalog/harga tanpa anggota_id (400 BAD_REQUEST).",
      tsCode: "const harga = await client.request('/api/v1/katalog/harga', { query: { produk_id, anggota_id } });",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/katalog/produk`, { headers: { Authorization: `Bearer ${token}` } });",
      curlCode: "curl -s -H \"Authorization: Bearer ${TOKEN}\" \"${API_BASE_URL}/api/v1/katalog/harga?produk_id=${PRODUK_ID}&anggota_id=${ANGGOTA_ID}\""
    },
    {
      id: "08",
      title: "08. Toko, Checkout Atomik & Pesanan",
      purpose: "A. Tujuan & Peran: Checkout mengunci stok via stok_mutasi, pembatalan, & kedaluwarsa pesanan.",
      flow: "B. Alur & Status: Checkout -> status='draft' -> buat QRIS (menunggu_pembayaran) -> diproses/selesai | batal/kedaluwarsa.",
      liveMethod: "GET",
      livePath: "/api/v1/toko/pesanan",
      liveRole: "anggota_a",
      fieldNotes: "POST /toko/checkout mengembalikan { pesanan_id, no_pesanan, total_akhir, status: 'draft' } (DISC-08).",
      errors: "F. Kesalahan Umum: Mengira status awal checkout adalah 'menunggu_pembayaran' atau lupa mengunci tombol saat klik ganda.",
      tsCode: "const order = await client.request('/api/v1/toko/checkout', { method: 'POST', body: { metode_serah_terima: 'ambil_di_koperasi' } });",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/toko/pesanan`, { headers: { Authorization: `Bearer ${token}` } });",
      curlCode: "curl -s -H \"Authorization: Bearer ${TOKEN}\" \"${API_BASE_URL}/api/v1/toko/checkout\" -X POST -d '{\"metode_serah_terima\":\"ambil_di_koperasi\"}'"
    },
    {
      id: "09",
      title: "09. Pembayaran QRIS, Kode Unik & Polling",
      purpose: "A. Tujuan & Peran: Kode QR + 3-4 digit kode unik, Idempotency-Key, klaim RRN, Maker-Checker, anti-IDOR.",
      flow: "B. Alur & Status: menunggu_pembayaran -> menunggu_verifikasi -> [menunggu_persetujuan_kedua jika > ambang] -> disetujui.",
      liveMethod: "GET",
      livePath: "/api/v1/pembayaran/antrean",
      liveRole: "admin_a",
      fieldNotes: "POST QRIS kirim nominal_total angka; GET /pembayaran/{id} kirim nominal_total string ('600356.00').",
      errors: "F. Kesalahan Umum: Pakai Idempotency-Key lama dengan body baru (422 IDEMPOTENCY_CONFLICT) atau polling terlalu cepat saat tab tertutup.",
      tsCode: "const key = client.newIdempotencyKey();\nconst qr = await client.createQRISPayment({ jenis_transaksi: 'iuran_bulanan', tagihan_iuran_id: [id] }, key);",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/pembayaran/qris`, {\n  method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': key, 'Content-Type': 'application/json' },\n  body: JSON.stringify(payload)\n});",
      curlCode: "curl -s -X POST \"${API_BASE_URL}/api/v1/pembayaran/qris\" -H \"Authorization: Bearer ${TOKEN}\" -H \"Idempotency-Key: $(uuidgen)\" -d '{\"jenis_transaksi\":\"iuran_bulanan\",\"tagihan_iuran_id\":[\"<UUID>\"]}'"
    },
    {
      id: "10",
      title: "10. Rekening QRIS, Mutasi CSV, Rekonsiliasi & Kas",
      purpose: "A. Tujuan & Peran: Rekening badan hukum EMVCo valid, impor CSV mutasi bank, rekonsiliasi harian WIB, & kas append-only.",
      flow: "B. Alur & Status: Impor CSV -> tercocokkan -> konfirmasi orang ke-2 -> kas_mutasi (append-only).",
      liveMethod: "GET",
      livePath: "/api/v1/pembayaran/rekonsiliasi/harian?tanggal=" + new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Jakarta" }).format(new Date()),
      liveRole: "admin_a",
      fieldNotes: "total_pembayaran_sistem, total_mutasi_bank_cr, total_tercocokkan, selisih_nominal.",
      errors: "F. Kesalahan Umum: Kirim tanggal UTC saat malam/pagi atau pengimpor CSV mencoba langsung meng-approve pembayaran sendiri (403).",
      tsCode: "const rekon = await client.request('/api/v1/pembayaran/rekonsiliasi/harian', { query: { tanggal: getJakartaTodayISO() } });",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/kas/mutasi`, { headers: { Authorization: `Bearer ${adminToken}` } });",
      curlCode: "curl -s -H \"Authorization: Bearer ${ADMIN_TOKEN}\" \"${API_BASE_URL}/api/v1/pembayaran/rekonsiliasi/harian?tanggal=${WIB_DATE}\""
    },
    {
      id: "11",
      title: "11. Laporan Keuangan, Rekap Anggota & Pengawas",
      purpose: "A. Tujuan & Peran: Ringkasan pemasukan, pengeluaran, saldo kas, & jumlah anggota per status (read-only pengawas).",
      flow: "B. Alur & Status: Request paralel GET /laporan/keuangan & /laporan/anggota tanpa PII mentah.",
      liveMethod: "GET",
      livePath: "/api/v1/laporan/keuangan",
      liveRole: "pengawas_a",
      fieldNotes: "periode_aktif, total_pemasukan, total_pengeluaran, saldo_kas_aktif, surplus_defisit.",
      errors: "F. Kesalahan Umum: Mengharapkan rincian_status selalu memiliki seluruh key status bila count == 0.",
      tsCode: "const keu = await client.request('/api/v1/laporan/keuangan');\nconst ang = await client.request('/api/v1/laporan/anggota');",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/laporan/keuangan`, { headers: { Authorization: `Bearer ${token}` } });",
      curlCode: "curl -s -H \"Authorization: Bearer ${ADMIN_TOKEN}\" \"${API_BASE_URL}/api/v1/laporan/keuangan\""
    },
    {
      id: "12",
      title: "12. Format Data: Rupiah & Zona Waktu Asia/Jakarta",
      purpose: "A. Tujuan & Peran: Format uang integer bebas error pembulatan float + penanggalan Asia/Jakarta konsisten.",
      flow: "B. Alur & Status: Terima int atau string desimal -> toWholeRupiah() -> Intl.NumberFormat('id-ID').",
      liveMethod: "GET",
      livePath: "/api/v1/public/config",
      liveRole: "unauthenticated",
      fieldNotes: "timezone='Asia/Jakarta', server_unix_ms untuk kalibrasi jam HP desa.",
      errors: "F. Kesalahan Umum: Memakai new Date().toISOString().slice(0,10) alih-alih Asia/Jakarta.",
      tsCode: "formatRupiah('600356.00'); // 'Rp 600.356'\ngetJakartaTodayISO();",
      fetchCode: "new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(600356);",
      curlCode: "curl -s \"${API_BASE_URL}/api/v1/public/config\" | jq .data.timezone"
    },
    {
      id: "13",
      title: "13. Penanganan Kesalahan, Retry Backoff & Idempotency",
      purpose: "A. Tujuan & Peran: Memetakan seluruh 15 error.code ke Bahasa Indonesia ramah & menghindari retry ganda berbahaya.",
      flow: "B. Alur & Status: Cek kode di ERROR_CATALOG_ID -> 429 hormati Retry-After (non-login) -> tampilkan X-Request-ID.",
      liveMethod: "POST",
      livePath: "/api/v1/auth/login",
      liveRole: "empty_bad_request_probe",
      fieldNotes: "Envelope: { success: false, error: { code: 'BAD_REQUEST', message: '...' } }.",
      errors: "F. Kesalahan Umum: Me-retry /auth/login otomatis saat 429 sehingga IP cepat terkunci 15 menit.",
      tsCode: "catch (err) { if (err instanceof ApiError) { showAlert(getErrorPresentation(err)); } }",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/auth/login`, { method: 'POST', body: '{}' }); // 400 BAD_REQUEST",
      curlCode: "curl -s -X POST \"${API_BASE_URL}/api/v1/auth/login\" -H \"Content-Type: application/json\" -d '{}'"
    },
    {
      id: "14",
      title: "14. Unggahan Berkas KYC/CSV & Privasi PDP",
      purpose: "A. Tujuan & Peran: Validasi ukuran & MIME dari public config, kompres foto KTP klien, zero localStorage.",
      flow: "B. Alur & Status: validateUploadFile -> compressImageInBrowser (<=1600px JPEG) -> XHR/Fetch FormData.",
      liveMethod: "GET",
      livePath: "/api/v1/public/config",
      liveRole: "unauthenticated",
      fieldNotes: "upload_limits.max_kyc_upload_bytes, allowed_mime_types, allowed_csv_mime_types.",
      errors: "F. Kesalahan Umum: Menyimpan JWT atau foto/NIK KTP di localStorage/sessionStorage (risiko XSS & UU 27/2022 PDP).",
      tsCode: "const v = validateUploadFile(file, { maxBytes: cfg.upload_limits.max_kyc_upload_bytes, allowedMimeTypes: cfg.upload_limits.allowed_mime_types });",
      fetchCode: "const fd = new FormData(); fd.append('dokumen', file); fd.append('jenis_dokumen', 'ktp');",
      curlCode: "curl -s \"${API_BASE_URL}/api/v1/public/config\" | jq .data.upload_limits"
    },
    {
      id: "15",
      title: "15. Cara Menguji Tanpa Bertanya ke Backend",
      purpose: "A. Tujuan & Peran: Menjalankan Lab Belajar, 27 Skenario Standar, Matriks 55 Operasi, & SDK Unit Test.",
      flow: "B. Alur & Status: seed_test_accounts -> uitester -> Diagnostik -> Skenario -> Unduh Proof Report.",
      liveMethod: "GET",
      livePath: "/readyz",
      liveRole: "unauthenticated",
      fieldNotes: "status='READY' menandakan PostgreSQL 16 + RLS siap diuji.",
      errors: "F. Kesalahan Umum: Mencentang brute-force di tengah uji rutin lalu heran terkena 429 pada request berikutnya.",
      tsCode: "// Jalankan di terminal repo:\n// npm --prefix frontend-reference run typecheck && npm --prefix frontend-reference test",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/readyz`);",
      curlCode: "curl -s \"${API_BASE_URL}/readyz\""
    },
    {
      id: "16",
      title: "16. Checklist Pra-Produksi UX & Kepatuhan Klien",
      purpose: "A. Tujuan & Peran: Memastikan mobile-first 44x44px, ARIA, zero PII, dan peninjauan hukum/akuntansi.",
      flow: "B. Alur & Status: Verifikasi zero-PII DOM + tombol 1x klik + PERLU VERIFIKASI HUKUM + KEPUTUSAN AKUNTAN.",
      liveMethod: "GET",
      livePath: "/healthz",
      liveRole: "unauthenticated",
      fieldNotes: "Lihat panel 6.4 Pemindaian PII & Unduh Laporan Bukti untuk daftar UNVERIFIED.",
      errors: "F. Kesalahan Umum: Lupa mencabut AbortController / Poller saat komponen unmount.",
      tsCode: "useEffect(() => { const p = createPaymentPoller(...); p.start(); return () => p.stop(); }, []);",
      fetchCode: "// Periksa zero storage:\n// Object.keys(localStorage).length === 0 && Object.keys(sessionStorage).length === 0",
      curlCode: "curl -s \"${API_BASE_URL}/healthz\""
    },
    {
      id: "17",
      title: "17. Contoh Integrasi React, Vue 3 & Vanilla JS",
      purpose: "A. Tujuan & Peran: Menjalankan adapter siap salin di React, Vue 3 Composable, dan Vanilla JS.",
      flow: "B. Alur & Status: Ada pada frontend-reference/src/examples/* serta frontend-tester/reference/*.html.",
      liveMethod: "GET",
      livePath: "/api/v1/public/config",
      liveRole: "unauthenticated",
      fieldNotes: "Gunakan halaman reference/index.html untuk melihat versi HTML polos berjalan nyata.",
      errors: "F. Kesalahan Umum: Menimpa Content-Type atau tidak menghubungkan onForceLogout ke router aplikasi.",
      tsCode: "import { useKopdesAuth, QrisPaymentCard } from './examples/react-example';",
      fetchCode: "import { initKopdesVanillaApp } from './examples/vanilla-example.js';",
      curlCode: "curl -s \"${API_BASE_URL}/api/v1/public/config\""
    },
    {
      id: "18",
      title: "18. Glosarium Istilah UI/UX & Status Ramah Warga",
      purpose: "A. Tujuan & Peran: Memastikan seluruh label status database tampil dalam Bahasa Indonesia konsisten.",
      flow: "B. Alur & Status: diajukan/perbaikan/disetujui/ditolak; draft/menunggu_pembayaran/disetujui.",
      liveMethod: "GET",
      livePath: "/api/v1/iuran/jenis",
      liveRole: "anggota_a",
      fieldNotes: "Lihat pemetaan status lengkap di docs/frontend/18-glosarium-ui-ux.md.",
      errors: "F. Kesalahan Umum: Menampilkan kode snake_case mentah seperti 'menunggu_persetujuan_kedua' langsung ke warga.",
      tsCode: "const LABEL_STATUS = { menunggu_verifikasi: 'Bukti Dikirim - Menunggu Dicek', disetujui: 'Lunas / Sah' };",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/iuran/jenis`, { headers: { Authorization: `Bearer ${token}` } });",
      curlCode: "curl -s -H \"Authorization: Bearer ${TOKEN}\" \"${API_BASE_URL}/api/v1/iuran/jenis\""
    },
    {
      id: "19",
      title: "19. Deploy, CORS, Reverse Proxy, BFF & Mode Pemeliharaan",
      purpose: "A. Tujuan & Peran: Memisahkan env dev/test/prod, ALLOWED_ORIGINS ketat, TRUSTED_PROXIES, BFF+CSRF, READ_ONLY_MODE.",
      flow: "B. Alur & Status: Origin cocok -> CORS lolos; BFF HttpOnly wajib anti-CSRF; 503 saat READ_ONLY_MODE.",
      liveMethod: "GET",
      livePath: "/api/v1/public/config",
      liveRole: "unauthenticated",
      fieldNotes: "features.read_only_mode=false pada operasional normal.",
      errors: "F. Kesalahan Umum: Memakai ALLOWED_ORIGINS=* di APP_ENV=production atau lupa menambahkan TRUSTED_PROXIES.",
      tsCode: "if (cfg.features.read_only_mode) { /* tampilkan banner pemeliharaan & nonaktifkan tombol tulis */ }",
      fetchCode: "const res = await fetch(`${API_BASE_URL}/api/v1/public/config`, { cache: 'no-store' });",
      curlCode: "curl -s -i \"${API_BASE_URL}/api/v1/public/config\" -H \"Origin: ${ALLOWED_ORIGIN}\""
    }
  ];

  // --- B.1 Diagnostik Lingkungan ---
  async function runDiagnostics() {
    const t = tester();
    const outPre = el("diagnosticsOutputPre");
    if (!t || !outPre) return;
    outPre.textContent = "Menjalankan Diagnostik Lingkungan...";
    const report = { checked_at: new Date().toISOString(), api_base: t.getApiBase(), checks: {} };
    try {
      const h = await t.apiRequest({ method: "GET", path: "/healthz", token: "" });
      const rz = await t.apiRequest({ method: "GET", path: "/readyz", token: "" });
      const pub = await t.apiRequest({ method: "GET", path: "/api/v1/public/config", token: "" });
      let corsProbe = null;
      try {
        const cr = await fetch("/api/probe-cors", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            target_url: t.getApiBase() + "/api/v1/public/config",
            origin: window.location.origin,
            method: "GET"
          })
        });
        corsProbe = await cr.json();
      } catch (ce) {
        corsProbe = { error: ce.message };
      }
      const serverMs = pub.json && pub.json.data ? pub.json.data.server_unix_ms : null;
      const clockSkewMs = serverMs ? Math.trunc(serverMs - Date.now()) : 0;
      const d = (pub.json && pub.json.data) || {};
      report.summary = h.status === 200 && rz.status === 200 && pub.status === 200 ? "SEMUA SEHAT (PASS)" : "ADA PERINGATAN";
      report.clock_skew_ms = clockSkewMs;
      report.checks = {
        healthz: { status: h.status, data: h.json && h.json.data },
        readyz: { status: rz.status, data: rz.json && rz.json.data },
        public_config: {
          status: pub.status,
          cache_control: pub.headers.get("Cache-Control"),
          api_version: d.api_version,
          timezone: d.timezone,
          features: d.features,
          captcha: d.captcha,
          upload_limits: d.upload_limits,
          pagination: d.pagination
        },
        cors_probe_own_origin: corsProbe
      };
      t.setDiagnostics(report);
      outPre.textContent = JSON.stringify(report, null, 2);
    } catch (err) {
      report.summary = "GAGAL: " + err.message;
      t.setDiagnostics(report);
      outPre.textContent = JSON.stringify(report, null, 2);
    }
  }

  // --- B.2 Config Playground ---
  function validatePlaygroundSchema(cfg) {
    const errs = [];
    if (!cfg || typeof cfg !== "object") return ["JSON bukan objek."];
    if (typeof cfg.api_version !== "string" || !/^[0-9]+\.[0-9]+\.[0-9]+/.test(cfg.api_version)) {
      errs.push("api_version harus berformat semver (mis. 1.1.0)");
    }
    if (cfg.timezone !== "Asia/Jakarta") {
      errs.push("timezone wajib 'Asia/Jakarta'");
    }
    if (!cfg.upload_limits || typeof cfg.upload_limits.max_kyc_upload_bytes !== "number" || cfg.upload_limits.max_kyc_upload_bytes < 524288) {
      errs.push("upload_limits.max_kyc_upload_bytes minimal 524288 byte");
    }
    if (!cfg.upload_limits || !Array.isArray(cfg.upload_limits.allowed_mime_types) || cfg.upload_limits.allowed_mime_types.length === 0) {
      errs.push("upload_limits.allowed_mime_types tidak boleh kosong");
    }
    if (typeof cfg.payment_expiry_seconds !== "number" || cfg.payment_expiry_seconds < 60) {
      errs.push("payment_expiry_seconds minimal 60 detik");
    }
    if (typeof cfg.polling_interval_ms !== "number" || cfg.polling_interval_ms < 1000) {
      errs.push("polling_interval_ms minimal 1000 ms");
    }
    if (!cfg.features || typeof cfg.features.read_only_mode !== "boolean" || typeof cfg.features.payments_enabled !== "boolean") {
      errs.push("features wajib memuat boolean read_only_mode, payments_enabled, registration_enabled, login_enabled");
    }
    if (!cfg.captcha || (cfg.captcha.enabled === false && cfg.captcha.site_key !== "")) {
      errs.push("captcha.site_key wajib string kosong saat captcha.enabled=false");
    }
    if (!cfg.pagination || cfg.pagination.default_limit < 1 || cfg.pagination.default_limit > cfg.pagination.max_limit) {
      errs.push("pagination wajib memenuhi 1 <= default_limit <= max_limit");
    }
    return errs;
  }

  async function loadPlaygroundConfig() {
    const t = tester();
    const ta = el("configPlaygroundTextarea");
    const pre = el("configPlaygroundPreview");
    if (!t || !ta) return;
    const res = await t.apiRequest({ method: "GET", path: "/api/v1/public/config", token: "" });
    if (res.json && res.json.data) {
      ta.value = JSON.stringify(res.json.data, null, 2);
      validateAndPreviewPlayground();
    } else {
      pre.textContent = "Gagal memuat /api/v1/public/config: HTTP " + res.status;
    }
  }

  function validateAndPreviewPlayground() {
    const t = tester();
    const ta = el("configPlaygroundTextarea");
    const pre = el("configPlaygroundPreview");
    if (!ta || !pre) return;
    try {
      const parsed = JSON.parse(ta.value);
      const errs = validatePlaygroundSchema(parsed);
      const feat = parsed.features || {};
      const impact = {
        schema_valid: errs.length === 0,
        schema_errors: errs,
        ui_preview: {
          banner_pemeliharaan: feat.read_only_mode ? "TAMPIL: Sistem Baca-Saja (Mutasi ditutup)" : "TIDAK TAMPIL",
          tombol_bayar_qris: !feat.read_only_mode && feat.payments_enabled ? "AKTIF" : "NONAKTIF (503)",
          form_pendaftaran: !feat.read_only_mode && feat.registration_enabled ? "AKTIF" : "NONAKTIF (503)",
          form_login: feat.login_enabled ? "AKTIF" : "NONAKTIF (503)",
          mode_qris: feat.qris_dynamic_enabled ? "QRIS Dinamis Auto-Lock" : "QRIS Statis + Kode Unik",
          widget_captcha: parsed.captcha && parsed.captcha.enabled ? "AKTIF (" + parsed.captcha.provider + ")" : "NONAKTIF",
          maks_upload_kyc_mb: parsed.upload_limits ? (parsed.upload_limits.max_kyc_upload_bytes / (1024 * 1024)).toFixed(2) + " MB" : "-",
          paginasi_default_max: parsed.pagination ? parsed.pagination.default_limit + " / " + parsed.pagination.max_limit : "-"
        }
      };
      if (t) t.setPlaygroundStatus(impact);
      pre.textContent = JSON.stringify(impact, null, 2);
    } catch (e) {
      pre.textContent = "JSON tidak valid: " + e.message;
    }
  }

  // --- B.3 Simulator Kondisi ---
  function initSimulators() {
    const out = el("simulatorOutputPre");
    if (!out) return;
    el("btnSimOffline").addEventListener("click", function () {
      out.textContent = JSON.stringify({
        simulasi: "Offline / Sinyal Terputus",
        navigator_onLine: navigator.onLine,
        penanganan_sdk: "Matikan tombol mutasi, pause polling QRIS otomatis, tampilkan pesan ramah 'Koneksi terputus — coba lagi saat sinyal desa kembali'."
      }, null, 2);
    });
    el("btnSimTimeout").addEventListener("click", async function () {
      const ac = new AbortController();
      setTimeout(function () { ac.abort(); }, 25);
      let aborted = false;
      try {
        await fetch(tester().getApiBase() + "/healthz", { signal: ac.signal });
      } catch (e) {
        aborted = e.name === "AbortError";
      }
      out.textContent = JSON.stringify({
        simulasi: "Timeout AbortController",
        aborted_cleanly: aborted,
        penanganan_sdk: "Gunakan AbortController 15000ms (selaras statement_timeout PostgreSQL 15s) dan abort di cleanup komponen."
      }, null, 2);
    });
    el("btnSim401SingleFlight").addEventListener("click", async function () {
      let refreshCalls = 0;
      let sharedPromise = null;
      function singleFlightRefresh() {
        if (!sharedPromise) {
          sharedPromise = new Promise(function (resolve) {
            refreshCalls++;
            setTimeout(function () { sharedPromise = null; resolve("new-access-jwt"); }, 30);
          });
        }
        return sharedPromise;
      }
      const tokens = await Promise.all([singleFlightRefresh(), singleFlightRefresh(), singleFlightRefresh()]);
      out.textContent = JSON.stringify({
        simulasi: "3 Request Paralel Menerima 401 Bersamaan",
        total_panggilan_refresh_ke_server: refreshCalls,
        ketiga_request_dapat_token_sama: tokens[0] === tokens[1] && tokens[1] === tokens[2],
        pencegahan_reuse_detection: true
      }, null, 2);
    });
    el("btnSim429RetryAfter").addEventListener("click", async function () {
      const retryAfterSec = 1;
      const t0 = Date.now();
      await new Promise(function (r) { setTimeout(r, retryAfterSec * 1000); });
      out.textContent = JSON.stringify({
        simulasi: "429 TOO_MANY_REQUESTS dengan Header Retry-After: " + retryAfterSec,
        durasi_tunggu_ms: Date.now() - t0,
        catatan: "Diterapkan pada endpoint bisnis/polling, DILARANG auto-retry pada /auth/login dan /auth/mfa/verify."
      }, null, 2);
    });
    el("btnSim503ReadOnly").addEventListener("click", function () {
      out.textContent = JSON.stringify({
        simulasi: "503 READ_ONLY_MODE / FEATURE_DISABLED",
        kode_server: "READ_ONLY_MODE",
        tindakan_otomatis_sdk: "Paksa muat ulang GET /api/v1/public/config dengan cache:'no-store', tampilkan banner pemeliharaan, nonaktifkan mutasi."
      }, null, 2);
    });
  }

  // --- B.4 Panduan Terpandu 7 Langkah ("Berikutnya") ---
  const GUIDED_STEPS = [
    { title: "Langkah 1 / 7: Muat Konfigurasi Publik (/api/v1/public/config)", desc: "Mengambil versi API, timezone, upload limits, fitur aktif, dan pagination." },
    { title: "Langkah 2 / 7: Masuk Anggota & Cek Profil (/auth/me)", desc: "Masuk dengan anggota_a ke memori JS tanpa localStorage dan verifikasi GET /api/v1/auth/me." },
    { title: "Langkah 3 / 7: Cek Jenis Iuran & Tagihan Saya", desc: "Memanggil GET /api/v1/iuran/jenis dan GET /api/v1/iuran/tagihan/saya." },
    { title: "Langkah 4 / 7: Lihat Katalog, Tambah Keranjang & Checkout", desc: "Memanggil produk -> tambah 1 qty keranjang -> POST /toko/checkout (status awal 'draft')." },
    { title: "Langkah 5 / 7: Buat Pembayaran QRIS + Kode Unik + Idempotency-Key", desc: "Membayar pesanan tadi memakai Idempotency-Key UUIDv4 & menampilkan nominal total + kode unik." },
    { title: "Langkah 6 / 7: Klaim RRN Anggota & Verifikasi Bendahara (Maker-Checker)", desc: "Klaim RRN lalu login bendahara berbeda untuk verifikasi GET /antrean & POST .../verifikasi." },
    { title: "Langkah 7 / 7: Rekonsiliasi Harian & Laporan Keuangan", desc: "Mengambil rekonsiliasi tanggal Asia/Jakarta dan GET /api/v1/laporan/keuangan." }
  ];
  let guidedIdx = 0;
  const guidedCtx = { anggotaToken: "", pesananId: "", pembayaranId: "" };

  function renderGuidedStep() {
    const s = GUIDED_STEPS[guidedIdx];
    el("guidedStepTitle").textContent = s.title;
    el("guidedStepDesc").textContent = s.desc;
    el("btnGuidedPrev").disabled = guidedIdx === 0;
    el("btnGuidedNext").disabled = guidedIdx >= GUIDED_STEPS.length - 1;
  }

  async function runCurrentGuidedStep() {
    const t = tester();
    const out = el("guidedStepStatus");
    if (!t || !out) return;
    out.textContent = "Menjalankan " + GUIDED_STEPS[guidedIdx].title + " ...";
    try {
      const cfg = t.state.config;
      if (guidedIdx === 0) {
        const res = await t.apiRequest({ method: "GET", path: "/api/v1/public/config", token: "" });
        out.textContent = JSON.stringify({ status: res.status, data: res.json.data }, null, 2);
        return;
      }
      if (!t.getTestPassword()) {
        out.textContent = "Isi Kata Sandi Akun Uji di Bagian 0 terlebih dahulu untuk langkah autentikasi!";
        return;
      }
      if (guidedIdx === 1) {
        const a = await t.loginAccount({ email: "anggota_a@kopdes-test.local", koperasi_id: cfg.koperasi_a_id });
        guidedCtx.anggotaToken = a.accessToken;
        const me = await t.apiRequest({ method: "GET", path: "/api/v1/auth/me", token: guidedCtx.anggotaToken });
        out.textContent = JSON.stringify({ login_role: a.loginData.role, me_status: me.status, me: me.json.data }, null, 2);
        return;
      }
      if (!guidedCtx.anggotaToken) {
        const a = await t.loginAccount({ email: "anggota_a@kopdes-test.local", koperasi_id: cfg.koperasi_a_id });
        guidedCtx.anggotaToken = a.accessToken;
      }
      if (guidedIdx === 2) {
        const j = await t.apiRequest({ method: "GET", path: "/api/v1/iuran/jenis", token: guidedCtx.anggotaToken });
        const s = await t.apiRequest({ method: "GET", path: "/api/v1/iuran/tagihan/saya", token: guidedCtx.anggotaToken });
        out.textContent = JSON.stringify({ jenis_status: j.status, tagihan_status: s.status, tagihan_count: Array.isArray(s.json.data) ? s.json.data.length : 0 }, null, 2);
        return;
      }
      if (guidedIdx === 3) {
        await t.apiRequest({ method: "POST", path: "/api/v1/toko/keranjang", token: guidedCtx.anggotaToken, json: { produk_id: cfg.resources.produk_a_id, qty: 1 } });
        const co = await t.apiRequest({ method: "POST", path: "/api/v1/toko/checkout", token: guidedCtx.anggotaToken, json: { metode_serah_terima: "ambil_di_koperasi", catatan: "Panduan Terpandu" } });
        guidedCtx.pesananId = co.json && co.json.data ? co.json.data.pesanan_id : "";
        out.textContent = JSON.stringify({ checkout_status: co.status, data: co.json.data }, null, 2);
        return;
      }
      if (guidedIdx === 4) {
        if (!guidedCtx.pesananId) {
          out.textContent = "Jalankan Langkah 4 dulu untuk membuat pesanan.";
          return;
        }
        const key = t.generateUUIDv4();
        const qr = await t.apiRequest({
          method: "POST",
          path: "/api/v1/pembayaran/qris",
          token: guidedCtx.anggotaToken,
          headers: { "Idempotency-Key": key },
          json: {
            pesanan_id: guidedCtx.pesananId,
            anggota_id: cfg.resources.anggota_a_id,
            metode_pembayaran_id: cfg.resources.metode_pembayaran_a_id,
            rekening_qris_id: cfg.resources.rekening_qris_a_id
          }
        });
        guidedCtx.pembayaranId = qr.json && qr.json.data ? qr.json.data.pembayaran_id : "";
        out.textContent = JSON.stringify({ qris_status: qr.status, idempotency_key: key, nominal_total: t.formatRupiah(qr.json.data.nominal_total), data: qr.json.data }, null, 2);
        return;
      }
      if (guidedIdx === 5) {
        if (!guidedCtx.pembayaranId) {
          out.textContent = "Jalankan Langkah 5 dulu untuk membuat pembayaran QRIS.";
          return;
        }
        await t.apiRequest({ method: "POST", path: "/api/v1/pembayaran/" + guidedCtx.pembayaranId + "/bukti", token: guidedCtx.anggotaToken, json: { referensi_rrn: "RRN-GUIDE-" + (Date.now() % 100000), catatan: "Sudah transfer" } });
        const ben = await t.loginAccount({ email: "bendahara_a@kopdes-test.local", koperasi_id: cfg.koperasi_a_id });
        const ver = await t.apiRequest({ method: "POST", path: "/api/v1/pembayaran/" + guidedCtx.pembayaranId + "/verifikasi", token: ben.accessToken, json: { keputusan: "disetujui", catatan: "Terverifikasi panduan" } });
        out.textContent = JSON.stringify({ verify_status: ver.status, data: ver.json }, null, 2);
        return;
      }
      if (guidedIdx === 6) {
        const adm = await t.loginAccount({ email: "admin_a@kopdes-test.local", koperasi_id: cfg.koperasi_a_id });
        const rk = await t.apiRequest({ method: "GET", path: "/api/v1/pembayaran/rekonsiliasi/harian?tanggal=" + encodeURIComponent(t.getTodayJakartaDate()), token: adm.accessToken });
        const lap = await t.apiRequest({ method: "GET", path: "/api/v1/laporan/keuangan", token: adm.accessToken });
        out.textContent = JSON.stringify({ rekon_status: rk.status, rekonsiliasi: rk.json.data, laporan_status: lap.status, laporan: lap.json.data }, null, 2);
      }
    } catch (err) {
      out.textContent = "Error: " + err.message;
    }
  }

  // --- B.5 Lab Belajar per Bab ---
  function renderSelectedChapter(liveResp) {
    const sel = el("labChapterSelect");
    const pre = el("labChapterContentPre");
    if (!sel || !pre) return;
    const ch = CHAPTERS.find(function (c) { return c.id === sel.value; }) || CHAPTERS[0];
    let txt = "=== " + ch.title + " ===\n\n";
    txt += ch.purpose + "\n\n";
    txt += ch.flow + "\n\n";
    txt += "C. Target Eksekusi Live: " + ch.liveMethod + " " + ch.livePath + " (akun: " + ch.liveRole + ")\n\n";
    txt += "D. Penjelasan Field Utama:\n" + ch.fieldNotes + "\n";
    if (liveResp) {
      txt += "\n--- Respons Live Server ---\n" + JSON.stringify(liveResp, null, 2) + "\n";
    }
    txt += "\nE. Contoh Kode (bebas port hardcode):\n[TypeScript SDK]\n" + ch.tsCode + "\n\n[fetch()]\n" + ch.fetchCode + "\n\n[cURL]\n" + ch.curlCode + "\n\n";
    txt += ch.errors + "\n";
    pre.textContent = txt;
  }

  async function runSelectedChapterLive() {
    const t = tester();
    const sel = el("labChapterSelect");
    if (!t || !sel) return;
    const ch = CHAPTERS.find(function (c) { return c.id === sel.value; }) || CHAPTERS[0];
    try {
      let token = "";
      let bodyJson;
      if (ch.liveRole === "empty_bad_request_probe") {
        bodyJson = {};
      } else if (ch.liveRole === "unauthenticated_login_probe") {
        if (!t.getTestPassword()) {
          renderSelectedChapter({ info: "Isi Kata Sandi Akun Uji di Panel 0 untuk uji login live." });
          return;
        }
        bodyJson = { email: "anggota_a@kopdes-test.local", password: t.getTestPassword(), koperasi_id: t.state.config.koperasi_a_id };
      } else if (ch.liveRole !== "unauthenticated") {
        if (!t.getTestPassword()) {
          renderSelectedChapter({ info: "Isi Kata Sandi Akun Uji di Panel 0 untuk mengeksekusi endpoint terautentikasi." });
          return;
        }
        const acc = await t.loginAccount({ email: ch.liveRole + "@kopdes-test.local", koperasi_id: t.state.config.koperasi_a_id });
        token = acc.accessToken;
        if (ch.liveRole === "anggota_a_sample_validation") {
          bodyJson = {}; // Probe 400 BAD_REQUEST aman tanpa mengotori DB pendaftaran
        }
      }
      const res = await t.apiRequest({ method: ch.liveMethod, path: ch.livePath, token: token, json: bodyJson });
      renderSelectedChapter({ http_status: res.status, x_request_id: res.requestId, body: res.json || res.text });
    } catch (err) {
      renderSelectedChapter({ error: err.message });
    }
  }

  document.addEventListener("DOMContentLoaded", function () {
    const sel = el("labChapterSelect");
    if (sel) {
      for (const ch of CHAPTERS) {
        const opt = document.createElement("option");
        opt.value = ch.id;
        opt.textContent = ch.title;
        sel.appendChild(opt);
      }
      sel.addEventListener("change", function () { renderSelectedChapter(null); });
      renderSelectedChapter(null);
    }

    el("btnRunDiagnostics")?.addEventListener("click", runDiagnostics);
    el("btnLoadPublicConfigPlayground")?.addEventListener("click", loadPlaygroundConfig);
    el("btnPreviewReadOnlyPlayground")?.addEventListener("click", function () {
      const ta = el("configPlaygroundTextarea");
      try {
        const cur = JSON.parse(ta.value);
        if (cur && cur.features) cur.features.read_only_mode = true;
        ta.value = JSON.stringify(cur, null, 2);
      } catch (_) {}
      validateAndPreviewPlayground();
    });
    el("btnValidateConfigPlayground")?.addEventListener("click", validateAndPreviewPlayground);

    initSimulators();

    renderGuidedStep();
    el("btnGuidedPrev")?.addEventListener("click", function () { if (guidedIdx > 0) { guidedIdx--; renderGuidedStep(); } });
    el("btnGuidedNext")?.addEventListener("click", function () { if (guidedIdx < GUIDED_STEPS.length - 1) { guidedIdx++; renderGuidedStep(); } });
    el("btnGuidedRunStep")?.addEventListener("click", runCurrentGuidedStep);

    el("btnLabRunLive")?.addEventListener("click", runSelectedChapterLive);
    function copySnippet(field) {
      const ch = CHAPTERS.find(function (c) { return c.id === el("labChapterSelect").value; }) || CHAPTERS[0];
      if (navigator.clipboard) navigator.clipboard.writeText(ch[field]);
    }
    el("btnLabCopyTS")?.addEventListener("click", function () { copySnippet("tsCode"); });
    el("btnLabCopyFetch")?.addEventListener("click", function () { copySnippet("fetchCode"); });
    el("btnLabCopyCurl")?.addEventListener("click", function () { copySnippet("curlCode"); });
  });
})();
