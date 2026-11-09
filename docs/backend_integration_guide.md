# Panduan Integrasi Backend & Dokumentasi API — Kopdes Merah Putih

Dokumen ini disusun untuk **Divisi Front-End**, **Mobile Developer**, dan **DevOps/Backend Engineer** sebagai referensi arsitektur, daftar modul, sistem keamanan, endpoint API, alur pendaftaran QRIS, mekanisme Auto-Lock Nominal, serta panduan deployment Docker untuk Server/VPS.

---

## 1. Struktur Modul & File Backend

```text
kopdes-merah-putih/
├── cmd/
│   └── server/
│       └── main.go                    # Entry point server HTTP, pembaca dinamis .env & graceful shutdown
├── internal/
│   ├── api/
│   │   ├── handlers/                  # HTTP Handlers (Controller)
│   │   │   ├── auth_handler.go        # Login, profil, autentikasi berbasis JWT
│   │   │   ├── qris_handler.go        # Pembuatan QRIS, Auto-Lock, antrean, verifikasi & Admin QRIS
│   │   │   ├── iuran_handler.go       # Generator tagihan bulanan idempoten & list penunggak
│   │   │   ├── katalog_handler.go     # Stok saldo gudang & kalkulasi harga diskon anggota
│   │   │   └── health_handler.go      # Health check (/healthz & /readyz)
│   │   ├── middleware/                # Pipeline Keamanan HTTP
│   │   │   ├── auth.go                # Validasi Bearer JWT & RBAC Role Guard
│   │   │   ├── tenant.go              # Inject app.current_koperasi_id ke pgx session (RLS)
│   │   │   ├── cors.go                # CORS handler terkonfigurasi via .env
│   │   │   ├── ratelimit.go           # Token-bucket IP rate limiter (anti-DDoS/brute force)
│   │   │   ├── security_headers.go    # OWASP Recommended Security Headers
│   │   │   └── recovery.go            # Panic recovery & graceful error JSON
│   │   ├── response/
│   │   │   └── response.go            # Standarisasi JSON envelope {success, data, error, meta}
│   │   ├── api_test.go                # Test suite integrasi HTTP API (CORS, Auth, QRIS, Admin)
│   │   └── server.go                  # Routing multiplexer & middleware chaining
│   ├── qris/
│   │   ├── emvco.go                   # Parser TLV, generator QRIS Dinamis & kalkulator CRC16-CCITT
│   │   └── emvco_test.go              # Test suite TLV, CRC16 test-vectors & Auto-Lock Nominal
│   ├── auth/
│   │   └── jwt.go                     # Generator & verifikator token JWT (HS256)
│   ├── security/
│   │   └── crypto.go                  # AES-256-GCM (UU PDP), HMAC Blind Index, bcrypt
│   └── db/                            # Hasil kompilasi sqlc (Tipe-aman)
│       ├── db.go                      # DBTX interface & New(db)
│       ├── models.go                  # Struct entitas PostgreSQL
│       └── kopdes.sql.go              # Query fungsi terkompilasi
├── db/
│   ├── migrations/
│   │   └── 000001_init_kopdes_schema.up.sql  # 1.700 baris DDL PostgreSQL 16 (RLS, Partisi, Trigger)
│   └── queries/
│       └── kopdes.sql                 # File query SQL sqlc
├── Dockerfile                         # Multi-stage Docker build untuk VPS / Server
├── docker-compose.yml                 # Orkestrasi PostgreSQL 16 & Go Backend API
├── .env.example                       # Contoh template konfigurasi port & database
├── service.go                         # Logika alokasi kode unik QRIS & iuran bulanan
├── service_test.go                    # Uji konkurensi goroutine, anti-tabrakan QRIS & isolasi RLS
├── docs/
│   ├── erd.md                         # Diagram ERD data relasional
│   └── backend_integration_guide.md   # Dokumen panduan ini
├── go.mod                             # Definisi modul & dependensi Go
└── sqlc.yaml                          # Konfigurasi sqlc engine
```

---

## 2. Fleksibilitas Port & Konfigurasi Lingkungan (`.env`)

Sistem dirancang agar port database dan port HTTP server API dapat diubah sewaktu-waktu tanpa perlu mengedit kode program:

```env
# 1. Port HTTP Server API (Bisa 8080, 3000, 5000, dll)
PORT=8080
APP_PORT=8080

# 2. Port & Kredensial Database PostgreSQL 16
# Ubah DB_PORT ke 5432 (default docker/VPS) atau 5433 (port pengujian dev lokal)
DB_HOST=127.0.0.1
DB_PORT=5433
DB_USER=kopdes_app
DB_PASSWORD=kopdes_dev_password
DB_NAME=kopdes_db
DB_SSLMODE=disable

# DSN Database (Otomatis menyerap variabel di atas)
DATABASE_URL=postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${DB_SSLMODE}

# 3. Kunci Kriptografi (32-byte Hex Random)
JWT_SECRET=4195a376a9c8dcafec4fc3eabf6723681224d4d1aa69bd5df68159f8b537ec7a
BLIND_INDEX_SECRET=67c1e8ce4b201264b2794bfc2275079f10dd5fba5a837b3afb76f41428f824a2
AES_MASTER_KEY=8311388c97cabf827500afc01f8cfaa3d68b957c7061ae86c095e5962715b063
QRIS_SECRET=1b43d46e7bbecedf897fc4c3200974391c0e0815a13dc32620bcdf2d565f6d65

# 4. Kebijakan Jaringan & Rate Limit
ALLOWED_ORIGINS=http://localhost:3000,http://localhost:5173,http://127.0.0.1:3000
RATE_LIMIT_RPS=30
RATE_LIMIT_BURST=60
```

---

## 3. Fitur Unggulan: Auto-Lock Nominal QRIS

### Apakah QRIS Statis Bisa Mengunci Nominal Otomatis?
**BISA dan 100% Sah sesuai regulasi Bank Indonesia (PADG BI 21/18/PADG/2019) dan standar EMVCo QR Code.**

Ketika koperasi mendaftar ke Bank Acquirer (BRI, Mandiri, BNI, BCA, dll), bank memberikan stiker QRIS Statis dengan tag initiation `01 = 11`. Backend Kopdes Merah Putih secara cerdas mengonversinya secara *on-the-fly* menjadi QRIS Dinamis berfitur **Auto-Lock Nominal**:

```
[QRIS Statis Bank] ──► [Backend Kopdes] ───────────────► [QRIS Dinamis Terkunci]
 Tag 01: '11' (Statis)   1. Ubah Tag 01 ke '12' (Dinamis)  Tag 01: '12'
 Tanpa Tag 54 (Nominal)  2. Injeksi Tag 54 (Nominal Total) Tag 54: '20142' (Terkunci!)
 Tanpa Tag 62 (Invoice)  3. Injeksi Tag 62 (No Pembayaran) Tag 62: 'PAY-1234'
 CRC16 Lama              4. Hitung Ulang CRC16 (Tag 63)   Tag 63: 'E8E9'
```

### Keuntungan untuk Anggota Koperasi:
1. **Tidak Ada Human Error:** Saat anggota scan QR code lewat m-banking (BCA, Livin by Mandiri, BRImo, BNI, GoPay, OVO, ShopeePay, Dana, LinkAja), **layar nominal transfer otomatis terkunci** pada angka pas (misal `Rp 20.142`). Anggota tidak perlu mengetik nominal lagi!
2. **Fallback Stiker Fisik:** Jika anggota memilih scan dari stiker kertas/akrilik yang tertempel di dinding kantor koperasi, sistem *linear probing* 3-digit unik tetap aktif sehingga pengurus dapat memverifikasi bukti bayar dengan akurat.

---

## 4. Daftar Endpoint API

### A. Autentikasi Pengguna
#### 1. `POST /api/v1/auth/login`
- **Akses**: Publik
- **Body**:
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
      "expires_in_seconds": 86400,
      "user": {
        "id": "0193108c-9a4f-7cd3-...",
        "nama_lengkap": "Budi Utomo",
        "email": "budi@kopdes.id",
        "koperasi_id": "0193108c-8f12-7000-...",
        "roles": ["anggota"]
      }
    }
  }
  ```

#### 2. `GET /api/v1/auth/me`
- **Akses**: Butuh Bearer Token
- **Headers**: `Authorization: Bearer <token>`
- **Response**: Mengembalikan data profil user yang sedang login.

---

### B. Transaksi Pembayaran & QRIS Anggota
#### 1. `POST /api/v1/pembayaran/qris`
- **Akses**: Semua Anggota / User Terotentikasi
- **Body**:
  ```json
  {
    "nominal_dasar": 50000,
    "jenis_transaksi": "iuran_bulanan",
    "anggota_id": "0193108c-bf53-7000-..."
  }
  ```
- **Response `201 Created`**:
  ```json
  {
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
  }
  ```
  *(Catatan Front-End: Langsung masukkan string `data.qris_payload` ke komponen QR Code SVG/Canvas. Saat discan, nominal Rp 50.112 otomatis terkunci!)*

#### 2. `GET /api/v1/pembayaran/{id}`
- **Akses**: Pemilik Transaksi / Pengurus
- **Response `200 OK`**:
  ```json
  {
    "success": true,
    "data": {
      "id": "0193109a-4c12-7011-...",
      "status": "menunggu_pembayaran",
      "no_pembayaran": "PAY-886f888a-0300",
      "nominal_total": "50112.00",
      "berlaku_sampai": "2026-11-09T16:00:00Z",
      "referensi_rrn": null
    }
  }
  ```

---

### C. Manajemen Rekening QRIS Koperasi (Khusus Pengurus / Admin)
Setiap Koperasi Desa wajib mendaftarkan rekening merchant QRIS mereka sebelum anggota dapat bertransaksi.

#### 1. `POST /api/v1/koperasi/rekening-qris`
- **Akses**: Role `admin`, `pengurus`
- **Body**:
  ```json
  {
    "nama_merchant": "KOPDES MERAH PUTIH MANDIRI",
    "nmid": "ID1020304050607",
    "nama_bank_pjp": "Bank Mandiri",
    "no_rekening_settlement": "1300099887766",
    "nama_pemilik_rekening": "Koperasi Merah Putih Mandiri",
    "is_atas_nama_badan_hukum": true,
    "qris_payload_statis": "0002010102112666...6304ABCD"
  }
  ```
- **Validasi Keamanan**:
  - `is_atas_nama_badan_hukum` wajib `true` (PADG BI 21/18/PADG/2019 melarang QRIS perorangan untuk operasional badan usaha koperasi).
- **Response `201 Created`**:
  ```json
  {
    "success": true,
    "data": {
      "id": "019310ba-1100-7fa3-...",
      "nama_merchant": "KOPDES MERAH PUTIH MANDIRI",
      "nmid": "ID1020304050607",
      "status_aktif": true,
      "pesan": "Rekening QRIS statis badan hukum koperasi berhasil didaftarkan."
    }
  }
  ```

#### 2. `GET /api/v1/koperasi/rekening-qris`
- **Akses**: Role `admin`, `pengurus`
- **Deskripsi**: Mengambil daftar seluruh konfigurasi rekening QRIS aktif milik koperasi tenant saat ini.

#### 3. `DELETE /api/v1/koperasi/rekening-qris/{id}`
- **Akses**: Role `admin`, `pengurus`
- **Deskripsi**: Menonaktifkan konfigurasi QRIS koperasi (soft-delete).

---

### D. Verifikasi Pembayaran (Maker-Checker Guard)
#### 1. `GET /api/v1/pembayaran/antrean`
- **Akses**: Role `admin`, `pengurus`, `operator`, `pengawas`
- **Deskripsi**: Menampilkan antrean klaim pembayaran yang menunggu verifikasi admin.

#### 2. `POST /api/v1/pembayaran/{id}/verifikasi`
- **Akses**: Role `admin`, `pengurus`, `operator`
- **Aturan Tegas**: **Maker-Checker Violation** — Jika admin yang memverifikasi adalah orang yang sama dengan pembuat pembayaran (`diverifikasi_oleh == pembayar_user_id`), server otomatis menolak dengan **HTTP 403 Forbidden**.
- **Body**:
  ```json
  {
    "disetujui": true,
    "referensi_rrn": "123456789012",
    "nominal_aktual": 50112,
    "catatan": "Transfer masuk di mutasi Bank Mandiri jam 14.30"
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

## 5. Rekomendasi Bahasa & Teknologi Front-End

Untuk membangun aplikasi front-end yang aman, cepat, dan mudah di-maintain, berikut rekomendasi stack terbaik:

```
┌────────────────────────────────────────────────────────┐
│             PORTAL WEB (Admin Desa & Anggota)          │
│  Teknologi: Next.js 14+ (App Router) + TypeScript      │
│  Styling: Tailwind CSS + shadcn/ui                     │
│  Alasan: SSR untuk performa, typed contract dengan Go,  │
│          mudah diakses via browser desktop kantor desa │
└────────────────────────────────────────────────────────┘
                           ▲
                           │ HTTP REST API (JSON)
                           ▼
┌────────────────────────────────────────────────────────┐
│           APLIKASI MOBILE (Anggota & POS Kasir)        │
│  Teknologi: Flutter (Dart)                             │
│  Alasan: Satu codebase untuk Android & iOS, akses      │
│          kamera instan untuk scan bukti bayar,         │
│          performa 60fps rendering lancar di HP murah   │
└────────────────────────────────────────────────────────┘
```

---

## 6. Contoh Kode Integrasi Front-End Siap Pakai

### A. Modul Klien API (TypeScript / Fetch)
```typescript
// lib/apiClient.ts
const BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

export interface APIEnvelope<T> {
  success: boolean;
  data: T;
  error?: {
    code: string;
    message: string;
  };
}

export async function kopdesAPI<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const token = typeof window !== "undefined" ? localStorage.getItem("access_token") : null;
  const headers = new Headers(options.headers || {});
  headers.set("Content-Type", "application/json");
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const res = await fetch(`${BASE_URL}${endpoint}`, { ...options, headers });
  const json: APIEnvelope<T> = await res.json();

  if (!res.ok || !json.success) {
    throw new Error(json.error?.message || "Terjadi kesalahan pada server");
  }
  return json.data;
}
```

### B. Komponen React Tampilan QRIS Auto-Lock
```tsx
// components/PembayaranQRIS.tsx
import React, { useState, useEffect } from "react";
import QRCode from "qrcode.react"; // npm install qrcode.react
import { kopdesAPI } from "../lib/apiClient";

interface QRISResponse {
  pembayaran_id: string;
  nominal_total: number;
  qris_payload: string;
  nama_merchant: string;
  sisa_waktu_detik: number;
}

export function PembayaranQRIS({ nominalDasar }: { nominalDasar: number }) {
  const [data, setData] = useState<QRISResponse | null>(null);
  const [sisaWaktu, setSisaWaktu] = useState(0);
  const [lunas, setLunas] = useState(false);

  useEffect(() => {
    kopdesAPI<QRISResponse>("/api/v1/pembayaran/qris", {
      method: "POST",
      body: JSON.stringify({ nominal_dasar: nominalDasar, jenis_transaksi: "iuran_bulanan" }),
    }).then((res) => {
      setData(res);
      setSisaWaktu(res.sisa_waktu_detik);
    });
  }, [nominalDasar]);

  // Hitung mundur waktu kadaluarsa
  useEffect(() => {
    if (sisaWaktu <= 0) return;
    const timer = setInterval(() => setSisaWaktu((t) => (t > 0 ? t - 1 : 0)), 1000);
    return () => clearInterval(timer);
  }, [sisaWaktu]);

  // Polling cek status setiap 4 detik
  useEffect(() => {
    if (!data || lunas) return;
    const poll = setInterval(async () => {
      try {
        const res = await kopdesAPI<{ status: string }>(`/api/v1/pembayaran/${data.pembayaran_id}`);
        if (res.status === "berhasil_diverifikasi") {
          setLunas(true);
          clearInterval(poll);
        }
      } catch (_) {}
    }, 4000);
    return () => clearInterval(poll);
  }, [data, lunas]);

  if (!data) return <p className="text-gray-500">Menyiapkan QRIS Auto-Lock...</p>;
  if (lunas) return <div className="p-6 bg-green-100 text-green-800 rounded-xl font-bold">Pembayaran Berhasil Dikonfirmasi!</div>;

  return (
    <div className="flex flex-col items-center p-6 bg-white rounded-2xl shadow-lg border border-gray-100">
      <h3 className="font-semibold text-lg">{data.nama_merchant}</h3>
      <div className="p-4 bg-white border-2 border-dashed border-red-500 rounded-xl my-4">
        {/* Render string dynamic QRIS berfitur auto-lock */}
        <QRCode value={data.qris_payload} size={240} level="M" />
      </div>
      <p className="text-sm text-gray-500">Scan menggunakan BCA, Livin, BRImo, Gopay, atau Dana</p>
      <h2 className="text-3xl font-extrabold text-red-600 mt-2">
        Rp {data.nominal_total.toLocaleString("id-ID")}
      </h2>
      <span className="text-xs bg-red-100 text-red-700 px-3 py-1 rounded-full mt-2 font-medium">
        Nominal terkunci otomatis di aplikasi bank Anda
      </span>
      <p className="text-xs text-gray-400 mt-3">
        Batas waktu: {Math.floor(sisaWaktu / 60)}:{sisaWaktu % 60 < 10 ? `0${sisaWaktu % 60}` : sisaWaktu % 60}
      </p>
    </div>
  );
}
```

---

## 7. Cara Deployment Server / VPS (Docker Compose)

Untuk menjalankan backend di VPS Ubuntu/Debian produksi:

1. **Pastikan Docker & Docker Compose Terpasang:**
   ```bash
   sudo apt update && sudo apt install -y docker.io docker-compose-v2
   ```

2. **Kloning Repositori & Siapkan Konfigurasi `.env`:**
   ```bash
   git clone https://github.com/Entahlah831/kopdes-merah-putih.git
   cd kopdes-merah-putih
   cp .env.example .env
   # Edit .env dan sesuaikan JWT_SECRET, AES_MASTER_KEY, serta password database
   nano .env
   ```

3. **Jalankan dengan Satu Perintah:**
   ```bash
   docker compose up -d --build
   ```
   *Docker Compose akan secara otomatis menyalakan PostgreSQL 16, menjalankan migrasi skema tabel, dan mengaktifkan REST API Go.*

4. **Periksa Status Kontainer:**
   ```bash
   docker compose ps
   curl http://localhost:8080/healthz
   ```
