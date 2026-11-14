import os
import subprocess
import zipfile
import html

# Path target output
DOCS_DIR = os.path.abspath("docs")
HTML_FILE = os.path.join(DOCS_DIR, "DOKUMENTASI_LENGKAP_API_KOPDES.html")
PDF_FILE = os.path.join(DOCS_DIR, "DOKUMENTASI_LENGKAP_API_KOPDES.pdf")
DOCX_FILE = os.path.join(DOCS_DIR, "DOKUMENTASI_LENGKAP_API_KOPDES.docx")

os.makedirs(DOCS_DIR, exist_ok=True)

html_content = """<!DOCTYPE html>
<html lang="id">
<head>
  <meta charset="UTF-8">
  <title>Dokumentasi Lengkap API & Backend — Kopdes Merah Putih</title>
  <style>
    @page {
      size: A4;
      margin: 18mm 15mm 18mm 15mm;
    }
    body {
      font-family: 'Segoe UI', -apple-system, BlinkMacSystemFont, Roboto, Helvetica, Arial, sans-serif;
      color: #1e293b;
      line-height: 1.6;
      font-size: 13px;
      margin: 0;
      padding: 0;
    }
    .cover-page {
      text-align: center;
      padding: 80px 20px 40px 20px;
      page-break-after: always;
      border-bottom: 3px solid #b91c1c;
    }
    .badge-flag {
      display: inline-block;
      background: linear-gradient(180deg, #dc2626 50%, #ffffff 50%);
      border: 1px solid #cbd5e1;
      border-radius: 4px;
      width: 48px;
      height: 32px;
      box-shadow: 0 2px 4px rgba(0,0,0,0.1);
      margin-bottom: 20px;
    }
    .cover-title {
      font-size: 28px;
      font-weight: 800;
      color: #0f172a;
      margin: 10px 0;
      text-transform: uppercase;
      letter-spacing: 0.5px;
    }
    .cover-subtitle {
      font-size: 16px;
      color: #b91c1c;
      font-weight: 600;
      margin-bottom: 30px;
    }
    .cover-meta {
      margin-top: 60px;
      font-size: 13px;
      color: #64748b;
      line-height: 1.8;
      border-top: 1px solid #e2e8f0;
      padding-top: 25px;
      display: inline-block;
      text-align: left;
    }
    h1 {
      font-size: 20px;
      color: #0f172a;
      border-bottom: 2px solid #b91c1c;
      padding-bottom: 6px;
      margin-top: 36px;
      margin-bottom: 14px;
      page-break-after: avoid;
    }
    h2 {
      font-size: 16px;
      color: #1e293b;
      border-left: 4px solid #b91c1c;
      padding-left: 10px;
      margin-top: 26px;
      margin-bottom: 10px;
      page-break-after: avoid;
    }
    h3 {
      font-size: 14px;
      color: #334155;
      margin-top: 18px;
      margin-bottom: 6px;
      page-break-after: avoid;
    }
    p, li {
      font-size: 13px;
      color: #334155;
    }
    .endpoint-card {
      border: 1px solid #cbd5e1;
      border-radius: 6px;
      margin-bottom: 20px;
      page-break-inside: avoid;
      overflow: hidden;
      background: #ffffff;
      box-shadow: 0 1px 3px rgba(0,0,0,0.05);
    }
    .endpoint-header {
      padding: 10px 14px;
      background: #f8fafc;
      border-bottom: 1px solid #e2e8f0;
      display: flex;
      align-items: center;
      gap: 10px;
    }
    .method-badge {
      font-weight: 700;
      font-size: 11px;
      padding: 3px 8px;
      border-radius: 4px;
      color: white;
      text-transform: uppercase;
    }
    .method-post { background-color: #16a34a; }
    .method-get { background-color: #2563eb; }
    .method-delete { background-color: #dc2626; }
    .endpoint-path {
      font-family: 'Consolas', 'Courier New', monospace;
      font-weight: 700;
      font-size: 13px;
      color: #0f172a;
    }
    .auth-badge {
      font-size: 11px;
      background: #f1f5f9;
      color: #475569;
      padding: 2px 8px;
      border-radius: 4px;
      border: 1px solid #e2e8f0;
      margin-left: auto;
    }
    .endpoint-body {
      padding: 12px 14px;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      margin: 12px 0;
      font-size: 12px;
      page-break-inside: avoid;
    }
    th, td {
      border: 1px solid #cbd5e1;
      padding: 8px 10px;
      text-align: left;
    }
    th {
      background-color: #f1f5f9;
      color: #1e293b;
      font-weight: 600;
    }
    pre {
      background: #0f172a;
      color: #f8fafc;
      padding: 12px 14px;
      border-radius: 5px;
      font-family: 'Consolas', 'Courier New', monospace;
      font-size: 11.5px;
      overflow-x: auto;
      margin: 8px 0;
      line-height: 1.45;
      page-break-inside: avoid;
    }
    code {
      font-family: 'Consolas', 'Courier New', monospace;
      font-size: 12px;
      background: #f1f5f9;
      color: #b91c1c;
      padding: 2px 5px;
      border-radius: 3px;
    }
    pre code {
      background: transparent;
      color: inherit;
      padding: 0;
    }
    .callout {
      padding: 12px 16px;
      border-radius: 6px;
      margin: 14px 0;
      font-size: 12.5px;
      page-break-inside: avoid;
    }
    .callout-info {
      background: #eff6ff;
      border-left: 4px solid #3b82f6;
      color: #1e40af;
    }
    .callout-warning {
      background: #fffbeb;
      border-left: 4px solid #f59e0b;
      color: #92400e;
    }
    .callout-success {
      background: #f0fdf4;
      border-left: 4px solid #22c55e;
      color: #166534;
    }
    .page-break {
      page-break-before: always;
    }
  </style>
</head>
<body>

  <!-- COVER PAGE -->
  <div class="cover-page">
    <div class="badge-flag"></div>
    <div class="cover-title">Kopdes Merah Putih</div>
    <div class="cover-subtitle">Buku Panduan Teknis & Dokumentasi Resmi Backend REST API</div>
    <p style="font-size: 14px; color: #475569; max-width: 600px; margin: 0 auto;">
      Panduan integrasi lengkap untuk Divisi Front-End, Mobile Developer, dan Administrator Sistem Koperasi Desa.
      Mencakup alur Pendaftaran Anggota, Pembayaran QRIS (Normal & Auto-Lock), Maker-Checker, Iuran Bulanan, dan Deployment VPS.
    </p>

    <div class="cover-meta">
      <strong>Penyusun:</strong> Tim Arsitektur & Keamanan Sistem Kopdes Merah Putih<br>
      <strong>Teknologi:</strong> Golang (Go 1.24), PostgreSQL 16 (Multi-Tenant RLS & Partitioning)<br>
      <strong>Standar Regulasi:</strong> PADG BI 21/18/PADG/2019 (QRIS) & UU No. 27 Tahun 2022 (PDP)<br>
      <strong>Status Dokumen:</strong> Versi 1.2 — Siap Produksi & Uji Coba Lapangan (Pilot Test)<br>
      <strong>Tanggal Pembaruan:</strong> 9 November 2026
    </div>
  </div>

  <!-- SECTION 1: ARSITEKTUR & PENDAHULUAN -->
  <h1>1. Pengenalan & Pondasi Arsitektur Sistem</h1>
  <p>
    <strong>Kopdes Merah Putih</strong> adalah platform perbankan desa dan koperasi konsumen terdistribusi
    yang dirancang dengan standar keamanan tingkat tinggi (fintech-grade). Sistem backend dibangun menggunakan
    bahasa <strong>Golang</strong> dengan mesin basis data <strong>PostgreSQL 16</strong>.
  </p>

  <div class="callout callout-info">
    <strong>5 Pilar Keamanan & Integritas Data Utama:</strong>
    <ol style="margin: 6px 0 0 16px; padding: 0;">
      <li><strong>Isolasi Multi-Tenant & RLS:</strong> Setiap query bisnis otomatis disaring oleh PostgreSQL <code>FORCE ROW LEVEL SECURITY</code> melalui session variable <code>app.current_koperasi_id</code> yang diekstrak murni dari token JWT (Mencegah kerentanan OWASP API1:2023 BOLA).</li>
      <li><strong>Maker-Checker Guard:</strong> Pembuat transaksi/pembayaran dilarang memverifikasi pembayarannya sendiri (Pencegahan fraud internal pengurus).</li>
      <li><strong>Anti-Float Financial:</strong> Seluruh perhitungan nilai uang menggunakan tipe data SQL <code>NUMERIC(18,2)</code> dan <code>int64</code> di Go untuk mencegah selisih pembulatan sen.</li>
      <li><strong>Append-Only Audit Journal:</strong> Tabel mutasi kas, log verifikasi, dan status dilarang keras di-<code>UPDATE</code> atau di-<code>DELETE</code>. Koreksi saldo wajib melalui entri reversal akuntansi.</li>
      <li><strong>Enkripsi Data Pribadi:</strong> NIK, Tanggal Lahir, dan Nomor HP dienkripsi menggunakan AES-256-GCM dengan Blind Index HKDF-SHA256 untuk fitur pencarian cepat tanpa membuka plaintext (rancangan proteksi PII, memerlukan audit hukum formal).</li>
    </ol>
  </div>

  <!-- SECTION 2: FLEKSIBILITAS PORT & DOCKER -->
  <h1>2. Fleksibilitas Konfigurasi Port & Lingkungan (.env)</h1>
  <p>
    Seluruh port server API maupun basis data dapat diubah secara bebas melalui file <code>.env</code>
    tanpa perlu melakukan kompilasi ulang pada kode program.
  </p>

  <table>
    <thead>
      <tr>
        <th>Variabel .env</th>
        <th>Nilai Default</th>
        <th>Fungsi & Penjelasan</th>
      </tr>
    </thead>
    <tbody>
      <tr>
        <td><code>API_PORT</code> / <code>API_HOST_PORT</code></td>
        <td><code>Dikonfigurasi di .env</code></td>
        <td>Port HTTP Server API & port publik host Docker (misal Set A vs Set B `18080`, dikonfigurasi via <code>.env</code>).</td>
      </tr>
      <tr>
        <td><code>DB_PORT</code></td>
        <td><code>5433</code> (dev) / <code>5432</code> (vps)</td>
        <td>Port PostgreSQL. Jika menggunakan docker-compose di VPS, gunakan 5432.</td>
      </tr>
      <tr>
        <td><code>DATABASE_URL</code></td>
        <td><code>postgres://...</code></td>
        <td>DSN lengkap koneksi database. Otomatis menyerap variabel host, user, dan port.</td>
      </tr>
      <tr>
        <td><code>JWT_SECRET</code></td>
        <td><em>32-byte Hex</em></td>
        <td>Kunci rahasia penandatanganan token otentikasi login pengguna.</td>
      </tr>
      <tr>
        <td><code>ALLOWED_ORIGINS</code></td>
        <td><code>http://localhost:3000</code></td>
        <td>Daftar domain Front-End web yang diizinkan melakukan request lintas-domain (CORS).</td>
      </tr>
    </tbody>
  </table>

  <h3>Cara Menjalankan Backend:</h3>
  <p><strong>A. Mode Pengembangan Lokal (Windows / Linux):</strong></p>
  <pre><code># 1. Pastikan PostgreSQL 16 menyala
# 2. Jalankan server API
go run ./cmd/server
# Server aktif pada port yang diset di API_PORT (misal http://0.0.0.0:${API_PORT})</code></pre>

  <p><strong>B. Mode Server / VPS Produksi (Docker Compose):</strong></p>
  <pre><code># Kloning repo, salin konfigurasi, dan nyalakan dalam 1 perintah
cp .env.example .env
docker compose up -d --build</code></pre>

  <!-- SECTION 3: DUA MODEL QRIS -->
  <div class="page-break"></div>
  <h1>3. Panduan Fitur Pembayaran QRIS: Dua Model Pilihan</h1>
  <p>
    Kopdes Merah Putih menyediakan dua model adopsi QRIS untuk menjembatani kebutuhan antara
    tahap awal desa yang praktis tanpa izin rumit, hingga tahap modern koperasi skala besar.
  </p>

  <div class="callout callout-success">
    <h3>MODEL 1 (Pilihan Saat Ini: QRIS Normal / Perorangan + Kode Unik Manual)</h3>
    <p>
      <strong>Mengapa Memakai Cara Ini?</strong> Anda tidak perlu menunggu pengurusan Akta Notaris Kemenkumham
      atau verifikasi rekening badan hukum bank yang memakan waktu berbulan-bulan. Anda bisa langsung memulai uji coba hari ini!
    </p>
    <ol>
      <li><strong>Pendaftaran:</strong> Pengurus mendaftarkan QRIS perorangan milik bendahara/ketua (misal dari GoPay Merchant Mikro, Dana Bisnis, Livin Mandiri, atau BCA) via menu Admin.</li>
      <li><strong>Pembuatan Tagihan:</strong> Anggota membuat tagihan (misal Iuran Rp 20.000). Sistem otomatis mengalokasikan 3-digit kode unik (misal 142) sehingga total transfer menjadi <strong>Rp 20.142</strong>.</li>
      <li><strong>Pembayaran:</strong> Anggota memindai stiker fisik QRIS bendahara di kantor desa lewat m-banking apa saja (BCA, Livin, BRImo, Dana), lalu mengetikkan nominal pas <strong>Rp 20.142</strong>.</li>
      <li><strong>Verifikasi:</strong> Bendahara melihat mutasi masuk Rp 20.142 di aplikasi HP miliknya, lalu mengklik tombol <em>"Verifikasi Pembayaran"</em> di sistem Kopdes.</li>
    </ol>
  </div>

  <div class="callout callout-info">
    <h3>MODEL 2 (Pilihan Otomatis: Dynamic QRIS Auto-Lock Nominal)</h3>
    <p>
      <strong>Bagaimana Cara Kerjanya?</strong> Algoritma backend di <code>internal/qris/emvco.go</code> secara otomatis
      mengubah payload QRIS menjadi QRIS Dinamis berstandar Bank Indonesia (PADG BI 21/18/PADG/2019):
    </p>
    <ul>
      <li>Tag <code>01</code> diubah menjadi <code>'12'</code> (Dynamic Point of Initiation).</li>
      <li>Tag <code>54</code> disuntikkan dengan total nominal pas (misal <code>20142</code>).</li>
      <li>Tag <code>62</code> disuntikkan dengan nomor invoice unik (misal <code>PAY-886f-0300</code>).</li>
      <li>Tag <code>63</code> dihitung ulang checksum CRC16-CCITT False.</li>
    </ul>
    <p>
      <strong>Hasilnya:</strong> Saat kode QR di layar HP atau web di-scan oleh anggota, <strong>layar nominal di aplikasi bank otomatis terkunci</strong> pada angka Rp 20.142. Anggota tidak bisa salah ketik nominal!
    </p>
  </div>

  <!-- SECTION 4: MODUL PENDAFTARAN ANGGOTA & DATA PRIBADI -->
  <h1>4. Modul Pendaftaran Anggota & Perlindungan Data Pribadi (UU PDP)</h1>
  <p>
    Sesuai mandat <strong>Undang-Undang No. 27 Tahun 2022 tentang Pelindungan Data Pribadi (UU PDP)</strong>,
    sistem Kopdes Merah Putih menerapkan enkripsi dua lapis untuk seluruh data identitas warga desa:
  </p>

  <table>
    <thead>
      <tr>
        <th>Data Sensitif</th>
        <th>Metode Penyimpanan</th>
        <th>Tujuan & Implementasi</th>
      </tr>
    </thead>
    <tbody>
      <tr>
        <td><strong>NIK (Nomor Induk Kependudukan)</strong></td>
        <td><code>nik_enkripsi</code> (AES-256-GCM)<br><code>nik_hash</code> (HMAC-SHA256)</td>
        <td>Plaintext NIK tidak pernah tersimpan di database. Kolom hash berfungsi sebagai Blind Index agar pencarian tetap instan via indeks B-Tree tanpa perlu mendekripsi seluruh baris data.</td>
      </tr>
      <tr>
        <td><strong>Nomor Handphone</strong></td>
        <td><code>no_hp_enkripsi</code> (AES-256-GCM)<br><code>no_hp_hash</code> (HMAC-SHA256)</td>
        <td>Melindungi nomor pribadi anggota dari kebocoran SMS/spam pemasaran ilegal.</td>
      </tr>
      <tr>
        <td><strong>Alamat & Tanggal Lahir</strong></td>
        <td><code>alamat_enkripsi</code>, <code>tanggal_lahir_enkripsi</code></td>
        <td>Dienkripsi penuh menggunakan AES-256-GCM dengan nonce acak per record.</td>
      </tr>
    </tbody>
  </table>

  <!-- SECTION 5: REFERENSI LENGKAP ENDPOINT API -->
  <div class="page-break"></div>
  <h1>5. Referensi Lengkap Endpoint API Backend</h1>
  <p>Semua respons API menggunakan amplop standar JSON: <code>{"success": true, "data": {...}}</code>.</p>

  <!-- 5.1 LOGIN -->
  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-post">POST</span>
      <span class="endpoint-path">/api/v1/auth/login</span>
      <span class="auth-badge">Publik</span>
    </div>
    <div class="endpoint-body">
      <p>Otentikasi pengguna menggunakan email dan password untuk mendapatkan Access Token JWT.</p>
      <strong>Request Body:</strong>
      <pre><code>{
  "email": "budi@kopdes.id",
  "password": "PasswordKuat123!"
}</code></pre>
      <strong>Response Sukses (200 OK):</strong>
      <pre><code>{
  "success": true,
  "data": {
    "access_token": "<JWT_ACCESS_TOKEN>",
    "token_type": "Bearer",
    "expires_in_seconds": 86400,
    "user": {
      "id": "0193108c-9a4f-7cd3-...",
      "nama_lengkap": "Budi Utomo",
      "email": "budi@kopdes.id",
      "koperasi_id": "0193108c-8f12-7000-...",
      "roles": ["anggota"]
    }
  }
}</code></pre>
    </div>
  </div>

  <!-- 5.2 BUAT QRIS -->
  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-post">POST</span>
      <span class="endpoint-path">/api/v1/pembayaran/qris</span>
      <span class="auth-badge">Bearer Token (Semua Role)</span>
    </div>
    <div class="endpoint-body">
      <p>Mengalokasikan pembayaran QRIS dengan 3-digit kode unik anti-tabrakan dan generator Auto-Lock Nominal.</p>
      <strong>Request Body:</strong>
      <pre><code>{
  "nominal_dasar": 50000,
  "jenis_transaksi": "iuran_bulanan",
  "anggota_id": "0193108c-bf53-7000-..."
}</code></pre>
      <strong>Response Sukses (201 Created):</strong>
      <pre><code>{
  "success": true,
  "data": {
    "pembayaran_id": "0193109a-4c12-7011-...",
    "no_pembayaran": "PAY-886f888a-0300",
    "kode_unik": 112,
    "nominal_dasar": 50000,
    "nominal_total": 50112,
    "qris_payload": "0002010102122666...5405501125802ID5913KOPDES MAKMUR6007BANDUNG62210117PAY-886f888a-03006304E8E9",
    "is_autolock_nominal": true,
    "nama_merchant": "KOPDES MAKMUR",
    "nmid": "ID1020304050607",
    "berlaku_sampai": "2026-11-09T16:00:00Z",
    "sisa_waktu_detik": 3600
  }
}</code></pre>
    </div>
  </div>

  <!-- 5.3 CEK STATUS PEMBAYARAN -->
  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-get">GET</span>
      <span class="endpoint-path">/api/v1/pembayaran/{id}</span>
      <span class="auth-badge">Bearer Token</span>
    </div>
    <div class="endpoint-body">
      <p>Memeriksa status terkini transaksi pembayaran (Polling front-end untuk mendeteksi konfirmasi pelunasan).</p>
      <strong>Response Sukses (200 OK):</strong>
      <pre><code>{
  "success": true,
  "data": {
    "id": "0193109a-4c12-7011-...",
    "status": "menunggu_pembayaran",
    "no_pembayaran": "PAY-886f888a-0300",
    "nominal_total": "50112.00",
    "berlaku_sampai": "2026-11-09T16:00:00Z",
    "referensi_rrn": null
  }
}</code></pre>
    </div>
  </div>

  <!-- 5.4 DAFTAR REKENING QRIS ADMIN -->
  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-post">POST</span>
      <span class="endpoint-path">/api/v1/koperasi/rekening-qris</span>
      <span class="auth-badge">Role: Admin / Pengurus</span>
    </div>
    <div class="endpoint-body">
      <p>Mendaftarkan stiker / data QRIS milik koperasi (baik QRIS perorangan sementara maupun QRIS merchant resmi).</p>
      <strong>Request Body:</strong>
      <pre><code>{
  "nama_merchant": "KOPDES MERAH PUTIH MANDIRI",
  "nmid": "ID1020304050607",
  "nama_bank_pjp": "Bank Mandiri",
  "no_rekening_settlement": "1300099887766",
  "nama_pemilik_rekening": "Koperasi Merah Putih Mandiri",
  "is_atas_nama_badan_hukum": true,
  "qris_payload_statis": "0002010102112666...6304ABCD"
}</code></pre>
      <strong>Response Sukses (201 Created):</strong>
      <pre><code>{
  "success": true,
  "data": {
    "id": "019310ba-1100-7fa3-...",
    "nama_merchant": "KOPDES MERAH PUTIH MANDIRI",
    "nmid": "ID1020304050607",
    "status_aktif": true,
    "pesan": "Rekening QRIS statis badan hukum koperasi berhasil didaftarkan."
  }
}</code></pre>
    </div>
  </div>

  <!-- 5.5 VERIFIKASI PEMBAYARAN MAKER-CHECKER -->
  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-post">POST</span>
      <span class="endpoint-path">/api/v1/pembayaran/{id}/verifikasi</span>
      <span class="auth-badge">Role: Admin / Pengurus / Operator</span>
    </div>
    <div class="endpoint-body">
      <p>
        Menyetujui atau menolak bukti pembayaran anggota.
        <strong style="color: #b91c1c;">Maker-Checker: Ditolak otomatis (HTTP 403) jika pembuat pembayaran mencoba memverifikasi transaksinya sendiri.</strong>
      </p>
      <strong>Request Body:</strong>
      <pre><code>{
  "disetujui": true,
  "referensi_rrn": "123456789012",
  "nominal_aktual": 50112,
  "catatan": "Uang masuk di mutasi Bank Mandiri pukul 14:15"
}</code></pre>
      <strong>Response Sukses (200 OK):</strong>
      <pre><code>{
  "success": true,
  "data": {
    "pembayaran_id": "0193109a-4c12-7011-...",
    "status": "berhasil_diverifikasi",
    "pesan": "Verifikasi pembayaran berhasil dicatat ke jurnal append-only."
  }
}</code></pre>
    </div>
  </div>

  <!-- 5.6 GENERATE IURAN -->
  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-post">POST</span>
      <span class="endpoint-path">/api/v1/iuran/generate</span>
      <span class="auth-badge">Role: Admin / Pengurus</span>
    </div>
    <div class="endpoint-body">
      <p>Menjalankan proses pembuatan tagihan iuran bulanan untuk seluruh anggota aktif secara idempoten.</p>
      <strong>Request Body:</strong>
      <pre><code>{
  "jenis_iuran_id": "0193108d-5122-7bc1-...",
  "periode": "2026-11"
}</code></pre>
    </div>
  </div>

  <!-- 5.7 MONITORING MENUNGGAK -->
  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-get">GET</span>
      <span class="endpoint-path">/api/v1/iuran/menunggak?min_bulan=2</span>
      <span class="auth-badge">Role: Admin / Pengurus</span>
    </div>
    <div class="endpoint-body">
      <p>Mengambil daftar anggota yang menunggak pembayaran iuran lebih dari N bulan untuk keperluan peringatan pengurus desa.</p>
    </div>
  </div>

  <!-- 5.8 KATALOG STOK & HARGA -->
  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-get">GET</span>
      <span class="endpoint-path">/api/v1/katalog/stok</span>
      <span class="auth-badge">Semua User</span>
    </div>
    <div class="endpoint-body">
      <p>Menampilkan stok saldo gudang dan sisa kuota barang bebas jual di toko koperasi desa.</p>
    </div>
  </div>

  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-get">GET</span>
      <span class="endpoint-path">/api/v1/katalog/harga?produk_id={id}&anggota_id={id}</span>
      <span class="auth-badge">Semua User</span>
    </div>
    <div class="endpoint-body">
      <p>Menghitung harga barang secara dinamis dengan memperhitungkan tier diskon khusus anggota koperasi.</p>
    </div>
  </div>

  <!-- 5.9 PENDAFTARAN ANGGOTA & KYC (P1-1) -->
  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-post">POST</span>
      <span class="endpoint-path">/api/v1/pendaftaran</span>
      <span class="auth-badge">Calon Anggota</span>
    </div>
    <div class="endpoint-body">
      <p>Mengajukan formulir pendaftaran anggota baru. Data sensitif (NIK, Tanggal Lahir, Alamat) otomatis dienkripsi dengan AES-256-GCM + AAD, nomor registrasi dibuat secara atomik (REG/YYYY/XXXXX), dan duplikasi NIK dicegah via Blind Index HKDF-SHA256.</p>
      <strong>Request Body:</strong>
      <pre><code>{
  "nik": "3201123456780001",
  "nama_lengkap": "Budi Santoso",
  "tempat_lahir": "Bogor",
  "tanggal_lahir": "1990-05-15",
  "jenis_kelamin": "L",
  "alamat_ktp": "Jl. Raya Desa No. 10",
  "pekerjaan": "Petani"
}</code></pre>
      <strong>Response Sukses (201 Created):</strong>
      <pre><code>{
  "success": true,
  "data": {
    "pendaftaran_id": "0193109b-1234-7000-...",
    "no_registrasi": "REG/2026/00001",
    "status": "diajukan"
  }
}</code></pre>
    </div>
  </div>

  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-get">GET</span>
      <span class="endpoint-path">/api/v1/pendaftaran/{id}</span>
      <span class="auth-badge">Pemohon / Admin</span>
    </div>
    <div class="endpoint-body">
      <p>Memeriksa status pendaftaran calon anggota. Dilindungi anti-IDOR (pemohon lain mendapat 404). NIK ditampilkan dalam bentuk tersamar (masking) otomatis <code>3201********0001</code>.</p>
    </div>
  </div>

  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-post">POST</span>
      <span class="endpoint-path">/api/v1/pendaftaran/{id}/dokumen</span>
      <span class="auth-badge">Pemohon (Multipart)</span>
    </div>
    <div class="endpoint-body">
      <p>Mengunggah foto KTP / dokumen KYC calon anggota. Sistem memvalidasi Magic Bytes file asli (hanya menerima JPEG, PNG, PDF asli), menolak script/eksekutabel, membatasi ukuran maksimal 5MB, dan mencatat checksum integritas SHA-256.</p>
    </div>
  </div>

  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-post">POST</span>
      <span class="endpoint-path">/api/v1/pendaftaran/{id}/verifikasi</span>
      <span class="auth-badge">Role: Admin / Pengurus (Maker-Checker)</span>
    </div>
    <div class="endpoint-body">
      <p>Memproses verifikasi penerimaan calon anggota. Dilindungi kontrol Maker-Checker (pemohon dilarang menyetujui pendaftarannya sendiri). Saat disetujui, nomor anggota unik <code>ANG/2026/XXXXX</code> diterbitkan secara atomik dan akun pengguna langsung dipromosikan ke role <code>anggota</code>.</p>
      <strong>Request Body:</strong>
      <pre><code>{
  "status": "disetujui",
  "catatan_perbaikan": "Dokumen KTP valid dan terverifikasi"
}</code></pre>
    </div>
  </div>

  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-post">POST</span>
      <span class="endpoint-path">/api/v1/pendaftaran/purge-ditolak</span>
      <span class="auth-badge">Role: Admin / Pengurus (UU PDP)</span>
    </div>
    <div class="endpoint-body">
      <p>Menjalankan prosedur pembersihan berkas fisik dan enkripsi PII bagi formulir pendaftaran yang ditolak lebih dari masa retensi terkonfigurasi (default 30 hari; catatan: perlu verifikasi hukum formal).</p>
    </div>
  </div>

  <!-- 5.10 KAS MUTASI & PROMETHEUS -->
  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-get">GET</span>
      <span class="endpoint-path">/api/v1/kas/mutasi</span>
      <span class="auth-badge">Role: Admin / Pengawas</span>
    </div>
    <div class="endpoint-body">
      <p>Membaca catatan buku mutasi kas masuk/keluar terpartisi bulanan berantai hash SHA-256 append-only (dilindungi trigger anti-TRUNCATE).</p>
    </div>
  </div>

  <div class="endpoint-card">
    <div class="endpoint-header">
      <span class="method-badge method-get">GET</span>
      <span class="endpoint-path">/metrics</span>
      <span class="auth-badge">Bearer METRICS_TOKEN (&gt;= 32 byte)</span>
    </div>
    <div class="endpoint-body">
      <p>Eksposisi metrik operasional Prometheus (uptime, antrean verifikasi pembayaran, slot kode unik QRIS aktif, total job gagal, dan status partisi database). Wajib menyertakan <code>Authorization: Bearer &lt;METRICS_TOKEN&gt;</code>; akses tanpa token ditolak <code>401</code>.</p>
    </div>
  </div>

  <!-- SECTION 6: PANDUAN FRONT-END -->
  <div class="page-break"></div>
  <h1>6. Panduan Praktis Integrasi Front-End (Web & Mobile)</h1>
  <p>
    Divisi Front-End memiliki kebebasan penuh dalam merancang antarmuka UI/UX.
    Berikut adalah modul siap pakai untuk menghubungkan aplikasi ke backend Go.
  </p>

  <h3>A. Modul Klien API (TypeScript / Fetch)</h3>
  <pre><code>// lib/apiClient.ts
const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "";
if (!BASE_URL) {
  throw new Error("NEXT_PUBLIC_API_URL wajib diisi di .env.local");
}

export async function kopdesAPI&lt;T&gt;(endpoint: string, options: RequestInit = {}): Promise&lt;T&gt; {
  const token = typeof window !== "undefined" ? localStorage.getItem("kopdes_token") : null;
  const headers = new Headers(options.headers || {});
  headers.set("Content-Type", "application/json");
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const res = await fetch(`${BASE_URL}${endpoint}`, { ...options, headers });
  const json = await res.json();

  if (!res.ok || !json.success) {
    throw new Error(json.error?.message || "Terjadi kesalahan pada server");
  }
  return json.data;
}</code></pre>

  <h3>B. Komponen Pembayaran QRIS (React / Next.js)</h3>
  <pre><code>import React, { useState, useEffect } from "react";
import QRCode from "qrcode.react"; // npm install qrcode.react
import { kopdesAPI } from "../lib/apiClient";

export function PembayaranQRIS({ tagihanIds }: { tagihanIds: string[] }) {
  const [data, setData] = useState&lt;any&gt;(null);
  const [lunas, setLunas] = useState(false);

  useEffect(() =&gt; {
    // 1. Buat alokasi QRIS dengan Idempotency-Key
    kopdesAPI("/api/v1/pembayaran/qris", {
      method: "POST",
      headers: { "Idempotency-Key": crypto.randomUUID() },
      body: JSON.stringify({ jenis_transaksi: "iuran_bulanan", tagihan_iuran_id: tagihanIds })
    }).then(res =&gt; setData(res));
  }, [tagihanIds]);

  // 2. Polling status pelunasan setiap 10 detik
  useEffect(() =&gt; {
    if (!data || lunas) return;
    const interval = setInterval(async () =&gt; {
      try {
        const res: any = await kopdesAPI(`/api/v1/pembayaran/${data.pembayaran_id}`);
        if (res.status === "disetujui") {
          setLunas(true);
          clearInterval(interval);
        }
      } catch (_) {}
    }, 10000);
    return () =&gt; clearInterval(interval);
  }, [data, lunas]);

  if (!data) return &lt;p&gt;Memuat QRIS...&lt;/p&gt;;
  if (lunas) return &lt;h2 style={{color: "green"}}&gt;Pembayaran Berhasil Dikonfirmasi!&lt;/h2&gt;;

  return (
    &lt;div style={{ textAlign: "center", padding: "20px", border: "1px solid #ddd", borderRadius: "12px" }}&gt;
      &lt;h3&gt;{data.nama_merchant}&lt;/h3&gt;
      &lt;QRCode value={data.qris_payload} size={240} level="M" /&gt;
      &lt;h1 style={{ color: "#dc2626" }}&gt;Rp {data.nominal_total.toLocaleString("id-ID")}&lt;/h1&gt;
      &lt;p&gt;Kode Unik Transaksi: &lt;b&gt;{data.kode_unik}&lt;/b&gt;&lt;/p&gt;
      &lt;small style={{ color: "#666" }}&gt;Nominal otomatis terkunci saat discan via m-banking&lt;/small&gt;
    &lt;/div&gt;
  );
}</code></pre>

  <div class="callout callout-info">
    <strong>Frontend Tester Polos &amp; Kontrak Lengkap (54 Operasi / 48 Path):</strong>
    Gunakan <code>cmd/uitester</code> (<code>UI_PORT</code> &amp; <code>API_PUBLIC_URL</code> dari <code>.env.test</code>) bersama <code>frontend-tester/</code> untuk menguji OpenAPI Explorer, alur terpandu 3.1–3.9, 26 skenario <code>scenarios.json</code>, serta matriks RBAC <code>roles_matrix.json</code> langsung di browser tanpa framework maupun penyimpanan token persisten. Konfigurasi total berjumlah 83 variabel di <code>docs/ENV_REFERENCE.md</code>.
  </div>

  <!-- SECTION 7: FAQ -->
  <h1>7. Pertanyaan Umum & Troubleshooting (FAQ)</h1>
  <div class="callout callout-info">
    <strong>T: Mengapa login saya mendapatkan error 401 saat user baru dibuat?</strong><br>
    J: Pastikan password yang dikirimkan cocok dengan password saat seeding, dan pastikan user tersebut telah dikaitkan dengan role di tabel <code>user_roles</code> untuk koperasi yang bersangkutan.
  </div>

  <div class="callout callout-info">
    <strong>T: Apakah anggota wajib transfer menggunakan nominal pas beserta kode unik?</strong><br>
    J: Ya, wajib. Jika anggota mentransfer tanpa kode unik (misal pas Rp 20.000 bukan Rp 20.142), sistem akan mendeteksi selisih bayar (<code>selisih_bayar <> 0</code>) dan admin harus melakukan penyesuaian manual di antrean verifikasi.
  </div>

  <div class="callout callout-info">
    <strong>T: Bagaimana jika server PostgreSQL dipindahkan ke server lain?</strong><br>
    J: Anda cukup mengedit file <code>.env</code> pada variabel <code>DB_HOST</code>, <code>DB_PORT</code>, <code>DB_USER</code>, dan <code>DB_PASSWORD</code>. Backend Go otomatis membaca konfigurasi baru tanpa kompilasi ulang.
  </div>

</body>
</html>
"""

# 1. Simpan file HTML
with open(HTML_FILE, "w", encoding="utf-8") as f:
    f.write(html_content)
print(f"[OK] File HTML berhasil disimpan: {HTML_FILE}")

# 2. Render ke PDF via Microsoft Edge Headless
edge_paths = [
    r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
    r"C:\Program Files\Microsoft\Edge\Application\msedge.exe"
]
edge_bin = next((p for p in edge_paths if os.path.exists(p)), None)

if edge_bin:
    cmd = [
        edge_bin,
        "--headless=new",
        "--disable-gpu",
        "--run-all-compositor-stages-before-draw",
        "--no-pdf-header-footer",
        f"--print-to-pdf={PDF_FILE}",
        HTML_FILE
    ]
    try:
        res = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        if os.path.exists(PDF_FILE) and os.path.getsize(PDF_FILE) > 1000:
            print(f"[OK] File PDF berhasil dibuat ({os.path.getsize(PDF_FILE):,} bytes): {PDF_FILE}")
        else:
            print(f"[WARN] Pembuatan PDF via Edge menghasilkan file kecil atau gagal: {res.stderr}")
    except Exception as e:
        print(f"[ERR] Gagal menjalankan Edge headless: {e}")
else:
    print("[WARN] Microsoft Edge tidak ditemukan untuk kompilasi PDF.")

# 3. Buat File Microsoft Word (.docx) Valid Menggunakan Standard Library zipfile & OpenXML
def create_docx(filename):
    content_types = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
</Types>"""

    rels = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>"""

    doc_rels = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>"""

    styles = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:docDefaults>
    <w:rPrDefault>
      <w:rPr>
        <w:rFonts w:ascii="Calibri" w:hAnsi="Calibri"/>
        <w:sz w:val="22"/>
        <w:color w:val="1E293B"/>
      </w:rPr>
    </w:rPrDefault>
  </w:docDefaults>
  <w:style w:type="paragraph" w:styleId="Heading1">
    <w:name w:val="heading 1"/>
    <w:pPr><w:spacing w:before="360" w:after="120"/></w:pPr>
    <w:rPr><w:b/><w:sz w:val="32"/><w:color w:val="B91C1C"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading2">
    <w:name w:val="heading 2"/>
    <w:pPr><w:spacing w:before="240" w:after="80"/></w:pPr>
    <w:rPr><w:b/><w:sz w:val="26"/><w:color w:val="0F172A"/></w:rPr>
  </w:style>
</w:styles>"""

    document_xml = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:pPr><w:jc w:val="center"/><w:spacing w:before="720" w:after="240"/></w:pPr>
      <w:r><w:rPr><w:b/><w:sz w:val="44"/><w:color w:val="B91C1C"/></w:rPr><w:t>KOPDES MERAH PUTIH</w:t></w:r>
    </w:p>
    <w:p>
      <w:pPr><w:jc w:val="center"/><w:spacing w:after="480"/></w:pPr>
      <w:r><w:rPr><w:b/><w:sz w:val="28"/><w:color w:val="0F172A"/></w:rPr><w:t>DOKUMENTASI LENGKAP PENGGUNAAN API &amp; BACKEND</w:t></w:r>
    </w:p>
    <w:p>
      <w:pPr><w:jc w:val="center"/><w:spacing w:after="720"/></w:pPr>
      <w:r><w:rPr><w:sz w:val="22"/><w:color w:val="64748B"/></w:rPr><w:t>Panduan Arsitektur, Endpoint API, Pembayaran QRIS, Registrasi Anggota, dan Deployment</w:t></w:r>
    </w:p>

    <!-- SECTION 1 -->
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>1. Pengenalan &amp; Arsitektur Inti</w:t></w:r></w:p>
    <w:p><w:r><w:t>Kopdes Merah Putih dirancang dengan arsitektur Multi-Tenant murni menggunakan PostgreSQL 16 Force Row Level Security (RLS). Setiap query bisnis diisolasi secara mutlak di tingkat database menggunakan session variable app.current_koperasi_id yang diekstrak dari JWT token.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>Prinsip Keamanan Inti:</w:t></w:r></w:p>
    <w:p><w:r><w:t>• Maker-Checker: Pembuat pembayaran dilarang memverifikasi pembayarannya sendiri.</w:t></w:r></w:p>
    <w:p><w:r><w:t>• Anti-Float: Seluruh angka keuangan menggunakan NUMERIC(18,2) di SQL dan int64 di Go.</w:t></w:r></w:p>
    <w:p><w:r><w:t>• Append-Only: Mutasi kas dan log verifikasi dilarang UPDATE dan DELETE.</w:t></w:r></w:p>
    <w:p><w:r><w:t>• UU PDP No. 27/2022: NIK dan nomor HP dienkripsi menggunakan AES-256-GCM dan HMAC-SHA256.</w:t></w:r></w:p>

    <!-- SECTION 2 -->
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>2. Fitur Pembayaran QRIS: Normal vs Auto-Lock</w:t></w:r></w:p>
    <w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Model 1: QRIS Normal / Perorangan + Kode Unik Manual (Rekomendasi Tahap Awal)</w:t></w:r></w:p>
    <w:p><w:r><w:t>Pengurus koperasi dapat menggunakan akun QRIS perorangan milik bendahara/ketua yang sudah ada (GoPay, Dana, OVO, BCA, Mandiri). Anggota membuat pembayaran iuran (misal Rp 20.000), backend memberikan 3-digit kode unik (misal 142) sehingga total menjadi Rp 20.142. Anggota memindai stiker fisik QRIS dan mengetikkan Rp 20.142. Bendahara memverifikasi mutasi masuk di aplikasi bank dan mengklik verifikasi di dashboard.</w:t></w:r></w:p>

    <w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Model 2: Dynamic QRIS Auto-Lock Nominal</w:t></w:r></w:p>
    <w:p><w:r><w:t>Backend secara otomatis mengonversi string EMVCo statis menjadi dinamis berstandar Bank Indonesia dengan mengubah Tag 01 menjadi '12', menyuntikkan Tag 54 dengan nominal pas, Tag 62 nomor invoice, dan menghitung ulang CRC16 (Tag 63). Saat discan via m-banking anggota, nominal transfer otomatis terkunci tanpa bisa diubah.</w:t></w:r></w:p>

    <!-- SECTION 3 -->
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>3. Referensi Lengkap Endpoint API</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>1. POST /api/v1/auth/login</w:t></w:r><w:r><w:t> - Login pengguna menggunakan email &amp; password, mengembalikan JWT Bearer Token.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>2. GET /api/v1/auth/me</w:t></w:r><w:r><w:t> - Mengambil profil pengguna terotentikasi.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>3. POST /api/v1/pembayaran/qris</w:t></w:r><w:r><w:t> - Membuat alokasi QRIS 3-digit kode unik &amp; payload auto-lock.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>4. GET /api/v1/pembayaran/{id}</w:t></w:r><w:r><w:t> - Memeriksa status transaksi pembayaran secara real-time.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>5. POST /api/v1/koperasi/rekening-qris</w:t></w:r><w:r><w:t> - (Admin) Mendaftarkan QRIS statis / perorangan koperasi.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>6. GET /api/v1/koperasi/rekening-qris</w:t></w:r><w:r><w:t> - (Admin) Menampilkan daftar rekening QRIS aktif.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>7. DELETE /api/v1/koperasi/rekening-qris/{id}</w:t></w:r><w:r><w:t> - (Admin) Menonaktifkan rekening QRIS.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>8. GET /api/v1/pembayaran/antrean</w:t></w:r><w:r><w:t> - (Admin) Menampilkan antrean klaim pembayaran yang menunggu verifikasi.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>9. POST /api/v1/pembayaran/{id}/verifikasi</w:t></w:r><w:r><w:t> - (Admin) Verifikasi bukti pembayaran dengan proteksi Maker-Checker.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>10. POST /api/v1/iuran/generate</w:t></w:r><w:r><w:t> - (Admin) Generator idempoten tagihan iuran bulanan seluruh anggota.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>11. GET /api/v1/iuran/menunggak</w:t></w:r><w:r><w:t> - (Admin) Rekapitulasi daftar penunggak iuran bulanan.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>12. GET /api/v1/katalog/stok</w:t></w:r><w:r><w:t> - Melihat ketersediaan stok fisik gudang toko desa.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>13. GET /api/v1/katalog/harga</w:t></w:r><w:r><w:t> - Kalkulasi harga akhir dengan diskon anggota aktif.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>14. POST /api/v1/pendaftaran</w:t></w:r><w:r><w:t> - Mengajukan formulir pendaftaran anggota baru (Enkripsi PII AES-256-GCM + AAD, Blind Index NIK HKDF).</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>15. GET /api/v1/pendaftaran/{id}</w:t></w:r><w:r><w:t> - Memeriksa status pendaftaran calon anggota dengan NIK tersamar (3201********0001).</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>16. POST /api/v1/pendaftaran/{id}/dokumen</w:t></w:r><w:r><w:t> - Mengunggah foto KTP KYC dengan validasi magic-bytes (hanya JPEG/PNG/PDF asli, max 5MB).</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>17. POST /api/v1/pendaftaran/{id}/verifikasi</w:t></w:r><w:r><w:t> - (Admin) Verifikasi penerimaan anggota dengan kontrol Maker-Checker &amp; nomor anggota atomik ANG/2026/XXXXX.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>18. POST /api/v1/pendaftaran/purge-ditolak</w:t></w:r><w:r><w:t> - (Admin) Pembersihan otomatis data pendaftaran ditolak &gt; 30 hari (Perlu verifikasi hukum).</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>19. GET /api/v1/kas/mutasi</w:t></w:r><w:r><w:t> - Membaca buku mutasi kas ringan berantai hash SHA-256 append-only.</w:t></w:r></w:p>
    <w:p><w:r><w:b/><w:t>20. GET /metrics</w:t></w:r><w:r><w:t> - Metrik monitoring Prometheus sistem real-time.</w:t></w:r></w:p>

    <!-- SECTION 4 -->
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>4. Panduan Deployment Server / VPS (Docker Compose)</w:t></w:r></w:p>
    <w:p><w:r><w:t>Sistem telah dilengkapi dengan Dockerfile multi-stage build dan docker-compose.yml yang secara otomatis menyalakan PostgreSQL 16 Alpine, mengeksekusi migrasi skema tabel, dan mengaktifkan API backend Go dengan satu perintah:</w:t></w:r></w:p>
    <w:p><w:r><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas"/><w:color w:val="B91C1C"/></w:rPr><w:t>docker compose up -d --build</w:t></w:r></w:p>

    <w:sectPr/>
  </w:body>
</w:document>"""

    with zipfile.ZipFile(filename, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("[Content_Types].xml", content_types)
        z.writestr("_rels/.rels", rels)
        z.writestr("word/_rels/document.xml.rels", doc_rels)
        z.writestr("word/styles.xml", styles)
        z.writestr("word/document.xml", document_xml)

    print(f"[OK] File DOCX (Microsoft Word) berhasil dibuat: {filename}")

create_docx(DOCX_FILE)
