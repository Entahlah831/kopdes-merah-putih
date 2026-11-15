export type ErrorUiPattern =
  | "inline_field_and_banner"
  | "silent_refresh_then_login_redirect"
  | "permission_banner"
  | "wizard_redirect_mfa_setup"
  | "empty_state"
  | "warning_banner"
  | "inline_file_error"
  | "toast_warning"
  | "countdown_cooldown_banner"
  | "restart_login_banner"
  | "fallback_error_with_request_id"
  | "maintenance_banner"
  | "global_readonly_banner"
  | "feature_unavailable_notice";

export interface ErrorCatalogEntry {
  code: string;
  http_status: number;
  title_id: string;
  message_id: string;
  user_action_id: string;
  retryable: boolean;
  retry_strategy: string;
  ui_pattern: ErrorUiPattern;
  related_claim_id: string;
}

export const ERROR_CATALOG_ID: Record<string, ErrorCatalogEntry> = {
  BAD_REQUEST: {
    code: "BAD_REQUEST",
    http_status: 400,
    title_id: "Data Input Tidak Valid",
    message_id: "Isian formulir belum sesuai format atau ada kolom wajib yang masih kosong.",
    user_action_id: "Periksa kembali pesan kesalahan di bawah kolom terkait lalu perbaiki sebelum mengirim ulang.",
    retryable: false,
    retry_strategy: "none",
    ui_pattern: "inline_field_and_banner",
    related_claim_id: "CLAIM-VALIDATION-400-01",
  },
  UNAUTHORIZED: {
    code: "UNAUTHORIZED",
    http_status: 401,
    title_id: "Sesi Masuk Berakhir",
    message_id: "Sesi Anda telah berakhir, kode OTP salah, atau token autentikasi tidak berlaku lagi.",
    user_action_id: "Coba perbarui sesi otomatis (single-flight refresh); jika tetap gagal, arahkan pengguna ke halaman Masuk.",
    retryable: true,
    retry_strategy: "single_flight_refresh_once",
    ui_pattern: "silent_refresh_then_login_redirect",
    related_claim_id: "CLAIM-AUTH-LOGIN-01",
  },
  FORBIDDEN: {
    code: "FORBIDDEN",
    http_status: 403,
    title_id: "Akses Ditolak",
    message_id: "Peran akun Anda tidak memiliki izin untuk aksi ini atau melanggar aturan pemisahan tugas (Maker-Checker / anti-eskalasi).",
    user_action_id: "Sembunyikan tombol mutasi sesuai matriks peran dan minta pengurus lain yang berwenang memprosesnya.",
    retryable: false,
    retry_strategy: "none",
    ui_pattern: "permission_banner",
    related_claim_id: "CLAIM-USERS-ESCALATION-01",
  },
  MFA_MANDATORY_SETUP_REQUIRED: {
    code: "MFA_MANDATORY_SETUP_REQUIRED",
    http_status: 403,
    title_id: "Wajib Aktifkan Keamanan Dua Langkah (MFA)",
    message_id: "Akun pengurus/admin/bendahara/ketua wajib mengaktifkan aplikasi autentikator TOTP sebelum dapat masuk ke sistem.",
    user_action_id: "Simpan data.setup_token di memori JS sementara dan arahkan segera ke wizard Aktivasi MFA (/auth/mfa/setup -> /enable).",
    retryable: false,
    retry_strategy: "redirect_mfa_enrollment",
    ui_pattern: "wizard_redirect_mfa_setup",
    related_claim_id: "CLAIM-AUTH-MFA-MANDATORY-01",
  },
  MFA_ENROLLMENT_REQUIRED: {
    code: "MFA_ENROLLMENT_REQUIRED",
    http_status: 403,
    title_id: "Token Terbatas Khusus Aktivasi MFA",
    message_id: "Token setup_token hanya dapat dipakai untuk menyelesaikan aktivasi MFA dan ditolak pada endpoint bisnis lain.",
    user_action_id: "Selesaikan pendaftaran TOTP sampai mendapat access_token penuh sebelum membuka dashboard.",
    retryable: false,
    retry_strategy: "redirect_mfa_enrollment",
    ui_pattern: "wizard_redirect_mfa_setup",
    related_claim_id: "CLAIM-AUTH-MFA-MANDATORY-01",
  },
  NOT_FOUND: {
    code: "NOT_FOUND",
    http_status: 404,
    title_id: "Data Tidak Ditemukan",
    message_id: "Data yang diminta tidak tersedia di koperasi Anda atau Anda tidak memiliki hak melihatnya.",
    user_action_id: "Tampilkan tampilan kosong yang ramah tanpa membocorkan apakah data milik anggota atau koperasi lain.",
    retryable: false,
    retry_strategy: "none",
    ui_pattern: "empty_state",
    related_claim_id: "CLAIM-PAYMENT-IDOR-01",
  },
  CONFLICT: {
    code: "CONFLICT",
    http_status: 409,
    title_id: "Terjadi Benturan Data",
    message_id: "Data duplikat (mis. NIK sudah terdaftar) atau slot kode unik pembayaran penuh sementara.",
    user_action_id: "Minta anggota memeriksa data pendaftaran atau tunggu transaksi lain kedaluwarsa sebelum mencoba ulang.",
    retryable: false,
    retry_strategy: "manual_user_retry",
    ui_pattern: "warning_banner",
    related_claim_id: "CLAIM-PENDAFTARAN-KYC-01",
  },
  PAYLOAD_TOO_LARGE: {
    code: "PAYLOAD_TOO_LARGE",
    http_status: 413,
    title_id: "Ukuran Berkas Terlalu Besar",
    message_id: "Ukuran permintaan atau foto dokumen melebihi batas maksimal server (bawaan 5 MB KYC / 10 MB payload).",
    user_action_id: "Kompres foto di browser sebelum mengirim atau pilih berkas yang lebih kecil.",
    retryable: false,
    retry_strategy: "compress_then_retry",
    ui_pattern: "inline_file_error",
    related_claim_id: "CLAIM-HTTP-HARDENING-01",
  },
  IDEMPOTENCY_CONFLICT: {
    code: "IDEMPOTENCY_CONFLICT",
    http_status: 422,
    title_id: "Kunci Transaksi Tidak Cocok Dengan Isi Baru",
    message_id: "Header Idempotency-Key yang sama dikirim ulang untuk isi permintaan pembayaran yang berbeda.",
    user_action_id: "Hasilkan UUID Idempotency-Key baru (crypto.randomUUID()) setiap kali anggota mengubah item/nominal.",
    retryable: false,
    retry_strategy: "regenerate_idempotency_key",
    ui_pattern: "toast_warning",
    related_claim_id: "CLAIM-QRIS-IDEMPOTENCY-01",
  },
  TOO_MANY_REQUESTS: {
    code: "TOO_MANY_REQUESTS",
    http_status: 429,
    title_id: "Terlalu Banyak Permintaan",
    message_id: "Batas laju permintaan atau percobaan masuk gagal telah tercapai sementara.",
    user_action_id: "Nonaktifkan tombol sementara, baca header Retry-After, dan tampilkan hitung mundur sebelum coba lagi.",
    retryable: true,
    retry_strategy: "respect_retry_after_backoff",
    ui_pattern: "countdown_cooldown_banner",
    related_claim_id: "CLAIM-AUTH-BRUTEFORCE-429",
  },
  MFA_SESSION_LOCKED: {
    code: "MFA_SESSION_LOCKED",
    http_status: 429,
    title_id: "Sesi Verifikasi MFA Terkunci",
    message_id: "Terlalu banyak kode TOTP atau kode pemulihan yang salah pada sesi login ini.",
    user_action_id: "Hapus mfa_session_token saat ini dan minta pengguna mengulang proses masuk dari email & kata sandi.",
    retryable: false,
    retry_strategy: "restart_login_flow",
    ui_pattern: "restart_login_banner",
    related_claim_id: "CLAIM-AUTH-MFA-VERIFY-01",
  },
  INTERNAL_SERVER_ERROR: {
    code: "INTERNAL_SERVER_ERROR",
    http_status: 500,
    title_id: "Gangguan Server Sementara",
    message_id: "Terjadi kendala internal pada server kami. Data Anda tetap aman.",
    user_action_id: "Tampilkan X-Request-ID sebagai nomor rujukan ke admin teknis dan sediakan tombol Muat Ulang untuk GET.",
    retryable: true,
    retry_strategy: "safe_idempotent_retry",
    ui_pattern: "fallback_error_with_request_id",
    related_claim_id: "CLAIM-HTTP-HARDENING-01",
  },
  DATABASE_UNAVAILABLE: {
    code: "DATABASE_UNAVAILABLE",
    http_status: 503,
    title_id: "Basis Data Sedang Tidak Tersedia",
    message_id: "Layanan basis data koperasi sedang memulihkan koneksi.",
    user_action_id: "Tampilkan status layanan sedang pulih dan ulangi permintaan GET secara berkala.",
    retryable: true,
    retry_strategy: "respect_retry_after_backoff",
    ui_pattern: "maintenance_banner",
    related_claim_id: "CLAIM-HEALTH-01",
  },
  READ_ONLY_MODE: {
    code: "READ_ONLY_MODE",
    http_status: 503,
    title_id: "Mode Baca-Saja (Pemeliharaan)",
    message_id: "Sistem koperasi sedang dalam mode baca-saja sementara; Anda masih bisa melihat data namun belum bisa menyimpan perubahan.",
    user_action_id: "Tampilkan banner pemeliharaan permanen dari GET /api/v1/public/config dan nonaktifkan tombol mutasi.",
    retryable: false,
    retry_strategy: "none",
    ui_pattern: "global_readonly_banner",
    related_claim_id: "CLAIM-PUBLIC-CONFIG-01",
  },
  FEATURE_DISABLED: {
    code: "FEATURE_DISABLED",
    http_status: 503,
    title_id: "Fitur Dinonaktifkan Sementara",
    message_id: "Fitur ini (login, pendaftaran, atau pembayaran) sedang ditutup sementara oleh pengelola koperasi.",
    user_action_id: "Periksa features.* pada GET /api/v1/public/config untuk menyembunyikan atau menonaktifkan alur terkait secara halus.",
    retryable: false,
    retry_strategy: "none",
    ui_pattern: "feature_unavailable_notice",
    related_claim_id: "CLAIM-PUBLIC-CONFIG-01",
  },
};

export interface ApiErrorParams {
  status: number;
  code: string;
  message: string;
  details?: unknown;
  requestId?: string;
  retryAfterSeconds?: number;
  data?: unknown;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: unknown;
  readonly requestId?: string;
  readonly retryAfterSeconds?: number;
  readonly data?: unknown;

  constructor(params: ApiErrorParams) {
    super(params.message);
    this.name = "ApiError";
    this.status = params.status;
    this.code = params.code;
    this.details = params.details;
    this.requestId = params.requestId;
    this.retryAfterSeconds = params.retryAfterSeconds;
    this.data = params.data;
  }
}

export function getErrorPresentation(err: ApiError): {
  title: string;
  message: string;
  action: string;
  uiPattern: ErrorUiPattern;
  requestId?: string;
} {
  const entry = ERROR_CATALOG_ID[err.code];
  if (!entry) {
    return {
      title: "Terjadi Kesalahan",
      message: err.message || "Permintaan belum dapat diproses.",
      action: "Silakan periksa kembali isian atau ulangi beberapa saat lagi.",
      uiPattern: "fallback_error_with_request_id",
      requestId: err.requestId,
    };
  }
  return {
    title: entry.title_id,
    message: err.message || entry.message_id,
    action: entry.user_action_id,
    uiPattern: entry.ui_pattern,
    requestId: err.requestId,
  };
}

export function isRetryableError(err: unknown, method = "GET"): boolean {
  if (!(err instanceof ApiError)) return false;
  const upperMethod = method.toUpperCase();
  if (err.code === "TOO_MANY_REQUESTS") return true;
  if (err.code === "DATABASE_UNAVAILABLE" && upperMethod === "GET") return true;
  if (err.code === "INTERNAL_SERVER_ERROR" && upperMethod === "GET") return true;
  return false;
}
