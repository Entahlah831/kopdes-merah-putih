"use strict";

(function () {
  const state = {
    uiConfig: null,
    publicConfig: null,
    openapi: null,
    rolesMatrix: null,
    scenarios: null,
    activeSession: null, // { roleKey, role, email, user_id, anggota_id, scope, session_token }
    roleCache: {}        // { [cacheKey]: session_token } in-memory only
  };

  function el(id) {
    return document.getElementById(id);
  }

  function randomDigits(len) {
    const arr = new Uint32Array(len);
    window.crypto.getRandomValues(arr);
    let out = "";
    for (let i = 0; i < len; i++) {
      out += String(arr[i] % 10);
    }
    return out;
  }

  function randomUUID() {
    if (window.crypto && typeof window.crypto.randomUUID === "function") {
      return window.crypto.randomUUID();
    }
    return "00000000-0000-4000-8000-" + randomDigits(12);
  }

  // Generate a 67-byte valid 1x1 RGBA PNG with random pixel bytes so sha256 differs every run.
  const crcTable = (function () {
    const tbl = new Uint32Array(256);
    for (let n = 0; n < 256; n++) {
      let c = n;
      for (let k = 0; k < 8; k++) {
        c = (c & 1) ? (0xedb88320 ^ (c >>> 1)) : (c >>> 1);
      }
      tbl[n] = c >>> 0;
    }
    return tbl;
  })();

  function crc32(bytes) {
    let c = 0xffffffff;
    for (let i = 0; i < bytes.length; i++) {
      c = crcTable[(c ^ bytes[i]) & 0xff] ^ (c >>> 8);
    }
    return (c ^ 0xffffffff) >>> 0;
  }

  function adler32(bytes) {
    let s1 = 1;
    let s2 = 0;
    for (let i = 0; i < bytes.length; i++) {
      s1 = (s1 + bytes[i]) % 65521;
      s2 = (s2 + s1) % 65521;
    }
    return ((s2 << 16) | s1) >>> 0;
  }

  function makeChunk(typeStr, data) {
    const chunk = new Uint8Array(12 + data.length);
    const dv = new DataView(chunk.buffer);
    dv.setUint32(0, data.length, false);
    for (let i = 0; i < 4; i++) {
      chunk[4 + i] = typeStr.charCodeAt(i);
    }
    chunk.set(data, 8);
    const crcInput = chunk.subarray(4, 8 + data.length);
    dv.setUint32(8 + data.length, crc32(crcInput), false);
    return chunk;
  }

  function makeUniquePNGBlob() {
    const rnd = new Uint8Array(4);
    window.crypto.getRandomValues(rnd);
    const sig = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
    const ihdr = new Uint8Array([
      0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0
    ]);
    const rawScanline = new Uint8Array([0, rnd[0], rnd[1], rnd[2], 255]);
    const adl = adler32(rawScanline);
    const zlibData = new Uint8Array([
      0x78, 0x01,
      0x01, 0x05, 0x00, 0xfa, 0xff,
      rawScanline[0], rawScanline[1], rawScanline[2], rawScanline[3], rawScanline[4],
      (adl >>> 24) & 0xff, (adl >>> 16) & 0xff, (adl >>> 8) & 0xff, adl & 0xff
    ]);
    const c1 = makeChunk("IHDR", ihdr);
    const c2 = makeChunk("IDAT", zlibData);
    const c3 = makeChunk("IEND", new Uint8Array(0));
    return new Blob([sig, c1, c2, c3], { type: "image/png" });
  }

  function getApiBase() {
    if (!state.uiConfig || !state.uiConfig.api_public_url) {
      throw new Error("/config.json belum dimuat atau api_public_url kosong");
    }
    return state.uiConfig.api_public_url.replace(/\/+$/, "");
  }

  async function apiRequest(method, path, opts) {
    const o = opts || {};
    const url = getApiBase() + path;
    const headers = Object.assign({}, o.headers || {});
    const upperMethod = String(method || "GET").toUpperCase();
    if (upperMethod !== "GET" && upperMethod !== "HEAD" && upperMethod !== "OPTIONS") {
      headers["X-Requested-With"] = "kopdes-tester";
    }
    if (o.token) {
      headers["Authorization"] = "Bearer " + o.token;
    }
    let body = o.body;
    if (o.json !== undefined) {
      headers["Content-Type"] = "application/json";
      body = JSON.stringify(o.json);
    }
    const fetchOpts = { method: upperMethod, headers: headers, body: body, credentials: "include" };
    let resp = await fetch(url, fetchOpts);
    if (resp.status === 429 && !o.allow429) {
      await new Promise(function (resolve) { setTimeout(resolve, 600); });
      resp = await fetch(url, fetchOpts);
    }
    const contentType = resp.headers.get("content-type") || "";
    const rawText = contentType.indexOf("image/") === 0 ? "[binary image " + contentType + "]" : await resp.text();
    let parsed = null;
    if (contentType.indexOf("application/json") !== -1 && rawText) {
      try {
        parsed = JSON.parse(rawText);
      } catch (_) {
        parsed = null;
      }
    }
    return {
      status: resp.status,
      ok: resp.ok,
      contentType: contentType,
      headers: resp.headers,
      text: rawText,
      json: parsed
    };
  }

  async function fetchSameOriginJSON(relPath) {
    const resp = await fetch(relPath, { cache: "no-store" });
    if (!resp.ok) {
      throw new Error("Gagal mengambil " + relPath + ": HTTP " + resp.status);
    }
    return resp.json();
  }

  async function fetchTOTPHelper(params) {
    const resp = await fetch("/api/totp-helper", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(params)
    });
    const data = await resp.json();
    if (!resp.ok || !data.code) {
      throw new Error(data.error || ("TOTP helper gagal HTTP " + resp.status));
    }
    return data.code;
  }

  function getSeedPassword() {
    const v = el("seedPasswordInput").value;
    if (!v) {
      throw new Error("Isi input Kata Sandi Uji (TEST_SEED_PASSWORD) terlebih dahulu.");
    }
    return v;
  }

  async function loginWithOptionalMFA(identifier, password, options) {
    const opts = options || {};
    const res = await apiRequest("POST", "/api/v1/auth/login", {
      json: { identifier: identifier, password: password },
      headers: opts.headers
    });
    if (res.status !== 200 || !res.json || !res.json.data) {
      throw new Error("Login " + identifier + " gagal HTTP " + res.status + ": " + res.text);
    }
    const d = res.json.data;
    const token = d.session_token || "";
    if (d.scope === "mfa_pending") {
      if (opts.explicitCode) {
        const mv = await apiRequest("POST", "/api/v1/auth/mfa/verify", {
          token: token,
          json: { code: opts.explicitCode }
        });
        if (mv.status !== 200) {
          throw new Error("MFA verify gagal HTTP " + mv.status + ": " + mv.text);
        }
      } else {
        const offsets = [0, 30, -30];
        let verified = false;
        let lastErr = "";
        for (let i = 0; i < offsets.length; i++) {
          const payload = opts.secretBase32
            ? { secret_base32: opts.secretBase32, offset_seconds: offsets[i] }
            : { account_email: identifier, offset_seconds: offsets[i] };
          const code = await fetchTOTPHelper(payload);
          const mv = await apiRequest("POST", "/api/v1/auth/mfa/verify", {
            token: token,
            json: { code: code }
          });
          if (mv.status === 200) {
            verified = true;
            break;
          }
          lastErr = "HTTP " + mv.status + ": " + mv.text;
        }
        if (!verified) {
          throw new Error("Verifikasi TOTP gagal pada semua step: " + lastErr);
        }
      }
    }
    const me = await apiRequest("GET", "/api/v1/auth/me", { token: token });
    const meData = (me.json && me.json.data) ? me.json.data : {};
    return {
      session_token: token,
      role: d.peran || meData.peran || "",
      email: identifier,
      user_id: d.user_id || meData.user_id || "",
      anggota_id: meData.anggota_id || "",
      scope: "full"
    };
  }

  async function ensureRoleToken(accountKeyOrEmail) {
    if (state.roleCache[accountKeyOrEmail]) {
      const check = await apiRequest("GET", "/api/v1/auth/me", { token: state.roleCache[accountKeyOrEmail] });
      if (check.status === 200) {
        return state.roleCache[accountKeyOrEmail];
      }
      delete state.roleCache[accountKeyOrEmail];
    }
    const pwd = getSeedPassword();
    let email = accountKeyOrEmail;
    if (state.uiConfig && Array.isArray(state.uiConfig.seeded_accounts)) {
      const targetNorm = String(accountKeyOrEmail).toLowerCase().replace(/_/g, "");
      const found = state.uiConfig.seeded_accounts.find(function (a) {
        return a.key === accountKeyOrEmail ||
          a.email === accountKeyOrEmail ||
          a.role === accountKeyOrEmail ||
          String(a.key || "").toLowerCase().replace(/_/g, "") === targetNorm;
      });
      if (found) {
        email = found.email;
      }
    }
    const sess = await loginWithOptionalMFA(email, pwd);
    state.roleCache[accountKeyOrEmail] = sess.session_token;
    return sess.session_token;
  }

  function updateSessionBanner() {
    const target = el("activeSessionInfo");
    if (!state.activeSession) {
      target.textContent = "Belum login";
      return;
    }
    const s = state.activeSession;
    const modeLabel = s.session_token ? ("token_len=" + s.session_token.length) : "auth=cookie-httponly";
    target.textContent = "peran=" + s.role + " | email=" + s.email + " | user_id=" + s.user_id +
      (s.anggota_id ? (" | anggota_id=" + s.anggota_id) : "") +
      " | " + modeLabel;
  }

  function populateAccountSelect() {
    const sel = el("accountSelect");
    sel.innerHTML = "";
    const accounts = (state.uiConfig && state.uiConfig.seeded_accounts) || [];
    accounts.forEach(function (acc) {
      const opt = document.createElement("option");
      opt.value = acc.email;
      opt.textContent = acc.key + " (" + acc.role + " | MFA=" + acc.mfa_enabled + ") - " + acc.email;
      sel.appendChild(opt);
    });
  }

  function defaultBodyForEndpoint(method, path) {
    if (method === "GET") return "";
    if (path === "/api/v1/auth/login") {
      return JSON.stringify({ identifier: "anggota1@kopdes-test.local", password: "" }, null, 2);
    }
    if (path === "/api/v1/auth/mfa/verify") {
      return JSON.stringify({ code: "123456" }, null, 2);
    }
    if (path === "/api/v1/public/pendaftaran") {
      return JSON.stringify({
        nama_lengkap: "Calon Anggota Uji",
        email: "calon-" + randomDigits(5) + "@kopdes-test.local",
        nik: "3201" + randomDigits(12),
        no_hp: "0817" + randomDigits(8),
        tanggal_lahir: "1995-05-10",
        alamat: "Jl. Desa Uji RT 01",
        pekerjaan: "Petani"
      }, null, 2);
    }
    if (path === "/api/v1/toko/checkout") {
      const pid = (state.uiConfig && state.uiConfig.resources && state.uiConfig.resources.produk_id) || "";
      return JSON.stringify({
        items: [{ produk_id: pid, kuantitas: 1 }],
        metode_pengiriman: "diantar",
        metode_pembayaran: "qris",
        nama_penerima: "Pembeli Tester",
        hp_penerima: "08119998887",
        alamat_pengantaran: "Jl. Balai Desa No. 3",
        tanggal_antar: new Date().toISOString().slice(0, 10)
      }, null, 2);
    }
    if (path === "/api/v1/pembayaran") {
      return JSON.stringify({
        jenis_transaksi: "pesanan",
        metode: "qris",
        pesanan_id: "",
        catatan_pembayar: "Uji QRIS tester"
      }, null, 2);
    }
    return JSON.stringify({}, null, 2);
  }

  function populateEndpointExplorer() {
    const sel = el("endpointSelect");
    sel.innerHTML = "";
    const paths = (state.openapi && state.openapi.paths) || {};
    Object.keys(paths).forEach(function (pathKey) {
      const methods = paths[pathKey];
      Object.keys(methods).forEach(function (methodKey) {
        const op = methods[methodKey];
        const mUpper = methodKey.toUpperCase();
        const opt = document.createElement("option");
        opt.value = mUpper + " " + pathKey;
        opt.textContent = mUpper + " " + pathKey + " — " + (op.summary || op.operationId || "");
        sel.appendChild(opt);
      });
    });
    syncExplorerInputs();
  }

  function syncExplorerInputs() {
    const val = el("endpointSelect").value;
    if (!val) return;
    const parts = val.split(" ");
    const method = parts[0];
    const path = parts.slice(1).join(" ");
    el("pathInput").value = path;
    el("queryInput").value = "";
    el("bodyInput").value = defaultBodyForEndpoint(method, path);
    const isMultipartRoute = path.indexOf("/dokumen") !== -1 && method === "POST";
    el("chkMultipart").checked = isMultipartRoute;
  }

  async function loadAllConfigs() {
    const statusEl = el("configStatus");
    statusEl.textContent = "Memuat /config.json ...";
    state.uiConfig = await fetchSameOriginJSON("/config.json");
    state.openapi = await fetchSameOriginJSON("/openapi.json");
    state.rolesMatrix = await fetchSameOriginJSON("/roles_matrix.json");
    state.scenarios = await fetchSameOriginJSON("/scenarios.json");
    populateAccountSelect();
    populateEndpointExplorer();

    const pub = await apiRequest("GET", "/api/v1/public/config");
    if (pub.status !== 200 || !pub.json || !pub.json.data) {
      statusEl.textContent = "API public/config gagal HTTP " + pub.status;
      return;
    }
    state.publicConfig = pub.json.data;
    const kop = state.publicConfig.koperasi || {};
    statusEl.textContent = "APP_ENV=" + state.uiConfig.app_env +
      " | API=" + state.uiConfig.api_public_url +
      " | Versi=" + state.publicConfig.api_version +
      " | Koperasi=" + (kop.nama_koperasi || "-") +
      " | QRIS Digits=" + (kop.qris_unique_digits || "-") +
      " | Ongkir=" + (kop.ongkir_tetap !== undefined ? kop.ongkir_tetap : "-");
    await restoreSessionFromCookie();
  }

  async function restoreSessionFromCookie() {
    if (state.activeSession) return;
    try {
      const me = await apiRequest("GET", "/api/v1/auth/me");
      if (me.status === 200 && me.json && me.json.data) {
        const d = me.json.data;
        state.activeSession = {
          session_token: "",
          role: d.peran || "",
          email: d.email || "",
          user_id: d.user_id || "",
          anggota_id: d.anggota_id || "",
          scope: d.scope || "full"
        };
        updateSessionBanner();
      }
    } catch (_) {
      // Abaikan jika belum ada cookie sesi aktif
    }
  }

  async function sendExplorerRequest(overrideMethod, overridePath, overrideQuery) {
    const val = el("endpointSelect").value || "GET /healthz";
    const method = overrideMethod || val.split(" ")[0];
    const rawPath = overridePath !== undefined ? overridePath : el("pathInput").value.trim();
    const rawQuery = overrideQuery !== undefined ? overrideQuery : el("queryInput").value.trim();
    const fullPath = rawPath + (rawQuery ? (rawQuery.indexOf("?") === 0 ? rawQuery : "?" + rawQuery) : "");

    const headers = {};
    const extraRaw = el("extraHeaderInput").value.trim();
    if (extraRaw && extraRaw.indexOf(":") !== -1) {
      const idx = extraRaw.indexOf(":");
      headers[extraRaw.slice(0, idx).trim()] = extraRaw.slice(idx + 1).trim();
    }

    const token = state.activeSession ? state.activeSession.session_token : "";
    let opts = { token: token, headers: headers };

    if (el("chkMultipart").checked && method === "POST") {
      const fd = new FormData();
      const meta = el("multipartMetaInput").value.trim();
      if (meta && meta.indexOf("=") !== -1) {
        const eq = meta.indexOf("=");
        fd.append(meta.slice(0, eq).trim(), meta.slice(eq + 1).trim());
      }
      const fileEl = el("multipartFileInput");
      if (fileEl.files && fileEl.files.length > 0) {
        fd.append("file", fileEl.files[0]);
      } else {
        fd.append("file", makeUniquePNGBlob(), "synthetic.png");
      }
      opts.body = fd;
    } else if (method !== "GET" && method !== "DELETE") {
      const b = el("bodyInput").value.trim();
      if (b) {
        opts.json = JSON.parse(b);
      }
    }

    const res = await apiRequest(method, fullPath, opts);
    el("explorerResponseOutput").textContent =
      method + " " + fullPath + "\nHTTP " + res.status + " (" + res.contentType + ")\n\n" + res.text;
    return res;
  }

  function wireHandlers() {
    el("endpointSelect").addEventListener("change", syncExplorerInputs);
    el("btnReloadConfig").addEventListener("click", function () {
      loadAllConfigs().catch(function (e) {
        el("configStatus").textContent = "Error: " + e.message;
      });
    });
    el("btnFetchTOTP").addEventListener("click", async function () {
      try {
        const code = await fetchTOTPHelper({ account_email: el("accountSelect").value });
        el("totpInput").value = code;
      } catch (e) {
        el("explorerResponseOutput").textContent = "TOTP Error: " + e.message;
      }
    });
    el("btnQuickLogin").addEventListener("click", async function () {
      try {
        const email = el("accountSelect").value;
        const explicitCode = el("totpInput").value.trim();
        const sess = await loginWithOptionalMFA(email, getSeedPassword(), { explicitCode: explicitCode });
        state.activeSession = sess;
        state.roleCache[email] = sess.session_token;
        updateSessionBanner();
        el("explorerResponseOutput").textContent = "Login sukses:\n" + JSON.stringify({
          role: sess.role, email: sess.email, user_id: sess.user_id, anggota_id: sess.anggota_id, scope: sess.scope
        }, null, 2);
      } catch (e) {
        el("explorerResponseOutput").textContent = "Login Error: " + e.message;
      }
    });
    el("btnCheckMe").addEventListener("click", async function () {
      try {
        const tok = state.activeSession ? state.activeSession.session_token : "";
        const res = await apiRequest("GET", "/api/v1/auth/me", { token: tok });
        el("explorerResponseOutput").textContent = "GET /api/v1/auth/me -> HTTP " + res.status + "\n" + res.text;
      } catch (e) {
        el("explorerResponseOutput").textContent = "Error: " + e.message;
      }
    });
    el("btnLogout").addEventListener("click", async function () {
      try {
        const tok = state.activeSession ? (state.activeSession.session_token || "") : "";
        await apiRequest("POST", "/api/v1/auth/logout", { token: tok, json: {} });
        Object.keys(state.roleCache).forEach(function (k) {
          if (state.roleCache[k] === tok) delete state.roleCache[k];
        });
        state.activeSession = null;
        updateSessionBanner();
        el("explorerResponseOutput").textContent = "Logout selesai; sesi server dicabut dan cookie dibersihkan.";
      } catch (e) {
        el("explorerResponseOutput").textContent = "Logout Error: " + e.message;
      }
    });
    el("btnGenIdemKey").addEventListener("click", function () {
      el("extraHeaderInput").value = "Idempotency-Key: idem-" + randomUUID();
    });
    el("btnSendRequest").addEventListener("click", function () {
      sendExplorerRequest().catch(function (e) {
        el("explorerResponseOutput").textContent = "Request Error: " + e.message;
      });
    });
    el("btnQuickKatalog").addEventListener("click", function () {
      sendExplorerRequest("GET", "/api/v1/katalog/produk", "").catch(function (e) {
        el("explorerResponseOutput").textContent = e.message;
      });
    });
    el("btnQuickTagihan").addEventListener("click", function () {
      sendExplorerRequest("GET", "/api/v1/iuran/tagihan", "").catch(function (e) {
        el("explorerResponseOutput").textContent = e.message;
      });
    });
    el("btnQuickDaftarAntar").addEventListener("click", function () {
      sendExplorerRequest("GET", "/api/v1/pesanan/daftar-antar", "").catch(function (e) {
        el("explorerResponseOutput").textContent = e.message;
      });
    });
    el("btnQuickLaporan").addEventListener("click", function () {
      sendExplorerRequest("GET", "/api/v1/laporan", "?jenis=pembayaran").catch(function (e) {
        el("explorerResponseOutput").textContent = e.message;
      });
    });
    el("btnQuickPengaturan").addEventListener("click", function () {
      sendExplorerRequest("GET", "/api/v1/public/config", "").catch(function (e) {
        el("explorerResponseOutput").textContent = e.message;
      });
    });
  }

  window.KopdesApp = {
    state: state,
    el: el,
    randomDigits: randomDigits,
    randomUUID: randomUUID,
    makeUniquePNGBlob: makeUniquePNGBlob,
    getApiBase: getApiBase,
    apiRequest: apiRequest,
    fetchTOTPHelper: fetchTOTPHelper,
    getSeedPassword: getSeedPassword,
    loginWithOptionalMFA: loginWithOptionalMFA,
    ensureRoleToken: ensureRoleToken,
    loadAllConfigs: loadAllConfigs
  };

  document.addEventListener("DOMContentLoaded", function () {
    wireHandlers();
    loadAllConfigs().catch(function (e) {
      el("configStatus").textContent = "Inisialisasi gagal: " + e.message;
    });
  });
})();
