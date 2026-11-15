import { ApiError } from "./errors.js";
import { normalizeBaseUrl } from "./config.js";

export interface AuthSessionUser {
  user_id: string;
  koperasi_id: string;
  nama_lengkap: string;
  role: string;
  expires_in: number;
}

export type LoginOutcome =
  | { status: "authenticated"; session: AuthSessionUser }
  | { status: "mfa_challenge"; mfaSessionToken: string }
  | { status: "mfa_setup_required"; setupToken: string; mfaSessionToken?: string };

export interface AuthManagerOptions {
  baseUrl: string;
  fetchImpl?: typeof fetch;
  onForceLogout?: (reason: string) => void;
  channelName?: string;
}

interface Envelope<T> {
  success?: boolean;
  data?: T;
  error?: { code?: string; message?: string; details?: unknown };
}

export const MFA_SETUP_ALLOWED_PATHS = new Set<string>([
  "/api/v1/auth/mfa/setup",
  "/api/v1/auth/mfa/enable",
]);

/**
 * AuthManager menyimpan seluruh token autentikasi murni di memori JS
 * (TIDAK pernah menggunakan localStorage, sessionStorage, atau cookie JS-readable).
 */
export class AuthManager {
  private readonly baseUrl: string;
  private readonly fetcher: typeof fetch;
  private readonly onForceLogoutCallback?: (reason: string) => void;
  private readonly channel: BroadcastChannel | null = null;

  private accessToken: string | null = null;
  private refreshToken: string | null = null;
  private setupToken: string | null = null;
  private mfaSessionToken: string | null = null;
  private sessionUser: AuthSessionUser | null = null;
  private refreshPromise: Promise<string | null> | null = null;

  constructor(options: AuthManagerOptions) {
    this.baseUrl = normalizeBaseUrl(options.baseUrl);
    this.fetcher = options.fetchImpl ?? globalThis.fetch;
    this.onForceLogoutCallback = options.onForceLogout;

    if (typeof BroadcastChannel !== "undefined") {
      try {
        this.channel = new BroadcastChannel(options.channelName ?? "kopdes_auth_channel");
        this.channel.onmessage = (ev: MessageEvent) => {
          const msg = ev?.data as { type?: string; reason?: string } | undefined;
          if (msg?.type === "logout") {
            // Hanya sinkronkan pembatalan sesi; jangan pernah menerima token mentah lewat BroadcastChannel
            this.clearMemory();
            this.onForceLogoutCallback?.(msg.reason ?? "other_tab_logout");
          }
        };
      } catch {
        this.channel = null;
      }
    }
  }

  getAccessToken(): string | null {
    return this.accessToken;
  }

  getSetupToken(): string | null {
    return this.setupToken;
  }

  getMFASessionToken(): string | null {
    return this.mfaSessionToken;
  }

  getSessionUser(): AuthSessionUser | null {
    return this.sessionUser ? { ...this.sessionUser } : null;
  }

  isAuthenticated(): boolean {
    return Boolean(this.accessToken);
  }

  /** Mengizinkan pengujian token kadaluwarsa di unit test tanpa menyentuh Web Storage */
  setTokensForSession(tokens: {
    accessToken: string;
    refreshToken: string;
    user?: AuthSessionUser;
  }): void {
    this.accessToken = tokens.accessToken;
    this.refreshToken = tokens.refreshToken;
    this.setupToken = null;
    this.mfaSessionToken = null;
    if (tokens.user) {
      this.sessionUser = { ...tokens.user };
    }
  }

  clearMemory(): void {
    this.accessToken = null;
    this.refreshToken = null;
    this.setupToken = null;
    this.mfaSessionToken = null;
    this.sessionUser = null;
  }

  destroy(): void {
    if (this.channel) {
      this.channel.close();
    }
  }

  async login(email: string, password: string, signal?: AbortSignal): Promise<LoginOutcome> {
    const res = await this.fetcher(`${this.baseUrl}/api/v1/auth/login`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, password }),
      signal,
    });
    const requestId = res.headers?.get?.("X-Request-ID") ?? undefined;
    const retryAfter = parseRetryAfterSeconds(res.headers?.get?.("Retry-After"));
    const json = (await safeJson(res)) as Envelope<{
      access_token?: string;
      refresh_token?: string;
      expires_in?: number;
      user_id?: string;
      koperasi_id?: string;
      nama_lengkap?: string;
      role?: string;
      mfa_required?: boolean;
      mfa_session_token?: string;
      mfa_setup_required?: boolean;
      setup_token?: string;
    }>;

    if (res.status === 403 && json?.error?.code === "MFA_MANDATORY_SETUP_REQUIRED") {
      const setupTok = json?.data?.setup_token ?? "";
      this.clearMemory();
      this.setupToken = setupTok;
      return { status: "mfa_setup_required", setupToken: setupTok };
    }

    if (!res.ok || !json?.success || !json.data) {
      throw new ApiError({
        status: res.status || 500,
        code: json?.error?.code ?? "UNAUTHORIZED",
        message: json?.error?.message ?? "Gagal masuk ke sistem.",
        details: json?.error?.details,
        requestId,
        retryAfterSeconds: retryAfter,
        data: json?.data,
      });
    }

    if (json.data.mfa_required && json.data.mfa_session_token) {
      this.accessToken = null;
      this.refreshToken = null;
      this.setupToken = null;
      this.mfaSessionToken = json.data.mfa_session_token;
      return { status: "mfa_challenge", mfaSessionToken: json.data.mfa_session_token };
    }

    const session: AuthSessionUser = {
      user_id: json.data.user_id ?? "",
      koperasi_id: json.data.koperasi_id ?? "",
      nama_lengkap: json.data.nama_lengkap ?? "",
      role: json.data.role ?? "",
      expires_in: json.data.expires_in ?? 900,
    };
    this.setTokensForSession({
      accessToken: json.data.access_token ?? "",
      refreshToken: json.data.refresh_token ?? "",
      user: session,
    });
    return { status: "authenticated", session };
  }

  async verifyMFA(
    params: { totpCode?: string; recoveryCode?: string; mfaSessionToken?: string },
    signal?: AbortSignal
  ): Promise<AuthSessionUser> {
    const token = params.mfaSessionToken ?? this.mfaSessionToken;
    if (!token) {
      throw new Error("mfa_session_token tidak tersedia; silakan login ulang terlebih dahulu.");
    }
    const res = await this.fetcher(`${this.baseUrl}/api/v1/auth/mfa/verify`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        mfa_session_token: token,
        totp_code: params.totpCode ?? "",
        recovery_code: params.recoveryCode ?? "",
      }),
      signal,
    });
    const requestId = res.headers?.get?.("X-Request-ID") ?? undefined;
    const retryAfter = parseRetryAfterSeconds(res.headers?.get?.("Retry-After"));
    const json = (await safeJson(res)) as Envelope<{
      access_token?: string;
      refresh_token?: string;
      expires_in?: number;
      user_id?: string;
      koperasi_id?: string;
      nama_lengkap?: string;
      role?: string;
    }>;

    if (!res.ok || !json?.success || !json.data) {
      if (json?.error?.code === "MFA_SESSION_LOCKED") {
        this.mfaSessionToken = null;
      }
      throw new ApiError({
        status: res.status || 401,
        code: json?.error?.code ?? "UNAUTHORIZED",
        message: json?.error?.message ?? "Verifikasi MFA gagal.",
        requestId,
        retryAfterSeconds: retryAfter,
      });
    }

    const session: AuthSessionUser = {
      user_id: json.data.user_id ?? "",
      koperasi_id: json.data.koperasi_id ?? "",
      nama_lengkap: json.data.nama_lengkap ?? "",
      role: json.data.role ?? "",
      expires_in: json.data.expires_in ?? 900,
    };
    this.setTokensForSession({
      accessToken: json.data.access_token ?? "",
      refreshToken: json.data.refresh_token ?? "",
      user: session,
    });
    return session;
  }

  async setupMFA(signal?: AbortSignal): Promise<{ secret: string; qr_uri: string }> {
    const tok = this.setupToken || this.accessToken;
    if (!tok) {
      throw new Error("Token sesi atau setup_token MFA tidak tersedia.");
    }
    const res = await this.fetcher(`${this.baseUrl}/api/v1/auth/mfa/setup`, {
      method: "POST",
      headers: { Authorization: `Bearer ${tok}` },
      signal,
    });
    const json = (await safeJson(res)) as Envelope<{ secret: string; qr_uri: string }>;
    if (!res.ok || !json?.success || !json.data) {
      throw new ApiError({
        status: res.status || 401,
        code: json?.error?.code ?? "UNAUTHORIZED",
        message: json?.error?.message ?? "Gagal menyiapkan setup MFA.",
      });
    }
    return json.data;
  }

  async enableMFA(
    secret: string,
    totpCode: string,
    signal?: AbortSignal
  ): Promise<{ recovery_codes?: string[]; message?: string }> {
    const tok = this.setupToken || this.accessToken;
    if (!tok) {
      throw new Error("Token sesi atau setup_token MFA tidak tersedia.");
    }
    const res = await this.fetcher(`${this.baseUrl}/api/v1/auth/mfa/enable`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${tok}`,
      },
      body: JSON.stringify({ secret, totp_code: totpCode }),
      signal,
    });
    const json = (await safeJson(res)) as Envelope<{ recovery_codes?: string[]; message?: string }>;
    if (!res.ok || !json?.success || !json.data) {
      throw new ApiError({
        status: res.status || 400,
        code: json?.error?.code ?? "BAD_REQUEST",
        message: json?.error?.message ?? "Gagal mengaktifkan MFA.",
      });
    }
    if (this.setupToken) {
      // Enrollment via setup_token selesai; minta login ulang untuk mendapat access_token penuh
      this.setupToken = null;
    }
    return json.data;
  }

  /**
   * Single-flight refresh: menjamin hanya 1 request POST /api/v1/auth/refresh
   * dikirim meski banyak panggilan API menerima HTTP 401 secara bersamaan.
   */
  async refreshAccessTokenSingleFlight(): Promise<string | null> {
    if (!this.refreshToken) {
      this.forceLogout("missing_refresh_token");
      return null;
    }
    if (this.refreshPromise) {
      return this.refreshPromise;
    }

    this.refreshPromise = (async () => {
      const rt = this.refreshToken;
      if (!rt) return null;
      try {
        const res = await this.fetcher(`${this.baseUrl}/api/v1/auth/refresh`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ refresh_token: rt }),
        });
        const json = (await safeJson(res)) as Envelope<{
          access_token?: string;
          refresh_token?: string;
        }>;
        if (!res.ok || !json?.success || !json.data?.access_token) {
          // Kegagalan refresh (termasuk deteksi token reuse!) mencabut sesi seketika
          this.forceLogout("refresh_rejected_or_reuse");
          return null;
        }
        this.accessToken = json.data.access_token;
        if (json.data.refresh_token) {
          this.refreshToken = json.data.refresh_token;
        }
        return this.accessToken;
      } catch {
        this.forceLogout("refresh_network_error");
        return null;
      } finally {
        this.refreshPromise = null;
      }
    })();

    return this.refreshPromise;
  }

  async logout(): Promise<void> {
    const rt = this.refreshToken;
    this.clearMemory();
    if (rt) {
      try {
        await this.fetcher(`${this.baseUrl}/api/v1/auth/revoke`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ refresh_token: rt }),
        });
      } catch {
        // Best effort revoke; sesi lokal sudah dibersihkan
      }
    }
    this.broadcastLogout("user_logout");
  }

  private forceLogout(reason: string): void {
    this.clearMemory();
    this.broadcastLogout(reason);
    this.onForceLogoutCallback?.(reason);
  }

  private broadcastLogout(reason: string): void {
    if (this.channel) {
      try {
        this.channel.postMessage({ type: "logout", reason });
      } catch {
        // Abaikan jika channel sudah ditutup
      }
    }
  }
}

export async function safeJson(res: Response): Promise<unknown> {
  try {
    return await res.json();
  } catch {
    return null;
  }
}

export function parseRetryAfterSeconds(raw: string | null | undefined): number | undefined {
  if (!raw) return undefined;
  const trimmed = raw.trim();
  const secs = Number.parseInt(trimmed, 10);
  if (Number.isFinite(secs) && secs >= 0) return secs;
  const dateMs = Date.parse(trimmed);
  if (!Number.isNaN(dateMs)) {
    return Math.max(0, Math.ceil((dateMs - Date.now()) / 1000));
  }
  return undefined;
}
