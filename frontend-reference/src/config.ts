import { ApiError } from "./errors.js";

export interface PublicConfigUploadLimits {
  max_body_bytes: number;
  max_kyc_upload_bytes: number;
  allowed_mime_types: string[];
  allowed_csv_mime_types: string[];
}

export interface PublicConfigFeatures {
  payments_enabled: boolean;
  registration_enabled: boolean;
  login_enabled: boolean;
  read_only_mode: boolean;
  qris_dynamic_enabled: boolean;
  personal_payment_pilot_enabled: boolean;
}

export interface PublicConfigCaptcha {
  enabled: boolean;
  provider: string;
  site_key: string;
}

export interface PublicConfigPagination {
  default_limit: number;
  max_limit: number;
}

export interface PublicConfig {
  api_version: string;
  server_time: string;
  server_unix_ms: number;
  timezone: string;
  upload_limits: PublicConfigUploadLimits;
  kyc_required_document_types: string[];
  kyc_supported_document_types: string[];
  payment_expiry_seconds: number;
  polling_interval_ms: number;
  features: PublicConfigFeatures;
  captcha: PublicConfigCaptcha;
  mfa_mandatory_roles: string[];
  access_token_ttl_seconds: number;
  pagination: PublicConfigPagination;
}

export interface FetchPublicConfigOptions {
  signal?: AbortSignal;
  cache?: RequestCache;
  fetchImpl?: typeof fetch;
}

export function normalizeBaseUrl(baseUrl: string): string {
  const trimmed = String(baseUrl ?? "").trim().replace(/\/+$/, "");
  if (!trimmed) {
    throw new Error("Base URL API wajib dikonfigurasi (mis. dari VITE_API_BASE_URL atau /config.json).");
  }
  return trimmed;
}

export async function fetchPublicConfig(
  baseUrl: string,
  options: FetchPublicConfigOptions = {}
): Promise<PublicConfig> {
  const root = normalizeBaseUrl(baseUrl);
  const fetcher = options.fetchImpl ?? globalThis.fetch;
  const res = await fetcher(`${root}/api/v1/public/config`, {
    method: "GET",
    cache: options.cache ?? "default",
    signal: options.signal,
  });
  const requestId = res.headers?.get?.("X-Request-ID") ?? undefined;
  let body: { success?: boolean; data?: PublicConfig; error?: { code?: string; message?: string } } | null = null;
  try {
    body = (await res.json()) as { success?: boolean; data?: PublicConfig; error?: { code?: string; message?: string } };
  } catch {
    body = null;
  }
  if (!res.ok || !body?.success || !body.data) {
    throw new ApiError({
      status: res.status || 500,
      code: body?.error?.code ?? "INTERNAL_SERVER_ERROR",
      message: body?.error?.message ?? "Gagal memuat konfigurasi publik koperasi.",
      requestId,
    });
  }
  const issues = validatePublicConfig(body.data);
  if (issues.length > 0) {
    throw new Error(`Konfigurasi publik tidak valid: ${issues.join("; ")}`);
  }
  return body.data;
}

export function validatePublicConfig(cfg: unknown): string[] {
  const errors: string[] = [];
  if (!cfg || typeof cfg !== "object") {
    return ["payload bukan objek JSON"];
  }
  const c = cfg as Record<string, unknown>;
  if (typeof c.api_version !== "string" || !/^[0-9]+\.[0-9]+\.[0-9]+/.test(c.api_version)) {
    errors.push("api_version harus semver valid");
  }
  if (c.timezone !== "Asia/Jakarta") {
    errors.push("timezone wajib 'Asia/Jakarta'");
  }
  const ul = c.upload_limits as Record<string, unknown> | undefined;
  if (!ul || typeof ul !== "object") {
    errors.push("upload_limits wajib objek");
  } else {
    if (typeof ul.max_body_bytes !== "number" || ul.max_body_bytes < 1048576) {
      errors.push("upload_limits.max_body_bytes minimal 1048576 byte");
    }
    if (typeof ul.max_kyc_upload_bytes !== "number" || ul.max_kyc_upload_bytes < 524288) {
      errors.push("upload_limits.max_kyc_upload_bytes minimal 524288 byte");
    }
    if (!Array.isArray(ul.allowed_mime_types) || ul.allowed_mime_types.length === 0) {
      errors.push("upload_limits.allowed_mime_types wajib berisi minimal 1 tipe MIME");
    }
  }
  if (typeof c.payment_expiry_seconds !== "number" || c.payment_expiry_seconds < 60) {
    errors.push("payment_expiry_seconds minimal 60 detik");
  }
  if (typeof c.polling_interval_ms !== "number" || c.polling_interval_ms < 1000) {
    errors.push("polling_interval_ms minimal 1000 ms");
  }
  const feat = c.features as Record<string, unknown> | undefined;
  if (!feat || typeof feat !== "object") {
    errors.push("features wajib objek boolean");
  } else {
    for (const k of [
      "payments_enabled",
      "registration_enabled",
      "login_enabled",
      "read_only_mode",
      "qris_dynamic_enabled",
      "personal_payment_pilot_enabled",
    ]) {
      if (typeof feat[k] !== "boolean") {
        errors.push(`features.${k} wajib boolean`);
      }
    }
  }
  const cap = c.captcha as Record<string, unknown> | undefined;
  if (!cap || typeof cap !== "object" || typeof cap.enabled !== "boolean") {
    errors.push("captcha wajib objek dengan enabled: boolean");
  } else if (!cap.enabled && cap.site_key !== "") {
    errors.push("captcha.site_key wajib kosong saat captcha.enabled=false");
  }
  const pag = c.pagination as Record<string, unknown> | undefined;
  if (
    !pag ||
    typeof pag !== "object" ||
    typeof pag.default_limit !== "number" ||
    typeof pag.max_limit !== "number" ||
    pag.default_limit < 1 ||
    pag.default_limit > pag.max_limit
  ) {
    errors.push("pagination wajib memiliki 1 <= default_limit <= max_limit");
  }
  return errors;
}

export function computeClockSkewMs(serverUnixMs: number, clientNowMs: number = Date.now()): number {
  if (!Number.isFinite(serverUnixMs) || serverUnixMs <= 0) return 0;
  return Math.trunc(serverUnixMs - clientNowMs);
}

export function isFeatureEnabled(
  cfg: PublicConfig,
  feature: "payments" | "registration" | "login"
): boolean {
  if (cfg.features.read_only_mode) {
    return feature === "login" ? cfg.features.login_enabled : false;
  }
  switch (feature) {
    case "payments":
      return cfg.features.payments_enabled;
    case "registration":
      return cfg.features.registration_enabled;
    case "login":
      return cfg.features.login_enabled;
  }
}
