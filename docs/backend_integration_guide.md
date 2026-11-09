# Panduan Integrasi Backend & Dokumentasi API — Kopdes Merah Putih

Dokumen ini ditujukan untuk **Divisi Front-End**, **Mobile**, dan **Backend Engineer** sebagai referensi lengkap arsitektur, daftar file, fungsi, sistem keamanan, dan panduan integrasi HTTP API.

---

## 1. Struktur File & Modul Backend

```text
kopdes-merah-putih/
├── cmd/
│   └── server/
│       └── main.go                    # Entry point aplikasi HTTP API server & graceful shutdown
├── internal/
│   ├── api/
│   │   ├── handlers/                  # Controller / HTTP Request Handlers
│   │   │   ├── auth_handler.go        # Login, profil user, token issuance
│   │   │   ├── qris_handler.go        # Pembuatan QRIS, cek status, antrean & verifikasi
│   │   │   ├── iuran_handler.go       # Generator tagihan bulanan & daftar penunggak
│   │   │   ├── katalog_handler.go     # Stok gudang & kalkulasi harga diskon anggota
│   │   │   └── health_handler.go      # Health check (/healthz & /readyz)
│   │   ├── middleware/                # Pipeline Keamanan HTTP
│   │   │   ├── auth.go                # Validasi JWT Bearer token & RBAC role guard
│   │   │   ├── tenant.go              # Inject app.current_koperasi_id ke pgx session (RLS)
│   │   │   ├── cors.go                # Cross-Origin Resource Sharing handler
│   │   │   ├── ratelimit.go           # Token-bucket IP rate limiter (anti-DDoS/brute force)
│   │   │   ├── security_headers.go    # OWASP Recommended Security Headers
│   │   │   └── recovery.go            # Panic recovery & graceful JSON 500
│   │   ├── response/
│   │   │   └── response.go            # Standarisasi JSON envelope {success, data, error, meta}
│   │   ├── api_test.go                # Integration tests untuk semua endpoint & middleware
│   │   └── server.go                  # Routing mux, konfigurasi server, & middleware pipeline
│   ├── auth/
│   │   └── jwt.go                     # Generator & verifikator token JWT (HMAC-SHA256)
│   ├── security/
│   │   └── crypto.go                  # AES-256-GCM (PDP), HMAC Blind Index, bcrypt hashing
│   └── db/                            # Hasil kompilasi sqlc (Data Access Layer)
│       ├── db.go                      # Inisialisasi Queries struct
│       ├── models.go                  # Tipe data Go untuk setiap tabel PostgreSQL
│       └── kopdes.sql.go              # Fungsi query tipe-aman yang digenerate sqlc
├── db/
│   ├── migrations/
│   │   └── 000001_init_kopdes_schema.up.sql  # 1.700 baris DDL PostgreSQL 16 (RLS, PDP, Partisi)
│   └── queries/
│       └── kopdes.sql                 # Koleksi query sqlc
├── docs/
│   ├── erd.md                         # Diagram ERD relasi data Mermaid
│   └── backend_integration_guide.md   # Dokumen panduan ini
├── service.go                         # Logika bisnis inti (Alokasi QRIS & Generator Tagihan)
├── service_test.go                    # Unit test konkurensi database & RLS
├── AGENTS.md                          # Aturan workspace agent
├── GEMINI.md                          # Aturan workspace Gemini
├── go.mod                             # Modul Go & dependensi
└── sqlc.yaml                          # Konfigurasi kompilasi sqlc
```

---

## 2. Fitur Keamanan (Security Hardening)

Aplikasi Kopdes Merah Putih menerapkan prinsip **Defense-in-Depth**:

| Fitur Keamanan | Implementasi | Tujuan Perlindungan |
| :--- | :--- | :--- |
| **Multi-Tenant RLS** | `middleware.WithTenantTx` & PostgreSQL `FORCE ROW LEVEL SECURITY` | Mencegah kebocoran data antar koperasi. Setiap query terisolasi mutlak di tingkat database engine. |
| **Maker-Checker** | `chk_pembayaran_maker_checker` & `HandleVerifikasiPembayaran` | Pembuat pembayaran (`pembayar_user_id`) dilarang memverifikasi pembayarannya sendiri (pencegahan fraud). |
| **Anti-Float Financial** | SQL `NUMERIC(18,2)` & Go `int64` / `string` | Mencegah selisih pembulatan uang akibat floating point error di JavaScript/DB. |
| **Tabel Append-Only** | PostgreSQL Trigger `trg_fn_append_only()` | Tabel audit dan kas dilarang `UPDATE` dan `DELETE`. Koreksi wajib via jurnal reversal. |
| **Perlindungan Data Pribadi** | AES-256-GCM + HMAC-SHA256 Blind Index | Sesuai UU 27/2022 PDP, data NIK/HP tidak pernah disimpan dalam bentuk plaintext. |
| **Rate Limiter** | `middleware.IPRateLimiter` (Token Bucket) | Menangkal serangan brute-force login dan denial of service (DoS). |
| **OWASP Security Headers** | `middleware.SecurityHeaders` | Anti-Clickjacking (`X-Frame-Options: DENY`), Anti-MIME Sniffing, dan CSP. |

---

## 3. Format Respons API Standar

Semua respons backend dikemas dalam amplop JSON standar:

### Respons Sukses (`200 OK`, `201 Created`):
```json
{
  "success": true,
  "data": { ... },
  "meta": { "total": 10 }
}
```

### Respons Gagal (`400`, `401`, `403`, `404`, `409`, `429`, `500`):
```json
{
  "success": false,
  "error": {
    "code": "BAD_REQUEST",
    "message": "Deskripsi kesalahan yang manusiawi",
    "details": null
  }
}
```

---

## 4. Kamus REST API Endpoints

### A. Autentikasi & Akun
#### 1. `POST /api/v1/auth/login`
- **Akses**: Publik
- **Deskripsi**: Menukarkan email dan password dengan token JWT 24 jam.
- **Request Body**:
  ```json
  {
    "email": "budi@kopdes.id",
    "password": "PasswordRahasia123!"
  }
  ```
- **Response `200 OK`**:
  ```json
  {
    "success": true,
    "data": {
      "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
      "token_type": "Bearer",
      "expires_in": 86400,
      "user_id": "0193108c-9824-7f89-8d8a-6f3b0c511210",
      "koperasi_id": "0193108a-2111-7a32-9bca-4b1190223344",
      "nama_lengkap": "Budi Santoso",
      "role": "anggota"
    }
  }
  ```

#### 2. `GET /api/v1/auth/me`
- **Akses**: Wajib Bearer Token
- **Deskripsi**: Mengambil profil pengguna yang sedang aktif.

---

### B. Pembayaran QRIS Statis
#### 1. `POST /api/v1/pembayaran/qris`
- **Akses**: Anggota / Pengguna terautentikasi
- **Deskripsi**: Menghasilkan tagihan QRIS dengan kode unik 3-digit (1..999) anti-tabrakan.
- **Request Body**:
  ```json
  {
    "nominal_dasar": 50000,
    "jenis_transaksi": "iuran_bulanan",
    "anggota_id": "0193108c-9824-7f89-8d8a-6f3b0c511210"
  }
  ```
- **Response `201 Created`**:
  ```json
  {
    "success": true,
    "data": {
      "pembayaran_id": "0193109a-4c12-7011-9a11-882233445566",
      "no_pembayaran": "PAY-a1b2c3d4-1234",
      "kode_unik": 142,
      "nominal_dasar": 50000,
      "nominal_total": 50142,
      "qris_payload": "00020101021126570011ID.CO.QRIS.WWW...",
      "nama_merchant": "Koperasi Merah Putih Sukamaju",
      "nmid": "ID1020304050607",
      "berlaku_sampai": "2026-11-09T15:24:16Z",
      "sisa_waktu_detik": 3600
    }
  }
  ```

#### 2. `GET /api/v1/pembayaran/{id}`
- **Akses**: Anggota / Operator
- **Deskripsi**: Polling status pembayaran dan countdown timer.
- **Response `200 OK`**:
  ```json
  {
    "success": true,
    "data": {
      "pembayaran_id": "0193109a-4c12-7011-...",
      "no_pembayaran": "PAY-a1b2c3d4-1234",
      "status": "menunggu_pembayaran",
      "nominal_total": "50142.00",
      "berlaku_sampai": "2026-11-09T15:24:16Z",
      "sisa_waktu_detik": 2940,
      "referensi_rrn": null
    }
  }
  ```

#### 3. `GET /api/v1/pembayaran/antrean`
- **Akses**: Khusus Role: `admin`, `pengurus`, `operator`
- **Deskripsi**: Daftar antrean pembayaran yang menunggu verifikasi admin toko/koperasi.

#### 4. `POST /api/v1/pembayaran/{id}/verifikasi`
- **Akses**: Khusus Role: `admin`, `pengurus`, `operator`
- **Aturan**: Maker-Checker enforced. Verifikator dilarang sama dengan pembuat transaksi.
- **Request Body**:
  ```json
  {
    "disetujui": true,
    "referensi_rrn": "987654321012",
    "catatan": "Mutasi bank BRI telah cocok."
  }
  ```
- **Response `200 OK`**:
  ```json
  {
    "success": true,
    "data": {
      "pembayaran_id": "0193109a-4c12-7011-...",
      "status": "berhasil_diverifikasi",
      "pesan": "Verifikasi pembayaran berhasil dicatat ke jurnal append-only."
    }
  }
  ```

---

### C. Iuran & Simpanan Anggota
#### 1. `POST /api/v1/iuran/generate`
- **Akses**: Khusus Role: `admin`, `pengurus`
- **Deskripsi**: Menjalankan pembuatan tagihan iuran bulanan untuk seluruh anggota aktif secara idempoten.
- **Request Body**:
  ```json
  {
    "jenis_iuran_id": "0193108d-5122-7bc1-...",
    "periode": "2026-11"
  }
  ```

#### 2. `GET /api/v1/iuran/menunggak?min_bulan=2`
- **Akses**: Khusus Role: `admin`, `pengurus`
- **Deskripsi**: Menampilkan daftar anggota yang menunggak iuran minimal $N$ bulan.

---

### D. Katalog & Diskon Anggota
#### 1. `GET /api/v1/katalog/stok`
- **Akses**: Semua Pengguna
- **Deskripsi**: Menampilkan stok saldo produk dan sisa stok bebas jual.

#### 2. `GET /api/v1/katalog/harga?produk_id={id}&anggota_id={id}`
- **Akses**: Semua Pengguna
- **Deskripsi**: Menghitung harga akhir secara dinamis dengan mengecek diskon khusus anggota dan promo terbaik.

---

## 5. Panduan Praktis Integrasi Front-End (Client SDK)

Berikut adalah contoh modul API Client siap pakai untuk Front-End (TypeScript / Axios atau Fetch API):

```typescript
// lib/apiClient.ts
const BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

export interface APIResponse<T> {
  success: boolean;
  data?: T;
  meta?: any;
  error?: {
    code: string;
    message: string;
    details?: any;
  };
}

export async function apiFetch<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<T> {
  const token = typeof window !== "undefined" ? localStorage.getItem("kopdes_token") : null;

  const headers = new Headers(options.headers || {});
  headers.set("Content-Type", "application/json");
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const res = await fetch(`${BASE_URL}${endpoint}`, {
    ...options,
    headers,
  });

  const body: APIResponse<T> = await res.json();

  if (!res.ok || !body.success) {
    throw new Error(body.error?.message || "Terjadi kesalahan pada server");
  }

  return body.data as T;
}
```

### Contoh Komponen React Pembayaran QRIS:
```tsx
import React, { useState, useEffect } from "react";
import QRCode from "qrcode.react"; // npm install qrcode.react
import { apiFetch } from "../lib/apiClient";

interface QRISData {
  pembayaran_id: string;
  kode_unik: number;
  nominal_total: number;
  qris_payload: string;
  nama_merchant: string;
  berlaku_sampai: string;
  sisa_waktu_detik: number;
}

export function PembayaranQRISView({ nominalDasar }: { nominalDasar: number }) {
  const [qris, setQRIS] = useState<QRISData | null>(null);
  const [sisaDetik, setSisaDetik] = useState(0);
  const [status, setStatus] = useState("menunggu");

  // 1. Buat transaksi QRIS saat komponen dipasang
  useEffect(() => {
    async function initQRIS() {
      try {
        const data = await apiFetch<QRISData>("/api/v1/pembayaran/qris", {
          method: "POST",
          body: JSON.stringify({
            nominal_dasar: nominalDasar,
            jenis_transaksi: "iuran_bulanan",
          }),
        });
        setQRIS(data);
        setSisaDetik(data.sisa_waktu_detik);
      } catch (err: any) {
        alert(err.message);
      }
    }
    initQRIS();
  }, [nominalDasar]);

  // 2. Countdown Timer
  useEffect(() => {
    if (sisaDetik <= 0) return;
    const interval = setInterval(() => {
      setSisaDetik((prev) => (prev > 0 ? prev - 1 : 0));
    }, 1000);
    return () => clearInterval(interval);
  }, [sisaDetik]);

  // 3. Polling Status Setiap 5 Detik
  useEffect(() => {
    if (!qris || status === "disetujui") return;
    const poller = setInterval(async () => {
      try {
        const res = await apiFetch<{ status: string }>(`/api/v1/pembayaran/${qris.pembayaran_id}`);
        if (res.status === "disetujui") {
          setStatus("disetujui");
          clearInterval(poller);
        }
      } catch (_) {}
    }, 5000);
    return () => clearInterval(poller);
  }, [qris, status]);

  if (!qris) return <p>Memuat QRIS...</p>;
  if (status === "disetujui") return <h2>Pembayaran Berhasil Dikonfirmasi!</h2>;

  const menit = Math.floor(sisaDetik / 60);
  const detik = sisaDetik % 60;

  return (
    <div style={{ textAlign: "center", padding: "24px" }}>
      <h3>{qris.nama_merchant}</h3>
      <QRCode value={qris.qris_payload} size={256} />
      <h1 style={{ color: "#d32f2f" }}>
        Rp {qris.nominal_total.toLocaleString("id-ID")}
      </h1>
      <p>
        Wajib transfer tepat hingga 3 digit terakhir: <b>{qris.kode_unik}</b>
      </p>
      <p style={{ color: sisaDetik < 300 ? "red" : "gray" }}>
        Batas Waktu: {menit}:{detik < 10 ? `0${detik}` : detik}
      </p>
    </div>
  );
}
```

---

## 6. Cara Menjalankan Server Backend

1. **Jalankan Server**:
   ```bash
   go run ./cmd/server
   ```
   *Server akan aktif pada port `:8080` dan otomatis terhubung ke PostgreSQL 16.*

2. **Jalankan Seluruh Unit & Integration Test**:
   ```bash
   go test -v ./...
   ```
   *Memvalidasi konkurensi QRIS, isolasi RLS multi-tenant, maker-checker, dan pengujian API HTTP.*
