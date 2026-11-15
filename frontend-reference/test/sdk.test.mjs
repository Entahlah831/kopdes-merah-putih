import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  createClient,
  fetchPublicConfig,
  validatePublicConfig,
  computeClockSkewMs,
  isFeatureEnabled,
  formatRupiah,
  parseRupiahInput,
  formatJakartaDateTime,
  getJakartaTodayISO,
  maskNIK,
  validateUploadFile,
  createPaymentPoller,
  ApiError,
  ERROR_CATALOG_ID,
  getErrorPresentation,
  AuthManager,
} from "../dist/index.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "../..");

function makeSampleConfig(overrides = {}) {
  return {
    api_version: "1.1.0",
    server_time: "2026-11-15T11:00:00+07:00",
    server_unix_ms: 1794715200000,
    timezone: "Asia/Jakarta",
    upload_limits: {
      max_body_bytes: 10485760,
      max_kyc_upload_bytes: 5242880,
      allowed_mime_types: ["image/jpeg", "image/png", "application/pdf"],
      allowed_csv_mime_types: ["text/csv", "application/csv", "text/plain"],
    },
    kyc_required_document_types: ["ktp"],
    kyc_supported_document_types: ["ktp", "kk", "foto_diri", "surat_domisili"],
    payment_expiry_seconds: 900,
    polling_interval_ms: 3000,
    features: {
      payments_enabled: true,
      registration_enabled: true,
      login_enabled: true,
      read_only_mode: false,
      qris_dynamic_enabled: false,
      personal_payment_pilot_enabled: false,
    },
    captcha: {
      enabled: false,
      provider: "turnstile",
      site_key: "",
    },
    mfa_mandatory_roles: ["admin", "bendahara", "ketua"],
    access_token_ttl_seconds: 900,
    pagination: {
      default_limit: 20,
      max_limit: 100,
    },
    ...overrides,
  };
}

function jsonResp(status, body, headers = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

test("ERROR_CATALOG_ID cocok persis 15 kode error dengan docs/frontend/pesan-error-id.json", () => {
  const catalogPath = path.join(repoRoot, "docs", "frontend", "pesan-error-id.json");
  const raw = JSON.parse(fs.readFileSync(catalogPath, "utf8"));
  const docCodes = Object.keys(raw.errors).sort();
  const sdkCodes = Object.keys(ERROR_CATALOG_ID).sort();
  assert.deepEqual(sdkCodes, docCodes);
  assert.equal(sdkCodes.length, 15);
});

test("formatRupiah, parseRupiahInput, getJakartaTodayISO, dan maskNIK bekerja akurat", () => {
  assert.equal(formatRupiah(600356), "Rp 600.356");
  assert.equal(formatRupiah("600356.00"), "Rp 600.356");
  assert.equal(parseRupiahInput("Rp 25.000"), 25000);
  // 2026-11-14T17:30:00Z adalah 2026-11-15 00:30 WIB di Asia/Jakarta!
  const utcLateMs = Date.parse("2026-11-14T17:30:00Z");
  assert.equal(getJakartaTodayISO(utcLateMs), "2026-11-15");
  assert.match(formatJakartaDateTime("2026-11-15T04:15:00Z"), /WIB$/);
  assert.equal(maskNIK("3201010101900001"), "3201********0001");
});

test("fetchPublicConfig, validasi skema, clock skew, dan isFeatureEnabled", async () => {
  const cfgPayload = makeSampleConfig();
  const clientFetch = async (url) => {
    assert.equal(url, "https://api.kopdes-contoh.example/api/v1/public/config");
    return jsonResp(200, { success: true, data: cfgPayload });
  };
  const cfg = await fetchPublicConfig("https://api.kopdes-contoh.example", {
    fetchImpl: clientFetch,
  });
  assert.deepEqual(validatePublicConfig(cfg), []);
  assert.equal(isFeatureEnabled(cfg, "payments"), true);
  assert.equal(
    isFeatureEnabled(makeSampleConfig({ features: { ...cfgPayload.features, read_only_mode: true } }), "payments"),
    false
  );
  assert.equal(computeClockSkewMs(1000000, 995000), 5000);
});

test("AuthManager menangani 3 cabang login + guard setup_token + BroadcastChannel logout", async () => {
  const requests = [];
  const fakeFetch = async (url, init = {}) => {
    requests.push({ url, method: init.method, headers: Object.fromEntries(new Headers(init.headers).entries()) });
    if (url.endsWith("/api/v1/auth/login")) {
      const body = JSON.parse(init.body);
      if (body.email === "ketua@kopdes.example") {
        return jsonResp(403, {
          success: false,
          data: { mfa_required: true, mfa_setup_required: true, setup_token: "setup-jwt-only" },
          error: { code: "MFA_MANDATORY_SETUP_REQUIRED", message: "Wajib MFA" },
        });
      }
      if (body.email === "mfa@kopdes.example") {
        return jsonResp(200, {
          success: true,
          data: { mfa_required: true, mfa_session_token: "mfa-sess-1" },
        });
      }
      return jsonResp(200, {
        success: true,
        data: {
          access_token: "acc-1",
          refresh_token: "ref-1",
          expires_in: 900,
          user_id: "u1",
          koperasi_id: "k1",
          nama_lengkap: "Budi",
          role: "anggota",
        },
      });
    }
    if (url.endsWith("/api/v1/auth/mfa/setup")) {
      return jsonResp(200, { success: true, data: { secret: "BASE32", qr_uri: "otpauth://totp/x" } });
    }
    if (url.endsWith("/api/v1/auth/mfa/verify")) {
      return jsonResp(429, {
        success: false,
        error: { code: "MFA_SESSION_LOCKED", message: "Sesi MFA terkunci" },
      });
    }
    if (url.endsWith("/api/v1/auth/revoke")) {
      return jsonResp(200, { success: true, data: { message: "revoked" } });
    }
    return jsonResp(404, { success: false, error: { code: "NOT_FOUND", message: "none" } });
  };

  // 1. Setup token guard: ditolak ke /api/v1/auth/me, diizinkan ke /mfa/setup
  const client = createClient({
    baseUrl: "https://api.kopdes-contoh.example",
    fetchImpl: fakeFetch,
  });
  const outSetup = await client.auth.login("ketua@kopdes.example", "pass");
  assert.equal(outSetup.status, "mfa_setup_required");
  await assert.rejects(
    () => client.request("/api/v1/auth/me"),
    (err) => err instanceof ApiError && err.code === "MFA_ENROLLMENT_REQUIRED"
  );
  const setupRes = await client.auth.setupMFA();
  assert.equal(setupRes.secret, "BASE32");

  // 2. MFA Challenge -> 429 MFA_SESSION_LOCKED membersihkan mfaSessionToken
  const outMfa = await client.auth.login("mfa@kopdes.example", "pass");
  assert.equal(outMfa.status, "mfa_challenge");
  await assert.rejects(
    () => client.auth.verifyMFA({ totpCode: "000000" }),
    (err) => err instanceof ApiError && err.code === "MFA_SESSION_LOCKED" && err.status === 429
  );
  assert.equal(client.auth.getMFASessionToken(), null);

  // 3. BroadcastChannel logout menyinkronkan tab kedua tanpa menyebarkan token
  let peerLoggedOut = false;
  const tabA = new AuthManager({
    baseUrl: "https://api.kopdes-contoh.example",
    fetchImpl: fakeFetch,
    channelName: "kopdes_test_chan",
  });
  const tabB = new AuthManager({
    baseUrl: "https://api.kopdes-contoh.example",
    fetchImpl: fakeFetch,
    channelName: "kopdes_test_chan",
    onForceLogout: () => {
      peerLoggedOut = true;
    },
  });
  await tabA.login("anggota@kopdes.example", "pass");
  tabB.setTokensForSession({ accessToken: "acc-b", refreshToken: "ref-b" });
  await tabA.logout();
  await new Promise((r) => setTimeout(r, 40));
  assert.equal(tabA.getAccessToken(), null);
  assert.equal(tabB.getAccessToken(), null);
  assert.equal(peerLoggedOut, true);
  tabA.destroy();
  tabB.destroy();
  client.auth.destroy();
});

test("Single-flight refresh: 3 panggilan paralel 401 hanya memicu 1 POST /auth/refresh", async () => {
  let refreshCount = 0;
  let currentValidAccess = "acc-v1";
  const fakeFetch = async (url, init = {}) => {
    const auth = new Headers(init.headers).get("Authorization");
    if (url.endsWith("/api/v1/auth/refresh")) {
      refreshCount += 1;
      await new Promise((r) => setTimeout(r, 20));
      currentValidAccess = "acc-v2";
      return jsonResp(200, {
        success: true,
        data: { access_token: "acc-v2", refresh_token: "ref-v2" },
      });
    }
    if (auth !== `Bearer ${currentValidAccess}`) {
      return jsonResp(401, {
        success: false,
        error: { code: "UNAUTHORIZED", message: "Token kedaluwarsa" },
      });
    }
    return jsonResp(200, { success: true, data: { ok: true, path: new URL(url).pathname } });
  };

  const client = createClient({
    baseUrl: "https://api.kopdes-contoh.example",
    fetchImpl: fakeFetch,
  });
  client.auth.setTokensForSession({ accessToken: "acc-expired", refreshToken: "ref-v1" });

  const [a, b, c] = await Promise.all([
    client.request("/api/v1/auth/me"),
    client.request("/api/v1/iuran/tagihan/saya"),
    client.request("/api/v1/toko/pesanan"),
  ]);

  assert.equal(refreshCount, 1);
  assert.equal(a.ok && b.ok && c.ok, true);
  assert.equal(client.auth.getAccessToken(), "acc-v2");
  client.auth.destroy();
});

test("Refresh gagal / token reuse memicu force logout otomatis", async () => {
  let forcedReason = "";
  const fakeFetch = async (url) => {
    if (url.endsWith("/api/v1/auth/refresh")) {
      return jsonResp(401, {
        success: false,
        error: { code: "UNAUTHORIZED", message: "Refresh token reuse terdeteksi" },
      });
    }
    return jsonResp(401, {
      success: false,
      error: { code: "UNAUTHORIZED", message: "Expired" },
    });
  };
  const client = createClient({
    baseUrl: "https://api.kopdes-contoh.example",
    fetchImpl: fakeFetch,
    onForceLogout: (r) => {
      forcedReason = r;
    },
  });
  client.auth.setTokensForSession({ accessToken: "old", refreshToken: "reused" });
  await assert.rejects(() => client.request("/api/v1/auth/me"), (err) => err instanceof ApiError);
  assert.equal(forcedReason, "refresh_rejected_or_reuse");
  assert.equal(client.auth.getAccessToken(), null);
  client.auth.destroy();
});

test("Retry-After pada 429, X-Request-ID, Idempotency-Key, dan presentasi pesan ID", async () => {
  let calls = 0;
  let capturedIdempKey = "";
  const fakeFetch = async (url, init = {}) => {
    calls += 1;
    capturedIdempKey = new Headers(init.headers).get("Idempotency-Key") ?? "";
    if (calls === 1) {
      return jsonResp(
        429,
        { success: false, error: { code: "TOO_MANY_REQUESTS", message: "Rate limit" } },
        { "Retry-After": "0", "X-Request-ID": "req-429-1" }
      );
    }
    return jsonResp(201, { success: true, data: { pembayaran_id: "p1", kode_unik: 123 } });
  };

  const client = createClient({
    baseUrl: "https://api.kopdes-contoh.example",
    fetchImpl: fakeFetch,
  });
  const qr = await client.createQRISPayment(
    { jenis_transaksi: "iuran_bulanan", tagihan_iuran_id: ["t1"] },
    "11111111-1111-4111-8111-111111111111"
  );
  assert.equal(calls, 2);
  assert.equal(qr.kode_unik, 123);
  assert.equal(capturedIdempKey, "11111111-1111-4111-8111-111111111111");

  const apiErr = new ApiError({
    status: 503,
    code: "READ_ONLY_MODE",
    message: "Maintenance",
    requestId: "req-xyz",
  });
  const ui = getErrorPresentation(apiErr);
  assert.equal(ui.uiPattern, "global_readonly_banner");
  assert.equal(ui.requestId, "req-xyz");
  client.auth.destroy();
});

test("validateUploadFile dan createPaymentPoller bekerja sesuai aturan", async () => {
  assert.equal(
    validateUploadFile(
      { name: "ktp.jpg", size: 2048, type: "image/jpeg" },
      { maxBytes: 5242880, allowedMimeTypes: ["image/jpeg", "image/png", "application/pdf"] }
    ).ok,
    true
  );
  assert.equal(
    validateUploadFile(
      { name: "ktp.webp", size: 2048, type: "image/webp" },
      { maxBytes: 5242880, allowedMimeTypes: ["image/jpeg", "image/png", "application/pdf"] }
    ).ok,
    false
  );

  let ticks = 0;
  let lastStatus = "";
  await new Promise((resolve) => {
    const poller = createPaymentPoller({
      minIntervalOverrideMs: 10,
      intervalMs: 10,
      fetchStatus: async () => {
        ticks += 1;
        return { status: ticks >= 2 ? "disetujui" : "menunggu_verifikasi" };
      },
      onUpdate: (data) => {
        lastStatus = data.status;
        if (data.status === "disetujui") {
          setTimeout(resolve, 15);
        }
      },
    });
    poller.start();
  });
  assert.equal(ticks, 2);
  assert.equal(lastStatus, "disetujui");
});

test("Live integration check saat API_PUBLIC_URL disediakan di env", async (t) => {
  const liveUrl = process.env.API_PUBLIC_URL;
  if (!liveUrl) {
    t.skip("API_PUBLIC_URL tidak diset; tes live terhadap Go server dilewati pada mode offline murni.");
    return;
  }
  const cfg = await fetchPublicConfig(liveUrl);
  assert.equal(cfg.timezone, "Asia/Jakarta");
  assert.deepEqual(validatePublicConfig(cfg), []);
});
