export interface RuntimeConfigJSON {
  app_env: string;
  api_public_url: string;
  ui_bind: string;
  ui_port: number;
  resources?: Record<string, string>;
  seeded_accounts?: Array<{
    email: string;
    role: string;
    requires_mfa: boolean;
  }>;
}

export interface PublicConfigData {
  api_version: string;
  timezone: string;
  payment_expiry_seconds: number;
  polling_interval_ms: number;
  stock_hold_minutes: number;
  mfa_mandatory_roles: string[];
  features: {
    payments_enabled: boolean;
    registration_enabled: boolean;
    login_enabled: boolean;
    read_only_mode: boolean;
  };
  captcha?: {
    enabled: boolean;
    provider?: string;
    site_key?: string;
  };
  auth: {
    cookie_primary: boolean;
    bearer_enabled: boolean;
    csrf_header: string;
  };
  koperasi?: {
    nama_koperasi: string;
    alamat_koperasi: string;
    telepon_koperasi: string;
    qris_unique_digits: number;
    ongkir_tetap: number;
    kebijakan_teks?: string;
    active_qris?: {
      rekening_id?: string;
      nama_merchant?: string;
      nmid?: string;
      jenis_pemilik?: string;
      qris_image_url?: string;
    };
  };
}

export interface UserSession {
  user_id: string;
  email: string;
  nama: string;
  peran: string;
  scope?: string;
  mfa_enabled?: boolean;
  anggota_id?: string | null;
}

export interface ProdukItem {
  id: string;
  kategori_id?: string;
  nama_kategori?: string;
  sku: string;
  nama: string;
  satuan: string;
  harga_umum?: number;
  harga_anggota?: number;
  harga_aktif?: number;
  tier_harga?: string;
  stok: number;
  stok_tertahan: number;
  stok_tersedia: number;
  status: string;
  foto_url?: string;
}

export interface CartEntry {
  produk: ProdukItem;
  kuantitas: number;
}

export type RouteKey =
  | "beranda"
  | "katalog"
  | "daftar"
  | "masuk"
  | "tagihan"
  | "pesanan"
  | "dashboard"
  | "verifikasi_anggota"
  | "verifikasi_pembayaran"
  | "operasional"
  | "pengaturan_user";
