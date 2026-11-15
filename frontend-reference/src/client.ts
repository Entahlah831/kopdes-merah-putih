import {
  AuthManager,
  MFA_SETUP_ALLOWED_PATHS,
  parseRetryAfterSeconds,
  safeJson,
} from "./auth.js";
import {
  fetchPublicConfig,
  normalizeBaseUrl,
  type PublicConfig,
} from "./config.js";
import { ApiError } from "./errors.js";
import { uploadMultipartWithProgress } from "./upload.js";

export interface KopdesClientOptions {
  baseUrl: string;
  publicConfig?: PublicConfig | null;
  fetchImpl?: typeof fetch;
  requestTimeoutMs?: number;
  maxRateLimitRetries?: number;
  onForceLogout?: (reason: string) => void;
  onConfigChange?: (config: PublicConfig) => void;
}

export interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  query?: Record<string, string | number | boolean | undefined>;
  headers?: Record<string, string>;
  body?: unknown;
  idempotencyKey?: string;
  signal?: AbortSignal;
  timeoutMs?: number;
}

interface ApiEnvelope<T> {
  success?: boolean;
  data?: T;
  meta?: unknown;
  error?: { code?: string; message?: string; details?: unknown };
}

export class KopdesClient {
  readonly baseUrl: string;
  readonly auth: AuthManager;
  private readonly fetcher: typeof fetch;
  private readonly defaultTimeoutMs: number;
  private readonly maxRateLimitRetries: number;
  private publicConfig: PublicConfig | null;
  private readonly onConfigChange?: (cfg: PublicConfig) => void;

  constructor(options: KopdesClientOptions) {
    this.baseUrl = normalizeBaseUrl(options.baseUrl);
    this.fetcher = options.fetchImpl ?? globalThis.fetch;
    this.defaultTimeoutMs = options.requestTimeoutMs ?? 15000;
    this.maxRateLimitRetries = options.maxRateLimitRetries ?? 2;
    this.publicConfig = options.publicConfig ?? null;
    this.onConfigChange = options.onConfigChange;
    this.auth = new AuthManager({
      baseUrl: this.baseUrl,
      fetchImpl: this.fetcher,
      onForceLogout: options.onForceLogout,
    });
  }

  getPublicConfig(): PublicConfig | null {
    return this.publicConfig;
  }

  async loadPublicConfig(cache: RequestCache = "default", signal?: AbortSignal): Promise<PublicConfig> {
    const cfg = await fetchPublicConfig(this.baseUrl, {
      cache,
      signal,
      fetchImpl: this.fetcher,
    });
    this.publicConfig = cfg;
    this.onConfigChange?.(cfg);
    return cfg;
  }

  newIdempotencyKey(): string {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return crypto.randomUUID();
    }
    return `idemp-${Date.now()}-${Math.random().toString(16).slice(2, 10)}`;
  }

  async createQRISPayment<T = unknown>(
    payload: {
      jenis_transaksi: "iuran_bulanan" | "belanja_toko" | string;
      tagihan_iuran_id?: string[];
      pesanan_id?: string;
    },
    idempotencyKey?: string,
    signal?: AbortSignal
  ): Promise<T> {
    return this.request<T>("/api/v1/pembayaran/qris", {
      method: "POST",
      idempotencyKey: idempotencyKey || this.newIdempotencyKey(),
      body: payload,
      signal,
    });
  }

  async uploadMultipart<T = unknown>(
    path: string,
    formData: FormData,
    opts: { onProgress?: (pct: number) => void; signal?: AbortSignal } = {}
  ): Promise<T> {
    this.assertAllowedForSetupToken(path);
    const fullUrl = `${this.baseUrl}${path}`;
    const res = await uploadMultipartWithProgress(fullUrl, formData, {
      accessToken: this.auth.getAccessToken(),
      onProgress: opts.onProgress,
      signal: opts.signal,
      fetchImpl: this.fetcher,
    });
    return this.unwrapEnvelope<T>(res);
  }

  async request<T = unknown>(path: string, opts: RequestOptions = {}): Promise<T> {
    const method = (opts.method ?? "GET").toUpperCase();
    this.assertAllowedForSetupToken(path);

    let url = `${this.baseUrl}${path}`;
    if (opts.query) {
      const qs = new URLSearchParams();
      for (const [k, v] of Object.entries(opts.query)) {
        if (v !== undefined && v !== "") qs.set(k, String(v));
      }
      const queryStr = qs.toString();
      if (queryStr) url += `?${queryStr}`;
    }

    const noAutoRetryRateLimit =
      path.startsWith("/api/v1/auth/login") || path.startsWith("/api/v1/auth/mfa/verify");

    let rateAttempt = 0;
    let refreshedOnce = false;

    while (true) {
      const headers = new Headers(opts.headers ?? {});
      const token = this.auth.getAccessToken();
      if (token) {
        headers.set("Authorization", `Bearer ${token}`);
      }
      if (opts.idempotencyKey) {
        headers.set("Idempotency-Key", opts.idempotencyKey);
      }
      let serializedBody: string | undefined;
      if (opts.body !== undefined) {
        if (!headers.has("Content-Type")) {
          headers.set("Content-Type", "application/json");
        }
        serializedBody = typeof opts.body === "string" ? opts.body : JSON.stringify(opts.body);
      }

      const timeoutMs = opts.timeoutMs ?? this.defaultTimeoutMs;
      const controller = new AbortController();
      const timeoutHandle = setTimeout(() => controller.abort(), timeoutMs);
      const onCallerAbort = () => controller.abort();
      if (opts.signal) {
        if (opts.signal.aborted) {
          clearTimeout(timeoutHandle);
          throw new DOMException("Aborted", "AbortError");
        }
        opts.signal.addEventListener("abort", onCallerAbort, { once: true });
      }

      let res: Response;
      try {
        res = await this.fetcher(url, {
          method,
          headers,
          body: serializedBody,
          signal: controller.signal,
        });
      } finally {
        clearTimeout(timeoutHandle);
        opts.signal?.removeEventListener("abort", onCallerAbort);
      }

      if (
        res.status === 401 &&
        !refreshedOnce &&
        !path.startsWith("/api/v1/auth/login") &&
        !path.startsWith("/api/v1/auth/refresh") &&
        !path.startsWith("/api/v1/auth/mfa/verify")
      ) {
        refreshedOnce = true;
        const newTok = await this.auth.refreshAccessTokenSingleFlight();
        if (newTok) {
          continue;
        }
      }

      if (
        (res.status === 429 || (res.status === 503 && method === "GET")) &&
        !noAutoRetryRateLimit &&
        rateAttempt < this.maxRateLimitRetries
      ) {
        const peekJson = (await safeJson(res.clone())) as ApiEnvelope<unknown> | null;
        const errCode = peekJson?.error?.code;
        if (errCode !== "MFA_SESSION_LOCKED" && errCode !== "READ_ONLY_MODE" && errCode !== "FEATURE_DISABLED") {
          rateAttempt += 1;
          const retryAfter = parseRetryAfterSeconds(res.headers?.get?.("Retry-After"));
          const waitMs = Math.max(100, (retryAfter ?? rateAttempt) * 1000);
          await sleepWithAbort(waitMs, opts.signal);
          continue;
        }
      }

      return this.unwrapEnvelope<T>(res);
    }
  }

  private assertAllowedForSetupToken(path: string): void {
    if (!this.auth.getAccessToken() && this.auth.getSetupToken() && !MFA_SETUP_ALLOWED_PATHS.has(path)) {
      throw new ApiError({
        status: 403,
        code: "MFA_ENROLLMENT_REQUIRED",
        message: "Selesaikan aktivasi MFA terlebih dahulu menggunakan setup_token sebelum memanggil endpoint bisnis.",
      });
    }
  }

  private async unwrapEnvelope<T>(res: Response): Promise<T> {
    const requestId = res.headers?.get?.("X-Request-ID") ?? undefined;
    const retryAfter = parseRetryAfterSeconds(res.headers?.get?.("Retry-After"));
    const json = (await safeJson(res)) as ApiEnvelope<T> | null;

    if (!res.ok || (json && json.success === false)) {
      const code = json?.error?.code ?? defaultCodeForStatus(res.status);
      if (code === "READ_ONLY_MODE" || code === "FEATURE_DISABLED") {
        // Segarkan status public config diam-diam saat server masuk mode pemeliharaan
        void this.loadPublicConfig("no-store").catch(() => undefined);
      }
      throw new ApiError({
        status: res.status || 500,
        code,
        message: json?.error?.message ?? `Permintaan gagal dengan status HTTP ${res.status}`,
        details: json?.error?.details,
        requestId,
        retryAfterSeconds: retryAfter,
        data: json?.data,
      });
    }

    if (json && "data" in json) {
      return json.data as T;
    }
    return json as unknown as T;
  }
}

export function createClient(options: KopdesClientOptions): KopdesClient {
  return new KopdesClient(options);
}

function defaultCodeForStatus(status: number): string {
  switch (status) {
    case 400:
      return "BAD_REQUEST";
    case 401:
      return "UNAUTHORIZED";
    case 403:
      return "FORBIDDEN";
    case 404:
      return "NOT_FOUND";
    case 409:
      return "CONFLICT";
    case 413:
      return "PAYLOAD_TOO_LARGE";
    case 422:
      return "IDEMPOTENCY_CONFLICT";
    case 429:
      return "TOO_MANY_REQUESTS";
    case 503:
      return "DATABASE_UNAVAILABLE";
    default:
      return "INTERNAL_SERVER_ERROR";
  }
}

function sleepWithAbort(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException("Aborted", "AbortError"));
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    };
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}
