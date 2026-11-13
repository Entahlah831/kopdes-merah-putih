// GENERATED FROM docs/openapi.yaml via scripts/generate_api_artifacts.py — DO NOT EDIT MANUALLY.

export interface APIError {
  code: string;
  message: string;
  details?: unknown;
}

export interface APIResponse<T = unknown> {
  success: boolean;
  data?: T;
  meta?: Record<string, unknown>;
  error?: APIError;
}

export interface LoginRequest {
  email: string;
  password: string;
  koperasi_id?: string;
}

export interface MFAEnableRequest {
  secret: string;
  totp_code: string;
}

export interface MFAVerifyRequest {
  mfa_session_token: string;
  totp_code?: string;
  recovery_code?: string;
}

export interface RefreshRequest {
  refresh_token: string;
}

export interface AjukanPendaftaranRequest {
  nik: string;
  nama_lengkap: string;
  tempat_lahir: string;
  tanggal_lahir: string;
  jenis_kelamin: "L" | "P";
  pekerjaan?: string;
  alamat_ktp: string;
  wilayah_desa_id: string;
}

export interface VerifikasiPendaftaranRequest {
  status: "disetujui" | "ditolak" | "perbaikan";
  catatan_perbaikan?: string;
  alasan_ditolak?: string;
}

export interface UpdateStatusAnggotaRequest {
  status: "aktif" | "nonaktif" | "dibekukan" | "keluar";
  alasan?: string;
  catatan?: string;
}

export interface UpdateUserRoleRequest {
  role: "anggota" | "operator" | "pengawas" | "pengurus" | "bendahara" | "ketua" | "admin";
}

export interface CreateJenisIuranRequest {
  kode: string;
  nama: string;
  tipe?: "pokok" | "wajib" | "sukarela";
  nominal_standar?: number;
  nominal?: number;
}

export interface GenerateTagihanRequest {
  jenis_iuran_id: string;
  periode: string;
}

export interface CreateProdukRequest {
  nama: string;
  sku: string;
  deskripsi?: string;
  satuan_dasar?: string;
  harga_umum?: number;
  harga_anggota?: number;
  stok_awal?: number;
}

export interface CreatePromoRequest {
  kode: string;
  nama: string;
  tipe_promo: "nominal" | "persen";
  nilai_diskon: number;
  minimal_belanja?: number;
  durasi_hari?: number;
}

export interface AddToCartRequest {
  produk_id: string;
  qty: number;
}

export interface CheckoutRequest {
  metode_serah_terima: "ambil_di_koperasi" | "kirim_alamat";
  catatan?: string;
  kode_promo?: string;
}

export interface BuatQRISRequest {
  nominal_dasar?: number;
  tagihan_iuran_id?: Array<string>;
  tagihan_iuran_ids?: Array<string>;
  pesanan_id?: string;
  jenis_transaksi?: "iuran_bulanan" | "belanja_toko" | "campuran";
  anggota_id?: string;
  metode_pembayaran_id?: string;
  rekening_qris_id?: string;
}

export interface KlaimBuktiRequest {
  referensi_rrn?: string;
  catatan?: string;
}

export interface VerifikasiRequest {
  status?: "disetujui" | "ditolak";
  disetujui?: boolean;
  nominal_aktual?: number;
  alasan_tolak?: string;
  referensi_rrn?: string;
  catatan?: string;
}

export interface AdminDaftarRekeningQRISRequest {
  nama_merchant: string;
  nmid: string;
  nama_bank_pjp: string;
  no_rekening_settlement: string;
  nama_pemilik_rekening: string;
  is_atas_nama_badan_hukum?: boolean;
  jenis_pemilik?: "badan_hukum_koperasi" | "perorangan_pengurus";
  qris_payload_statis: string;
}

export type KopdesFetcher = (path: string, init?: RequestInit) => Promise<Response>;

export function createKopdesEndpoints(fetcher: KopdesFetcher) {
  return {
    getHealthz: () =>
      fetcher(`/healthz`, { method: "GET" }),
    getReadyz: () =>
      fetcher(`/readyz`, { method: "GET" }),
    getMetrics: () =>
      fetcher(`/metrics`, { method: "GET" }),
    authLogin: (body: LoginRequest) =>
      fetcher(`/api/v1/auth/login`, { method: "POST", body: JSON.stringify(body) }),
    authMfaSetup: () =>
      fetcher(`/api/v1/auth/mfa/setup`, { method: "POST" }),
    authMfaEnable: (body: MFAEnableRequest) =>
      fetcher(`/api/v1/auth/mfa/enable`, { method: "POST", body: JSON.stringify(body) }),
    authMfaVerify: (body: MFAVerifyRequest) =>
      fetcher(`/api/v1/auth/mfa/verify`, { method: "POST", body: JSON.stringify(body) }),
    authRefresh: (body: RefreshRequest) =>
      fetcher(`/api/v1/auth/refresh`, { method: "POST", body: JSON.stringify(body) }),
    authRevoke: (body: RefreshRequest) =>
      fetcher(`/api/v1/auth/revoke`, { method: "POST", body: JSON.stringify(body) }),
    authMe: () =>
      fetcher(`/api/v1/auth/me`, { method: "GET" }),
    createPendaftaran: (body: AjukanPendaftaranRequest) =>
      fetcher(`/api/v1/pendaftaran`, { method: "POST", body: JSON.stringify(body) }),
    getPendaftaran: () =>
      fetcher(`/api/v1/pendaftaran/{id}`, { method: "GET" }),
    uploadDokumenPendaftaran: (body: FormData) =>
      fetcher(`/api/v1/pendaftaran/{id}/dokumen`, { method: "POST", body: body }),
    verifikasiPendaftaran: (body: VerifikasiPendaftaranRequest) =>
      fetcher(`/api/v1/pendaftaran/{id}/verifikasi`, { method: "POST", body: JSON.stringify(body) }),
    purgePendaftaranDitolak: () =>
      fetcher(`/api/v1/pendaftaran/purge-ditolak`, { method: "POST" }),
    listAnggota: () =>
      fetcher(`/api/v1/anggota`, { method: "GET" }),
    getAnggota: () =>
      fetcher(`/api/v1/anggota/{id}`, { method: "GET" }),
    updateAnggotaStatus: (body: UpdateStatusAnggotaRequest) =>
      fetcher(`/api/v1/anggota/{id}/status`, { method: "PATCH", body: JSON.stringify(body) }),
    listUsers: () =>
      fetcher(`/api/v1/users`, { method: "GET" }),
    getUserDetail: () =>
      fetcher(`/api/v1/users/{id}`, { method: "GET" }),
    updateUserRole: (body: UpdateUserRoleRequest) =>
      fetcher(`/api/v1/users/{id}/role`, { method: "PATCH", body: JSON.stringify(body) }),
    listJenisIuran: () =>
      fetcher(`/api/v1/iuran/jenis`, { method: "GET" }),
    createJenisIuran: (body: CreateJenisIuranRequest) =>
      fetcher(`/api/v1/iuran/jenis`, { method: "POST", body: JSON.stringify(body) }),
    getTagihanSaya: () =>
      fetcher(`/api/v1/iuran/tagihan/saya`, { method: "GET" }),
    generateTagihanIuran: (body: GenerateTagihanRequest) =>
      fetcher(`/api/v1/iuran/generate`, { method: "POST", body: JSON.stringify(body) }),
    listIuranMenunggak: () =>
      fetcher(`/api/v1/iuran/menunggak`, { method: "GET" }),
    listKatalogStok: () =>
      fetcher(`/api/v1/katalog/stok`, { method: "GET" }),
    getHargaAnggota: () =>
      fetcher(`/api/v1/katalog/harga`, { method: "GET" }),
    listProduk: () =>
      fetcher(`/api/v1/katalog/produk`, { method: "GET" }),
    createProduk: (body: CreateProdukRequest) =>
      fetcher(`/api/v1/katalog/produk`, { method: "POST", body: JSON.stringify(body) }),
    updateProduk: (body: CreateProdukRequest) =>
      fetcher(`/api/v1/katalog/produk/{id}`, { method: "PUT", body: JSON.stringify(body) }),
    deleteProduk: () =>
      fetcher(`/api/v1/katalog/produk/{id}`, { method: "DELETE" }),
    listPromo: () =>
      fetcher(`/api/v1/katalog/promo`, { method: "GET" }),
    createPromo: (body: CreatePromoRequest) =>
      fetcher(`/api/v1/katalog/promo`, { method: "POST", body: JSON.stringify(body) }),
    getKeranjang: () =>
      fetcher(`/api/v1/toko/keranjang`, { method: "GET" }),
    addToCart: (body: AddToCartRequest) =>
      fetcher(`/api/v1/toko/keranjang`, { method: "POST", body: JSON.stringify(body) }),
    removeFromCart: (produk_id: string) =>
      fetcher(`/api/v1/toko/keranjang/${encodeURIComponent(produk_id)}`, { method: "DELETE" }),
    checkoutToko: (body: CheckoutRequest) =>
      fetcher(`/api/v1/toko/checkout`, { method: "POST", body: JSON.stringify(body) }),
    listPesanan: () =>
      fetcher(`/api/v1/toko/pesanan`, { method: "GET" }),
    batalPesanan: () =>
      fetcher(`/api/v1/toko/pesanan/{id}/batal`, { method: "POST" }),
    expirePesanan: () =>
      fetcher(`/api/v1/toko/pesanan/{id}/expire`, { method: "POST" }),
    createPembayaranQRIS: (body: BuatQRISRequest, idempotencyKey?: string) =>
      fetcher(`/api/v1/pembayaran/qris`, { method: "POST", headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : undefined, body: JSON.stringify(body) }),
    getPembayaranStatus: () =>
      fetcher(`/api/v1/pembayaran/{id}`, { method: "GET" }),
    klaimBuktiPembayaran: (body: KlaimBuktiRequest) =>
      fetcher(`/api/v1/pembayaran/{id}/bukti`, { method: "POST", body: JSON.stringify(body) }),
    listAntreanVerifikasi: () =>
      fetcher(`/api/v1/pembayaran/antrean`, { method: "GET" }),
    verifikasiPembayaran: (body: VerifikasiRequest) =>
      fetcher(`/api/v1/pembayaran/{id}/verifikasi`, { method: "POST", body: JSON.stringify(body) }),
    imporMutasiBank: (body: FormData) =>
      fetcher(`/api/v1/pembayaran/mutasi-bank/impor`, { method: "POST", body: body }),
    laporanRekonsiliasiHarian: () =>
      fetcher(`/api/v1/pembayaran/rekonsiliasi/harian`, { method: "GET" }),
    listRekeningQRIS: () =>
      fetcher(`/api/v1/koperasi/rekening-qris`, { method: "GET" }),
    createRekeningQRIS: (body: AdminDaftarRekeningQRISRequest) =>
      fetcher(`/api/v1/koperasi/rekening-qris`, { method: "POST", body: JSON.stringify(body) }),
    deactivateRekeningQRIS: () =>
      fetcher(`/api/v1/koperasi/rekening-qris/{id}`, { method: "DELETE" }),
    listKasMutasi: () =>
      fetcher(`/api/v1/kas/mutasi`, { method: "GET" }),
    getLaporanKeuangan: () =>
      fetcher(`/api/v1/laporan/keuangan`, { method: "GET" }),
    getLaporanAnggota: () =>
      fetcher(`/api/v1/laporan/anggota`, { method: "GET" }),
  };
}
