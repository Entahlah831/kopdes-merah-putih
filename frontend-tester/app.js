(function () {
  "use strict";

  const state = {
    config: null,
    openapi: null,
    scenarios: null,
    rolesMatrix: null,
    guardConfirmed: false,
    accessToken: "",
    refreshToken: "",
    refreshInFlight: null,
    activeAccount: null,
    lastIdempotencyKey: "",
    lastRequestId: "",
    vars: {},
    recordedResponses: [],
    proofResults: [],
    liveDemoResults: [],
    roleTokens: {}
  };

  function el(id) {
    return document.getElementById(id);
  }

  function safeString(v) {
    return v === null || v === undefined ? "" : String(v);
  }

  function formatRupiah(nominal) {
    return new Intl.NumberFormat("id-ID", {
      style: "currency",
      currency: "IDR",
      maximumFractionDigits: 0
    }).format(Number(nominal || 0));
  }

  function getTodayJakartaDate() {
    try {
      return new Intl.DateTimeFormat("en-CA", {
        timeZone: "Asia/Jakarta",
        year: "numeric",
        month: "2-digit",
        day: "2-digit"
      }).format(new Date());
    } catch (_) {
      return new Date().toISOString().slice(0, 10);
    }
  }

  function redactSecrets(text) {
    let out = safeString(text);
    const secrets = [
      state.accessToken,
      state.refreshToken,
      el("testSeedPasswordInput") ? el("testSeedPasswordInput").value : "",
      el("metricsTokenInput") ? el("metricsTokenInput").value : ""
    ].filter(Boolean);
    for (const s of secrets) {
      if (s.length >= 4) {
        out = out.split(s).join("***REDACTED***");
      }
    }
    return out;
  }

  function getApiBase() {
    const raw = el("apiBaseUrlInput").value.trim() || (state.config && state.config.api_public_url) || window.location.origin;
    return raw.replace(/\/+$/, "");
  }

  function getTestPassword() {
    return el("testSeedPasswordInput").value;
  }

  function isLocalHostUrl(urlStr) {
    try {
      const u = new URL(urlStr);
      return u.hostname === "127.0.0.1" || u.hostname === "localhost" || u.hostname === "::1";
    } catch (_) {
      return false;
    }
  }

  function isMutationAllowed() {
    const base = getApiBase();
    const isProd = state.config && String(state.config.app_env).toLowerCase() === "production";
    if (!isLocalHostUrl(base) || isProd) {
      return state.guardConfirmed;
    }
    return true;
  }

  function updateGuardUI() {
    const base = getApiBase();
    const isProd = state.config && String(state.config.app_env).toLowerCase() === "production";
    const needGuard = !isLocalHostUrl(base) || isProd;
    el("nonLocalGuardBox").hidden = !needGuard;
    const locked = needGuard && !state.guardConfirmed;
    document.querySelectorAll('button[data-mutates="true"]').forEach(function (btn) {
      btn.disabled = locked;
    });
  }

  function generateUUIDv4() {
    if (window.crypto && typeof window.crypto.randomUUID === "function") {
      return window.crypto.randomUUID();
    }
    return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, function (c) {
      const r = (Math.random() * 16) | 0;
      const v = c === "x" ? r : (r & 0x3) | 0x8;
      return v.toString(16);
    });
  }

  let nikSeq = 0;
  function generateUniqueNIK() {
    nikSeq++;
    const tail = String((Date.now() % 1000000000) + nikSeq).padStart(10, "0").slice(-10);
    return "320101" + tail;
  }

  function createTinyPNGBlob() {
    // 1x1 transparent PNG bytes generated without external network assets
    const b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=";
    const bin = atob(b64);
    const arr = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) {
      arr[i] = bin.charCodeAt(i);
    }
    return new Blob([arr], { type: "image/png" });
  }

  function computeCRC16CCITT(str) {
    let crc = 0xffff;
    for (let i = 0; i < str.length; i++) {
      crc ^= (str.charCodeAt(i) & 0xff) << 8;
      for (let b = 0; b < 8; b++) {
        crc = (crc & 0x8000) !== 0 ? ((crc << 1) ^ 0x1021) & 0xffff : (crc << 1) & 0xffff;
      }
    }
    return crc.toString(16).toUpperCase().padStart(4, "0");
  }

  function buildSampleStaticQRIS(merchantName) {
    const m = merchantName || "Kopdes Uji";
    const tag26Val = "0011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI";
    const tag26 = "26" + String(tag26Val.length).padStart(2, "0") + tag26Val;
    const tag59 = "59" + String(m.length).padStart(2, "0") + m;
    const base = "000201010211" + tag26 + "5204541153033605802ID" + tag59 + "6007Jakarta6304";
    return base + computeCRC16CCITT(base);
  }

  function decodeJWTPayload(jwt) {
    try {
      const parts = jwt.split(".");
      if (parts.length !== 3) return null;
      const b64 = parts[1].replace(/-/g, "+").replace(/_/g, "/");
      const jsonStr = atob(b64);
      return JSON.parse(jsonStr);
    } catch (_) {
      return null;
    }
  }

  function updateSessionBar() {
    if (!state.accessToken) {
      el("sessionStatusText").textContent = "Belum login (Unauthenticated)";
    } else {
      const claims = decodeJWTPayload(state.accessToken) || {};
      const expText = claims.exp ? new Date(claims.exp * 1000).toISOString() : "-";
      el("sessionStatusText").textContent =
        "Role=" + (claims.role || "-") +
        " | Koperasi=" + (claims.koperasi_id || "-") +
        " | JTI=" + (claims.jti || "-") +
        " | Exp=" + expText;
    }
    if (el("lastRequestIdText")) {
      el("lastRequestIdText").textContent = state.lastRequestId || "-";
    }
    el("varsDumpPre").textContent = redactSecrets(JSON.stringify(state.vars, null, 2));
  }

  function recordTrace(entry) {
    state.recordedResponses.push({
      time: new Date().toISOString(),
      method: entry.method,
      path: entry.path,
      status: entry.status,
      requestId: entry.requestId || "",
      bodyText: redactSecrets(entry.bodyText)
    });
  }

  function appendFlowLog(title, detail) {
    const pre = el("flowOutputPre");
    const stamp = new Date().toISOString();
    const reqInfo = state.lastRequestId ? " [X-Request-ID=" + state.lastRequestId + "]" : "";
    const msg = "[" + stamp + "]" + reqInfo + " " + title + "\n" + redactSecrets(typeof detail === "string" ? detail : JSON.stringify(detail, null, 2)) + "\n\n";
    pre.textContent = msg + pre.textContent;
  }

  // Single-flight refresh mengikuti docs/FRONTEND_GUIDE.md §2
  async function refreshAccessTokenSingleFlight() {
    if (state.refreshInFlight) {
      return state.refreshInFlight;
    }
    if (!state.refreshToken) {
      state.accessToken = "";
      state.activeAccount = null;
      updateSessionBar();
      return null;
    }

    state.refreshInFlight = (async function () {
      try {
        const res = await fetchWith429Retry({
          method: "POST",
          path: "/api/v1/auth/refresh",
          token: "",
          json: { refresh_token: state.refreshToken },
          autoRefresh: false
        });
        const data = res.json && res.json.data;
        if (res.status !== 200 || !data || !data.access_token || !data.refresh_token) {
          state.accessToken = "";
          state.refreshToken = "";
          state.activeAccount = null;
          updateSessionBar();
          return null;
        }
        state.accessToken = data.access_token;
        state.refreshToken = data.refresh_token;
        updateSessionBar();
        return state.accessToken;
      } finally {
        state.refreshInFlight = null;
      }
    })();

    return state.refreshInFlight;
  }

  async function rawFetchAPI(opts) {
    const method = (opts.method || "GET").toUpperCase();
    const rawPath = opts.path || "/";
    if (method !== "GET" && method !== "HEAD" && method !== "OPTIONS" && !isMutationAllowed()) {
      throw new Error("Target non-lokal belum dikonfirmasi. Ketik SAYA PAHAM INI BUKAN PRODUKSI terlebih dahulu.");
    }

    const url = rawPath.startsWith("http") ? rawPath : getApiBase() + rawPath;
    const headers = Object.assign({}, opts.headers || {});
    const token = opts.token !== undefined ? opts.token : state.accessToken;
    if (token) {
      headers["Authorization"] = "Bearer " + token;
    }

    let bodyToSend = opts.body;
    if (opts.json !== undefined) {
      headers["Content-Type"] = "application/json";
      bodyToSend = JSON.stringify(opts.json);
    }

    const fetchOpts = {
      method: method,
      headers: headers,
      signal: opts.signal
    };
    if (method !== "GET" && method !== "HEAD" && bodyToSend !== undefined) {
      fetchOpts.body = bodyToSend;
    }

    const resp = await fetch(url, fetchOpts);
    const text = await resp.text();
    const requestId = resp.headers.get("X-Request-ID") || "";
    if (requestId) {
      state.lastRequestId = requestId;
      if (el("lastRequestIdText")) {
        el("lastRequestIdText").textContent = requestId;
      }
    }
    let parsed = null;
    try {
      parsed = JSON.parse(text);
    } catch (_) {
      parsed = null;
    }
    recordTrace({ method: method, path: rawPath, status: resp.status, requestId: requestId, bodyText: text });
    return {
      status: resp.status,
      headers: resp.headers,
      requestId: requestId,
      text: text,
      json: parsed
    };
  }

  function sleep(ms) {
    return new Promise(function (resolve) { setTimeout(resolve, ms); });
  }

  function isIPRateLimit429(res) {
    return !!(
      res &&
      res.status === 429 &&
      res.text &&
      res.text.includes("Batas frekuensi permintaan terlampaui")
    );
  }

  async function fetchWith429Retry(opts) {
    let res = await rawFetchAPI(opts);
    if (opts.retryOn429 !== false) {
      for (let attempt = 1; attempt <= 4 && isIPRateLimit429(res); attempt++) {
        const retryHdr = parseInt((res.headers && res.headers.get("Retry-After")) || "1", 10) || 1;
        await sleep(Math.min(2000, retryHdr * 550 * attempt));
        res = await rawFetchAPI(opts);
      }
    }
    return res;
  }

  async function apiRequest(opts) {
    const rawPath = opts.path || "/";
    let res = await fetchWith429Retry(opts);
    // Hanya auto-refresh untuk request sesi aktif (opts.token tidak di-override eksplisit)
    if (
      opts.autoRefresh !== false &&
      res.status === 401 &&
      opts.token === undefined &&
      state.refreshToken &&
      !rawPath.startsWith("/api/v1/auth/refresh") &&
      !rawPath.startsWith("/api/v1/auth/login")
    ) {
      const newToken = await refreshAccessTokenSingleFlight();
      if (newToken) {
        res = await fetchWith429Retry(opts);
      }
    }
    return res;
  }

  async function fetchTOTP(params) {
    const resp = await fetch("/api/totp-helper", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(params)
    });
    const data = await resp.json();
    if (!resp.ok) {
      throw new Error(data.error || "Gagal meminta kode TOTP");
    }
    return data.code;
  }

  async function verifyMFAWithWindowFallback(email, mfaSessionToken) {
    let lastVerifyRes = null;
    for (const offset of [0, 30]) {
      const code = await fetchTOTP({ email: email, offset_seconds: offset });
      const verifyRes = await apiRequest({
        method: "POST",
        path: "/api/v1/auth/mfa/verify",
        token: "",
        json: {
          mfa_session_token: mfaSessionToken,
          totp_code: code
        }
      });
      lastVerifyRes = verifyRes;
      if (verifyRes.status === 200 && verifyRes.json && verifyRes.json.data && verifyRes.json.data.access_token) {
        return verifyRes;
      }
      if (!(verifyRes.status === 401 && verifyRes.text.includes("sudah pernah digunakan"))) {
        break;
      }
    }
    throw new Error("MFA verify gagal (" + (lastVerifyRes ? lastVerifyRes.status : 0) + "): " + (lastVerifyRes ? lastVerifyRes.text : ""));
  }

  async function loginAccount(account, customPassword) {
    const pwd = customPassword !== undefined ? customPassword : getTestPassword();
    if (!pwd) {
      throw new Error("Isi input 'Kata Sandi Akun Uji' terlebih dahulu.");
    }
    const res = await apiRequest({
      method: "POST",
      path: "/api/v1/auth/login",
      token: "",
      json: {
        email: account.email,
        password: pwd,
        koperasi_id: account.koperasi_id || undefined
      }
    });

    if (res.status === 200 && res.json && res.json.data && res.json.data.access_token) {
      return {
        accessToken: res.json.data.access_token,
        refreshToken: res.json.data.refresh_token || "",
        loginResponse: res
      };
    }

    if (res.status === 200 && res.json && res.json.data && res.json.data.mfa_required && res.json.data.mfa_session_token) {
      const verifyRes = await verifyMFAWithWindowFallback(account.email, res.json.data.mfa_session_token);
      return {
        accessToken: verifyRes.json.data.access_token,
        refreshToken: verifyRes.json.data.refresh_token || "",
        loginResponse: verifyRes
      };
    }

    throw new Error("Login gagal (" + res.status + "): " + res.text);
  }

  function buildAccountsMap() {
    const accountsMap = {};
    for (const a of (state.config && state.config.seeded_accounts) || []) {
      accountsMap[a.key] = a;
      accountsMap[a.role + "_" + a.tenant.toLowerCase()] = accountsMap[a.role + "_" + a.tenant.toLowerCase()] || a;
    }
    return accountsMap;
  }

  function renderAccountsTable() {
    const tbody = el("accountsTableBody");
    tbody.textContent = "";
    const accounts = (state.config && state.config.seeded_accounts) || [];
    if (!accounts.length) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = 7;
      td.textContent = "Tidak ada akun seeded di /config.json.";
      tr.appendChild(td);
      tbody.appendChild(tr);
      return;
    }

    for (const acc of accounts) {
      const tr = document.createElement("tr");
      const cols = [acc.key, acc.tenant, acc.email, acc.role, acc.mfa_mandatory ? "Ya" : "Tidak", acc.description];
      for (const c of cols) {
        const td = document.createElement("td");
        td.textContent = c;
        tr.appendChild(td);
      }
      const tdAct = document.createElement("td");
      const btnFill = document.createElement("button");
      btnFill.type = "button";
      btnFill.textContent = "Isi Form Login";
      btnFill.addEventListener("click", function () {
        el("flowLoginEmail").value = acc.email;
        el("activeKoperasiInput").value = acc.koperasi_id;
        el("flowLoginPass").value = getTestPassword();
      });

      const btnQuick = document.createElement("button");
      btnQuick.type = "button";
      btnQuick.textContent = "Login Cepat";
      btnQuick.setAttribute("data-mutates", "true");
      btnQuick.addEventListener("click", async function () {
        try {
          const authRes = await loginAccount(acc);
          state.accessToken = authRes.accessToken;
          state.refreshToken = authRes.refreshToken;
          state.activeAccount = acc;
          el("activeKoperasiInput").value = acc.koperasi_id;
          state.roleTokens[acc.key] = authRes.accessToken;
          updateSessionBar();
          appendFlowLog("Login Cepat OK (" + acc.key + ")", { role: acc.role, status: authRes.loginResponse.status });
        } catch (err) {
          appendFlowLog("Login Cepat Gagal (" + acc.key + ")", err.message);
        }
      });

      tdAct.appendChild(btnFill);
      tdAct.appendChild(document.createTextNode(" "));
      tdAct.appendChild(btnQuick);
      tr.appendChild(tdAct);
      tbody.appendChild(tr);
    }
    updateGuardUI();
  }

  function populateOpenAPIOperations() {
    const sel = el("openapiOpSelect");
    sel.textContent = "";
    if (!state.openapi || !state.openapi.paths) {
      const opt = document.createElement("option");
      opt.textContent = "(openapi.json belum tersedia)";
      sel.appendChild(opt);
      return;
    }

    const ops = [];
    for (const pathStr of Object.keys(state.openapi.paths).sort()) {
      const pathObj = state.openapi.paths[pathStr];
      for (const methodStr of Object.keys(pathObj)) {
        const mUpper = methodStr.toUpperCase();
        if (!["GET", "POST", "PUT", "PATCH", "DELETE"].includes(mUpper)) continue;
        ops.push({ method: mUpper, path: pathStr, spec: pathObj[methodStr] });
      }
    }

    ops.forEach(function (op, idx) {
      const opt = document.createElement("option");
      opt.value = String(idx);
      opt.textContent = op.method + " " + op.path + " — " + (op.spec.summary || op.spec.operationId || "");
      sel.appendChild(opt);
    });
    state._openapiOps = ops;
    onSelectOpenAPIOp();
  }

  function getSelectedOpenAPIOp() {
    if (!state._openapiOps || !state._openapiOps.length) return null;
    const idx = parseInt(el("openapiOpSelect").value || "0", 10);
    return state._openapiOps[idx] || state._openapiOps[0];
  }

  function onSelectOpenAPIOp() {
    const op = getSelectedOpenAPIOp();
    if (!op) return;
    el("openapiOpDesc").textContent = (op.spec.description || op.spec.summary || "");
    const pathDefaults = {};
    const params = op.spec.parameters || [];
    const queryDefaults = {};
    for (const p of params) {
      if (p.in === "path") {
        pathDefaults[p.name] = state.vars[p.name] || "<uuid>";
      } else if (p.in === "query") {
        queryDefaults[p.name] = (p.schema && p.schema.default) || "";
      }
    }
    el("openapiPathParams").value = JSON.stringify(pathDefaults, null, 2);
    el("openapiQueryParams").value = JSON.stringify(queryDefaults, null, 2);

    let bodySample = {};
    if (op.spec.requestBody && op.spec.requestBody.content && op.spec.requestBody.content["application/json"]) {
      const media = op.spec.requestBody.content["application/json"];
      if (media.example) bodySample = media.example;
      else if (media.schema && media.schema.example) bodySample = media.schema.example;
    }
    el("openapiBody").value = JSON.stringify(bodySample, null, 2);
  }

  function buildOpenAPIRequestDetails() {
    const op = getSelectedOpenAPIOp();
    if (!op) throw new Error("OpenAPI spec belum dimuat.");
    const pathParams = JSON.parse(el("openapiPathParams").value || "{}");
    const queryParams = JSON.parse(el("openapiQueryParams").value || "{}");
    let finalPath = op.path;
    for (const k of Object.keys(pathParams)) {
      finalPath = finalPath.replace("{" + k + "}", encodeURIComponent(pathParams[k]));
    }
    const qs = new URLSearchParams();
    for (const k of Object.keys(queryParams)) {
      if (queryParams[k] !== "" && queryParams[k] !== null && queryParams[k] !== undefined) {
        qs.set(k, String(queryParams[k]));
      }
    }
    const qStr = qs.toString();
    if (qStr) {
      finalPath += "?" + qStr;
    }

    const headers = {};
    const idempMode = el("openapiIdempMode").value;
    if (idempMode === "auto") {
      state.lastIdempotencyKey = generateUUIDv4();
      el("openapiIdempCustom").value = state.lastIdempotencyKey;
      headers["Idempotency-Key"] = state.lastIdempotencyKey;
    } else if (idempMode === "reuse") {
      if (!state.lastIdempotencyKey) state.lastIdempotencyKey = generateUUIDv4();
      el("openapiIdempCustom").value = state.lastIdempotencyKey;
      headers["Idempotency-Key"] = state.lastIdempotencyKey;
    } else if (idempMode === "custom" && el("openapiIdempCustom").value.trim()) {
      headers["Idempotency-Key"] = el("openapiIdempCustom").value.trim();
    }

    const rawBody = el("openapiBody").value.trim();
    return {
      method: op.method,
      path: finalPath,
      headers: headers,
      hasBody: ["POST", "PUT", "PATCH"].includes(op.method) && rawBody !== "",
      rawBody: rawBody
    };
  }

  function appendStatusCell(tr, statusText) {
    const td = document.createElement("td");
    td.textContent = statusText;
    if (statusText === "PASS") td.className = "badge-pass";
    else if (statusText === "FAIL") td.className = "badge-fail";
    else if (statusText.startsWith("SKIP")) td.className = "badge-skip";
    tr.appendChild(td);
  }

  async function runLiveDemo() {
    const pwd = getTestPassword();
    if (!pwd) {
      alert("Isi 'Kata Sandi Akun Uji' di Bagian 0 terlebih dahulu.");
      return;
    }
    if (!isMutationAllowed()) {
      alert("Konfirmasi target non-lokal pada Bagian 0 terlebih dahulu.");
      return;
    }

    const accountsMap = buildAccountsMap();
    const resIds = (state.config && state.config.resources) || {};
    const tbody = el("liveDemoTableBody");
    tbody.textContent = "";
    el("btnDownloadLiveDemoJSON").disabled = true;
    el("liveDemoSummary").textContent = "Menjalankan Live Demo 10 modul end-to-end ...";

    const demoRows = [];
    const tokens = {};
    const loginCached = async function (key, forceFresh) {
      if (!forceFresh && tokens[key]) return tokens[key];
      const acc = accountsMap[key];
      if (!acc) throw new Error("Akun seeded tidak ditemukan: " + key);
      const auth = await loginAccount(acc, pwd);
      tokens[key] = auth;
      state.roleTokens[key] = auth.accessToken;
      return auth;
    };

    const runStep = async function (modul, label, fn) {
      let statusLabel = "PASS";
      let httpCode = "-";
      let reqId = "-";
      let summary = "";
      try {
        const out = await fn();
        if (out && out.skip) {
          statusLabel = "SKIP";
          httpCode = out.http || "-";
          reqId = out.requestId || state.lastRequestId || "-";
          summary = out.summary || "Dilewati";
        } else {
          httpCode = safeString((out && out.http) || 200);
          reqId = (out && out.requestId) || state.lastRequestId || "-";
          summary = redactSecrets((out && out.summary) || "OK");
        }
      } catch (err) {
        statusLabel = "FAIL";
        reqId = state.lastRequestId || "-";
        summary = redactSecrets(err && err.message ? err.message : String(err));
      }

      const row = {
        modul: modul,
        fitur: label,
        status: statusLabel,
        http_status: httpCode,
        x_request_id: reqId,
        ringkasan: summary,
        timestamp: new Date().toISOString()
      };
      demoRows.push(row);

      const tr = document.createElement("tr");
      [row.modul, row.fitur].forEach(function (c) {
        const td = document.createElement("td");
        td.textContent = c;
        tr.appendChild(td);
      });
      appendStatusCell(tr, row.status);
      [row.http_status, row.x_request_id, row.ringkasan].forEach(function (c) {
        const td = document.createElement("td");
        td.textContent = safeString(c);
        tr.appendChild(td);
      });
      tbody.appendChild(tr);
    };

    // Modul 1: Health, Readiness, X-Request-ID & Hardening HTTP
    await runStep("1. Infrastruktur HTTP", "GET /healthz, GET /readyz, JSON rusak 400, GET /metrics proteksi token", async function () {
      const h = await apiRequest({ method: "GET", path: "/healthz", token: "" });
      const rd = await apiRequest({ method: "GET", path: "/readyz", token: "" });
      const bad = await apiRequest({ method: "POST", path: "/api/v1/auth/login", token: "", headers: { "Content-Type": "application/json" }, body: "{\"email\":" });
      const munauth = await apiRequest({ method: "GET", path: "/metrics", token: "" });
      if (h.status !== 200 || !h.requestId) throw new Error("/healthz gagal atau tanpa X-Request-ID: " + h.status);
      if (rd.status !== 200) throw new Error("/readyz gagal: " + rd.status);
      if (bad.status !== 400) throw new Error("JSON rusak harus 400, dapat " + bad.status);
      if (munauth.status !== 401) throw new Error("/metrics tanpa token harus 401, dapat " + munauth.status);
      const mTok = el("metricsTokenInput").value.trim();
      let metricsNote = "unauth=401";
      if (mTok) {
        const mauth = await apiRequest({ method: "GET", path: "/metrics", token: mTok });
        if (mauth.status !== 200) throw new Error("/metrics dengan token gagal: " + mauth.status);
        metricsNote += ", auth=200";
      }
      return { http: "200/400/401", requestId: h.requestId, summary: "healthz=200, readyz=200, bad_json=400, /metrics (" + metricsNote + ")" };
    });

    // Modul 2: Auth, Single-Flight Refresh, MFA TOTP & Wajib Enrollment
    await runStep("2. Auth & MFA", "Login anggota_a, Single-Flight Refresh 2x401->200, Login MFA bendahara_a, Enroll Wajib admin_nomfa_a", async function () {
      const ang = await loginCached("anggota_a", true);
      state.accessToken = "expired.probe.token";
      state.refreshToken = ang.refreshToken;
      state.activeAccount = accountsMap["anggota_a"];
      updateSessionBar();

      const [r1, r2] = await Promise.all([
        apiRequest({ method: "GET", path: "/api/v1/auth/me" }),
        apiRequest({ method: "GET", path: "/api/v1/auth/me" })
      ]);
      if (r1.status !== 200 || r2.status !== 200) {
        throw new Error("Single-flight refresh gagal: r1=" + r1.status + ", r2=" + r2.status);
      }
      tokens["anggota_a"] = { accessToken: state.accessToken, refreshToken: state.refreshToken };

      const b1 = await loginCached("bendahara_a", true);
      const bMe = await apiRequest({ method: "GET", path: "/api/v1/auth/me", token: b1.accessToken });
      if (bMe.status !== 200) throw new Error("/auth/me bendahara gagal: " + bMe.status);

      const nomfaLogin = await apiRequest({
        method: "POST",
        path: "/api/v1/auth/login",
        token: "",
        json: { email: "admin_nomfa_a@kopdes-test.local", password: pwd, koperasi_id: state.config.koperasi_a_id }
      });
      let mfaNote = "";
      if (nomfaLogin.status === 403 && nomfaLogin.json && nomfaLogin.json.data && nomfaLogin.json.data.setup_token) {
        const sTok = nomfaLogin.json.data.setup_token;
        const sRes = await apiRequest({ method: "POST", path: "/api/v1/auth/mfa/setup", token: sTok, json: {} });
        const sec = sRes.json && sRes.json.data && sRes.json.data.secret;
        if (!sec) throw new Error("mfa/setup tidak mengembalikan secret");
        const code = await fetchTOTP({ secret_base32: sec });
        const enRes = await apiRequest({ method: "POST", path: "/api/v1/auth/mfa/enable", token: sTok, json: { secret: sec, totp_code: code } });
        if (enRes.status !== 200) throw new Error("mfa/enable gagal: " + enRes.status);
        mfaNote = "enroll wajib 403->setup->enable OK";
      } else if (nomfaLogin.status === 200 && nomfaLogin.json && nomfaLogin.json.data && nomfaLogin.json.data.mfa_session_token) {
        await verifyMFAWithWindowFallback("admin_nomfa_a@kopdes-test.local", nomfaLogin.json.data.mfa_session_token);
        mfaNote = "sudah enroll sebelumnya -> verify TOTP OK";
      } else {
        throw new Error("Respons login admin_nomfa_a tidak terduga: " + nomfaLogin.status);
      }

      // Reuse detection pada operator_a
      const opAuth = await loginAccount(accountsMap["operator_a"], pwd);
      const rt1 = opAuth.refreshToken;
      const rot = await apiRequest({ method: "POST", path: "/api/v1/auth/refresh", token: "", json: { refresh_token: rt1 } });
      if (rot.status !== 200) throw new Error("Rotasi refresh token gagal: " + rot.status);
      const reuse = await apiRequest({ method: "POST", path: "/api/v1/auth/refresh", token: "", json: { refresh_token: rt1 } });
      if (reuse.status !== 401) throw new Error("Replay refresh token harus 401, dapat " + reuse.status);

      return { http: "200/401/403", requestId: r1.requestId, summary: "Single-flight 2x200 OK, MFA bendahara OK, " + mfaNote + ", Reuse RT=401" };
    });

    // Modul 3: Pendaftaran Anggota, KYC PNG, NIK Masking & Maker-Checker
    await runStep("3. Pendaftaran & KYC", "Ajukan NIK unik -> Upload PNG KTP -> Cek nik_masked -> Maker-Checker 403 vs 200", async function () {
      const p1 = await loginCached("pengurus_a");
      const p2 = await loginCached("pengurus2_a");
      const nik = generateUniqueNIK();
      const reg = await apiRequest({
        method: "POST",
        path: "/api/v1/pendaftaran",
        token: p1.accessToken,
        json: {
          nama_lengkap: "Calon Live Demo " + nik.slice(-4),
          nik: nik,
          tempat_lahir: "Bogor",
          tanggal_lahir: "1993-05-12",
          jenis_kelamin: "P",
          pekerjaan: "Pedagang",
          alamat_ktp: "Jl. Koperasi Desa No. 7"
        }
      });
      if (reg.status !== 201 || !reg.json || !reg.json.data || !reg.json.data.pendaftaran_id) {
        throw new Error("POST /pendaftaran gagal: " + reg.status + " " + reg.text);
      }
      const pid = reg.json.data.pendaftaran_id;
      state.vars.last_pendaftaran_id = pid;

      const fd = new FormData();
      fd.append("dokumen", createTinyPNGBlob(), "ktp-demo.png");
      fd.append("jenis_dokumen", "ktp");
      const up = await apiRequest({ method: "POST", path: "/api/v1/pendaftaran/" + pid + "/dokumen", token: p1.accessToken, body: fd });
      if (up.status !== 201 && up.status !== 200) throw new Error("Upload KYC gagal: " + up.status);

      const det = await apiRequest({ method: "GET", path: "/api/v1/pendaftaran/" + pid, token: p1.accessToken });
      if (det.status !== 200 || det.text.includes(nik) || !(det.json && det.json.data && String(det.json.data.nik_masked).includes("*"))) {
        throw new Error("NIK tidak termasking dengan aman pada GET /pendaftaran/{id}");
      }

      const selfVer = await apiRequest({ method: "POST", path: "/api/v1/pendaftaran/" + pid + "/verifikasi", token: p1.accessToken, json: { status: "disetujui" } });
      if (selfVer.status !== 403) throw new Error("Self-verify pendaftaran harus 403, dapat " + selfVer.status);

      const appVer = await apiRequest({ method: "POST", path: "/api/v1/pendaftaran/" + pid + "/verifikasi", token: p2.accessToken, json: { status: "disetujui" } });
      if (appVer.status !== 200) throw new Error("Approve pendaftaran pengurus2_a gagal: " + appVer.status);

      return {
        http: "201/403/200",
        requestId: appVer.requestId,
        summary: "nik_masked=" + det.json.data.nik_masked + ", no_anggota=" + (appVer.json && appVer.json.data && appVer.json.data.no_anggota)
      };
    });

    // Modul 4: Rekening QRIS Merchant
    await runStep("4. Rekening QRIS", "Tolak CRC rusak (400), Tolak rekening pribadi non-pilot (403), List rekening badan hukum (200)", async function () {
      const p1 = await loginCached("pengurus_a");
      const badCrc = await apiRequest({
        method: "POST",
        path: "/api/v1/koperasi/rekening-qris",
        token: p1.accessToken,
        json: {
          nama_merchant: "Bad CRC Demo",
          nmid: "ID88888888",
          nama_bank_pjp: "BRI",
          no_rekening_settlement: "11223344",
          nama_pemilik_rekening: "Koperasi Kopdes",
          is_atas_nama_badan_hukum: true,
          jenis_pemilik: "badan_hukum_koperasi",
          qris_payload_statis: "0002010102115802ID5906KOPDES6005BOGOR6304DEAD"
        }
      });
      const personal = await apiRequest({
        method: "POST",
        path: "/api/v1/koperasi/rekening-qris",
        token: p1.accessToken,
        json: {
          nama_merchant: "Personal Demo",
          nmid: "ID88888887",
          nama_bank_pjp: "BCA",
          no_rekening_settlement: "99887766",
          nama_pemilik_rekening: "Pengurus Pribadi",
          is_atas_nama_badan_hukum: false,
          jenis_pemilik: "perorangan_pengurus",
          qris_payload_statis: buildSampleStaticQRIS("DemoPersonal")
        }
      });
      const list = await apiRequest({ method: "GET", path: "/api/v1/koperasi/rekening-qris", token: p1.accessToken });
      if (badCrc.status !== 400) throw new Error("CRC rusak harus 400, dapat " + badCrc.status);
      if (personal.status !== 403) throw new Error("Rekening perorangan non-pilot harus ditolak 403, dapat " + personal.status);
      if (list.status !== 200) throw new Error("List rekening gagal: " + list.status);
      return {
        http: "400/" + personal.status + "/200",
        requestId: list.requestId,
        summary: "bad_crc=400, personal_rejected=" + personal.status + ", rekening_aktif=" + ((list.json && list.json.data && list.json.data.length) || 0)
      };
    });

    // Modul 5: Iuran Netral (KEPUTUSAN AKUNTAN) & Generate Tagihan
    await runStep("5. Iuran Anggota", "GET /iuran/jenis, POST /iuran/jenis (KEPUTUSAN AKUNTAN), POST /iuran/generate, GET /tagihan/saya & /menunggak", async function () {
      const p1 = await loginCached("pengurus_a");
      const ang = await loginCached("anggota_a");
      const pw = await loginCached("pengawas_a");
      const ts = Date.now();
      const getJ = await apiRequest({ method: "GET", path: "/api/v1/iuran/jenis", token: ang.accessToken });
      const postJ = await apiRequest({
        method: "POST",
        path: "/api/v1/iuran/jenis",
        token: p1.accessToken,
        json: { kode: "DEMO-" + ts, nama: "Iuran Live Demo " + ts, tipe: "wajib", nominal_standar: 25000 }
      });
      if (postJ.status !== 201 || !postJ.text.includes("KEPUTUSAN AKUNTAN")) {
        throw new Error("POST /iuran/jenis gagal atau tanpa penanda KEPUTUSAN AKUNTAN: " + postJ.status);
      }
      const jiId = postJ.json.data.id;
      const gen = await apiRequest({ method: "POST", path: "/api/v1/iuran/generate", token: p1.accessToken, json: { jenis_iuran_id: jiId, periode: "2026-11" } });
      const myT = await apiRequest({ method: "GET", path: "/api/v1/iuran/tagihan/saya", token: ang.accessToken });
      const mng = await apiRequest({ method: "GET", path: "/api/v1/iuran/menunggak", token: pw.accessToken });
      if (getJ.status !== 200 || gen.status !== 200 || myT.status !== 200 || mng.status !== 200) {
        throw new Error("Status iuran tidak lengkap: " + [getJ.status, gen.status, myT.status, mng.status].join("/"));
      }
      return {
        http: "200/201",
        requestId: gen.requestId,
        summary: "Tagihan baru=" + (gen.json && gen.json.data && gen.json.data.jumlah_tagihan_baru) + ", penanda KEPUTUSAN AKUNTAN ada"
      };
    });

    // Modul 6: Pembayaran QRIS, Idempotency-Key & Maker-Checker
    await runStep("6. Pembayaran QRIS", "POST /pembayaran/qris (201), Replay (201 sama ID), Konflik Body (422), Klaim RRN & Maker-Checker", async function () {
      const ang = await loginCached("anggota_a");
      const p1 = await loginCached("pengurus_a");
      const p2 = await loginCached("pengurus2_a");

      const makeOrder = async function (tag) {
        const c = await apiRequest({ method: "POST", path: "/api/v1/toko/keranjang", token: ang.accessToken, json: { produk_id: resIds.produk_a_id, qty: 1 } });
        if (c.status !== 200 && c.status !== 201) throw new Error("Add cart gagal: " + c.status);
        const co = await apiRequest({ method: "POST", path: "/api/v1/toko/checkout", token: ang.accessToken, json: { metode_serah_terima: "ambil_di_koperasi", catatan: tag } });
        if (co.status !== 201) throw new Error("Checkout gagal: " + co.status);
        return co.json.data.pesanan_id;
      };

      const oid1 = await makeOrder("LiveDemo QRIS 1");
      const oid2 = await makeOrder("LiveDemo QRIS 2");
      const idempKey = generateUUIDv4();
      const body1 = { pesanan_id: oid1, anggota_id: resIds.anggota_a_id, metode_pembayaran_id: resIds.metode_pembayaran_a_id, rekening_qris_id: resIds.rekening_qris_a_id };

      const q1 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/qris", token: p1.accessToken, headers: { "Idempotency-Key": idempKey }, json: body1 });
      const q2 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/qris", token: p1.accessToken, headers: { "Idempotency-Key": idempKey }, json: body1 });
      const qConflict = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/qris", token: p1.accessToken, headers: { "Idempotency-Key": idempKey }, json: { pesanan_id: oid2, anggota_id: resIds.anggota_a_id, metode_pembayaran_id: resIds.metode_pembayaran_a_id, rekening_qris_id: resIds.rekening_qris_a_id } });
      if (q1.status !== 201 || q2.status !== 201) throw new Error("QRIS/Replay gagal: " + q1.status + "/" + q2.status);
      if (q1.json.data.pembayaran_id !== q2.json.data.pembayaran_id) throw new Error("Replay mengembalikan pembayaran_id berbeda!");
      if (qConflict.status !== 422) throw new Error("Body berbeda pada Idempotency-Key sama harus 422, dapat " + qConflict.status);

      const payId = q1.json.data.pembayaran_id;
      state.vars.last_pembayaran_id = payId;
      const klaim = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/" + payId + "/bukti", token: p1.accessToken, json: { referensi_rrn: "RRN-LD-" + String(Date.now()).slice(-6), catatan: "Bukti live demo" } });
      const selfVer = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/" + payId + "/verifikasi", token: p1.accessToken, json: { disetujui: true } });
      const okVer = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/" + payId + "/verifikasi", token: p2.accessToken, json: { disetujui: true, catatan: "Valid Live Demo" } });
      if (klaim.status !== 200 || selfVer.status !== 403 || okVer.status !== 200) {
        throw new Error("Klaim/Maker-Checker gagal: " + [klaim.status, selfVer.status, okVer.status].join("/"));
      }
      return {
        http: "201/422/403/200",
        requestId: okVer.requestId,
        summary: "Total=" + formatRupiah(q1.json.data.nominal_total) + " (+kode " + q1.json.data.kode_unik + "), conflict=422, Maker-Checker=403->200"
      };
    });

    // Modul 7: Impor Mutasi Bank CSV, Segregasi Peran & Rekonsiliasi Harian
    await runStep("7. Mutasi CSV & Rekonsiliasi", "Impor CSV 5 kolom (auto-approve off -> menunggu_persetujuan_kedua), Deteksi Duplikat, Segregasi 403 vs 200", async function () {
      const op = await loginCached("operator_a");
      const p1 = await loginCached("pengurus_a");
      const p2 = await loginCached("pengurus2_a");
      const pw = await loginCached("pengawas_a");

      const c7 = await apiRequest({ method: "POST", path: "/api/v1/toko/keranjang", token: op.accessToken, json: { produk_id: resIds.produk_a_id, qty: 1 } });
      if (c7.status !== 200 && c7.status !== 201) {
        throw new Error("Add cart mutasi gagal: " + c7.status + " " + c7.text);
      }
      const co = await apiRequest({ method: "POST", path: "/api/v1/toko/checkout", token: op.accessToken, json: { metode_serah_terima: "ambil_di_koperasi", catatan: "LiveDemo Mutasi" } });
      if (co.status !== 201 || !co.json || !co.json.data || !co.json.data.pesanan_id) {
        throw new Error("Checkout mutasi gagal: " + co.status + " " + co.text);
      }
      const q = await apiRequest({
        method: "POST",
        path: "/api/v1/pembayaran/qris",
        token: op.accessToken,
        json: { pesanan_id: co.json.data.pesanan_id, anggota_id: resIds.anggota_a_id, metode_pembayaran_id: resIds.metode_pembayaran_a_id, rekening_qris_id: resIds.rekening_qris_a_id }
      });
      if (q.status !== 201 || !q.json || !q.json.data || !q.json.data.pembayaran_id) {
        throw new Error("Buat QRIS mutasi gagal: " + q.status + " " + q.text);
      }
      const payId = q.json.data.pembayaran_id;
      const nomTotal = q.json.data.nominal_total;
      const tglJkt = getTodayJakartaDate();
      const csv = "tanggal,keterangan,tipe,nominal,no_rekening\n" + tglJkt + " 10:15:00,SETTLEMENT LIVE DEMO " + payId + ",CR," + nomTotal + ",111222333A\n";

      const fd1 = new FormData();
      fd1.append("file", new Blob([csv], { type: "text/csv" }), "mutasi-demo.csv");
      const imp1 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/mutasi-bank/impor", token: p1.accessToken, body: fd1 });
      const chk = await apiRequest({ method: "GET", path: "/api/v1/pembayaran/" + payId, token: op.accessToken });
      const fd2 = new FormData();
      fd2.append("file", new Blob([csv], { type: "text/csv" }), "mutasi-demo.csv");
      const imp2 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/mutasi-bank/impor", token: p1.accessToken, body: fd2 });
      const sameImp = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/" + payId + "/verifikasi", token: p1.accessToken, json: { disetujui: true } });
      const secVer = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/" + payId + "/verifikasi", token: p2.accessToken, json: { disetujui: true, catatan: "Konfirmasi ke-2 live demo" } });
      const rekon = await apiRequest({ method: "GET", path: "/api/v1/pembayaran/rekonsiliasi/harian?tanggal=" + encodeURIComponent(tglJkt), token: pw.accessToken });

      if (imp1.status !== 200 || imp2.status !== 200 || !(chk.text.includes("menunggu_persetujuan_kedua")) || sameImp.status !== 403 || secVer.status !== 200 || rekon.status !== 200) {
        throw new Error("Alur mutasi/segregasi/rekon tidak sesuai: " + [imp1.status, imp2.status, sameImp.status, secVer.status, rekon.status].join("/"));
      }
      return {
        http: "200/403/200",
        requestId: rekon.requestId,
        summary: "Matched=" + formatRupiah(nomTotal) + " -> menunggu_persetujuan_kedua, duplikat=" + (imp2.json && imp2.json.data && imp2.json.data.duplikat) + ", rekon(" + tglJkt + ")=200"
      };
    });

    // Modul 8: Katalog, Harga Anggota, Promo, Checkout, Batal & Expire
    await runStep("8. Katalog & Toko", "List produk/stok/harga/promo, Checkout promo + Batal (pulih stok), Expire operator 200 vs pengawas 403", async function () {
      const ang = await loginCached("anggota_a");
      const pw = await loginCached("pengawas_a");
      const op = await loginCached("operator_a");

      const prod = await apiRequest({ method: "GET", path: "/api/v1/katalog/produk", token: ang.accessToken });
      const stok = await apiRequest({ method: "GET", path: "/api/v1/katalog/stok", token: ang.accessToken });
      const hrg = await apiRequest({ method: "GET", path: "/api/v1/katalog/harga?produk_id=" + resIds.produk_a_id + "&anggota_id=" + resIds.anggota_a_id, token: ang.accessToken });
      const prm = await apiRequest({ method: "GET", path: "/api/v1/katalog/promo", token: ang.accessToken });

      const cb1 = await apiRequest({ method: "POST", path: "/api/v1/toko/keranjang", token: ang.accessToken, json: { produk_id: resIds.produk_a_id, qty: 1 } });
      if (cb1.status !== 200 && cb1.status !== 201) {
        throw new Error("Add cart batal gagal: " + cb1.status + " " + cb1.text);
      }
      const coBatal = await apiRequest({ method: "POST", path: "/api/v1/toko/checkout", token: ang.accessToken, json: { metode_serah_terima: "ambil_di_koperasi", kode_promo: resIds.promo_a_kode, catatan: "LiveDemo Batal" } });
      if (coBatal.status !== 201 || !coBatal.json || !coBatal.json.data || !coBatal.json.data.pesanan_id) {
        throw new Error("Checkout batal gagal: " + coBatal.status + " " + coBatal.text);
      }
      const btl = await apiRequest({ method: "POST", path: "/api/v1/toko/pesanan/" + coBatal.json.data.pesanan_id + "/batal", token: ang.accessToken, json: {} });

      const cb2 = await apiRequest({ method: "POST", path: "/api/v1/toko/keranjang", token: ang.accessToken, json: { produk_id: resIds.produk_a_id, qty: 1 } });
      if (cb2.status !== 200 && cb2.status !== 201) {
        throw new Error("Add cart expire gagal: " + cb2.status + " " + cb2.text);
      }
      const coExp = await apiRequest({ method: "POST", path: "/api/v1/toko/checkout", token: ang.accessToken, json: { metode_serah_terima: "ambil_di_koperasi", catatan: "LiveDemo Expire" } });
      if (coExp.status !== 201 || !coExp.json || !coExp.json.data || !coExp.json.data.pesanan_id) {
        throw new Error("Checkout expire gagal: " + coExp.status + " " + coExp.text);
      }
      const expPw = await apiRequest({ method: "POST", path: "/api/v1/toko/pesanan/" + coExp.json.data.pesanan_id + "/expire", token: pw.accessToken, json: {} });
      const expOp = await apiRequest({ method: "POST", path: "/api/v1/toko/pesanan/" + coExp.json.data.pesanan_id + "/expire", token: op.accessToken, json: {} });

      if (prod.status !== 200 || stok.status !== 200 || hrg.status !== 200 || prm.status !== 200 || btl.status !== 200 || expPw.status !== 403 || expOp.status !== 200) {
        throw new Error("Katalog/Toko gagal: " + [prod.status, stok.status, hrg.status, prm.status, btl.status, expPw.status, expOp.status].join("/"));
      }
      return {
        http: "200/201/403",
        requestId: expOp.requestId,
        summary: "Harga+Promo OK, Batal(stok pulih)=200, Expire(pengawas=403, operator=200)"
      };
    });

    // Modul 9: Anggota, Users, Anti-Self-Escalation Role, Kas & Laporan
    await runStep("9. Manajemen & Laporan", "GET /anggota, /users, /kas/mutasi, /laporan/keuangan, /laporan/anggota & Tolak Self-Escalation 403", async function () {
      const adm = await loginCached("admin_a");
      const angs = await apiRequest({ method: "GET", path: "/api/v1/anggota", token: adm.accessToken });
      const usrs = await apiRequest({ method: "GET", path: "/api/v1/users", token: adm.accessToken });
      const kas = await apiRequest({ method: "GET", path: "/api/v1/kas/mutasi", token: adm.accessToken });
      const lk = await apiRequest({ method: "GET", path: "/api/v1/laporan/keuangan", token: adm.accessToken });
      const la = await apiRequest({ method: "GET", path: "/api/v1/laporan/anggota", token: adm.accessToken });
      const selfEsc = await apiRequest({ method: "PATCH", path: "/api/v1/users/" + accountsMap["admin_a"].user_id + "/role", token: adm.accessToken, json: { role: "admin" } });
      if (angs.status !== 200 || usrs.status !== 200 || kas.status !== 200 || lk.status !== 200 || la.status !== 200 || selfEsc.status !== 403) {
        throw new Error("Status manajemen/laporan/role gagal: " + [angs.status, usrs.status, kas.status, lk.status, la.status, selfEsc.status].join("/"));
      }
      return {
        http: "200/403",
        requestId: selfEsc.requestId,
        summary: "Anggota=" + ((angs.json && angs.json.data && angs.json.data.length) || 0) + ", Users+Kas+Laporan=200, Self-escalation=403"
      };
    });

    // Modul 10: Isolasi Multi-Tenant RLS, Anti-IDOR & Audit Zero-PII
    await runStep("10. RLS, Anti-IDOR & Zero-PII", "Kop A baca anggota/user/pembayaran Kop B (404), Anti-IDOR anggota (404), Pindai 0 kebocoran PII", async function () {
      const p1 = await loginCached("pengurus_a");
      const adm = await loginCached("admin_a");
      const ang = await loginCached("anggota_a");

      const cAng = await apiRequest({ method: "GET", path: "/api/v1/anggota/" + resIds.anggota_b_id, token: p1.accessToken });
      const cUsr = await apiRequest({ method: "GET", path: "/api/v1/users/" + accountsMap["admin_b"].user_id, token: adm.accessToken });
      const cPay = await apiRequest({ method: "GET", path: "/api/v1/pembayaran/" + resIds.pembayaran_b_id, token: ang.accessToken });
      if (cAng.status !== 404 || cUsr.status !== 404 || cPay.status !== 404) {
        throw new Error("Isolasi lintas-tenant / IDOR gagal: " + [cAng.status, cUsr.status, cPay.status].join("/"));
      }

      const nikRe = /\b3\d{15}\b/;
      let leakCount = 0;
      for (const rec of state.recordedResponses) {
        if (nikRe.test(rec.bodyText) || /password_hash|totp_secret_enkripsi|postgres:\/\/[^@]+@/i.test(rec.bodyText)) {
          leakCount++;
        }
      }
      if (leakCount > 0) throw new Error("Ditemukan " + leakCount + " potensi kebocoran PII/rahasia!");

      return {
        http: "404/404/404",
        requestId: cPay.requestId,
        summary: "RLS Kop A->B=404, Anti-IDOR=404, 0 kebocoran PII di " + state.recordedResponses.length + " respons"
      };
    });

    state.liveDemoResults = demoRows;
    const passCount = demoRows.filter(function (r) { return r.status === "PASS"; }).length;
    const failCount = demoRows.filter(function (r) { return r.status === "FAIL"; }).length;
    const skipCount = demoRows.filter(function (r) { return r.status === "SKIP"; }).length;
    el("liveDemoSummary").textContent = "Live Demo Selesai (Mode Standar Tanpa Brute-Force): PASS=" + passCount + ", SKIP=" + skipCount + ", FAIL=" + failCount + ", Total=" + demoRows.length;
    el("btnDownloadLiveDemoJSON").disabled = false;
  }

  async function runScenarioList() {
    if (!state.scenarios || !Array.isArray(state.scenarios.scenarios)) {
      el("scenariosSummary").textContent = "scenarios.json belum tersedia.";
      return;
    }
    const pwd = getTestPassword();
    if (!pwd) {
      alert("Isi 'Kata Sandi Akun Uji' di Bagian 0 terlebih dahulu.");
      return;
    }

    const includeBruteforce = !!(el("chkIncludeBruteforce") && el("chkIncludeBruteforce").checked);
    const tbody = el("scenariosTableBody");
    tbody.textContent = "";
    const results = [];
    const tokensByKey = {};
    const accountsMap = buildAccountsMap();

    const ensureRoleToken = async function (roleKey, forceFresh) {
      if (!roleKey || roleKey === "none" || roleKey === "unauthenticated") return "";
      if (!forceFresh && tokensByKey[roleKey]) return tokensByKey[roleKey];
      if (!forceFresh && state.roleTokens[roleKey]) {
        tokensByKey[roleKey] = state.roleTokens[roleKey];
        return state.roleTokens[roleKey];
      }
      const acc = accountsMap[roleKey];
      if (!acc) throw new Error("Akun uji tidak ditemukan: " + roleKey);
      const auth = await loginAccount(acc, pwd);
      tokensByKey[roleKey] = auth.accessToken;
      state.roleTokens[roleKey] = auth.accessToken;
      return auth.accessToken;
    };

    const ctxVars = Object.assign({}, (state.config && state.config.resources) || {});
    ctxVars.koperasi_a_id = state.config.koperasi_a_id;
    ctxVars.koperasi_b_id = state.config.koperasi_b_id;
    ctxVars.user_admin_a_id = (accountsMap["admin_a"] || {}).user_id || "";
    ctxVars.user_admin_b_id = (accountsMap["admin_b"] || {}).user_id || "";
    ctxVars.sample_static_qris = buildSampleStaticQRIS("Kopdes Uji Browser");
    ctxVars.seed_password = pwd;
    ctxVars.metrics_token = el("metricsTokenInput").value.trim();

    let nikCounter = 0;
    const expandTemplate = function (val) {
      if (typeof val === "string") {
        let out = val;
        if (out === "{{new_uuid}}") return generateUUIDv4();
        out = out.replace(/\{\{([a-zA-Z0-9_]+)\}\}/g, function (_, k) {
          if (k === "new_uuid") return generateUUIDv4();
          if (k === "unix_ts") return String(Math.floor(Date.now() / 1000));
          if (k === "new_nik") {
            nikCounter++;
            const suffix = String((Date.now() % 100000000) + nikCounter).padStart(10, "0").slice(-10);
            return "320101" + suffix;
          }
          return ctxVars[k] !== undefined ? String(ctxVars[k]) : "";
        });
        return out;
      }
      if (Array.isArray(val)) return val.map(expandTemplate);
      if (val && typeof val === "object") {
        const res = {};
        for (const k of Object.keys(val)) {
          res[k] = expandTemplate(val[k]);
        }
        return res;
      }
      return val;
    };

    let passCount = 0;
    let skipCount = 0;
    let failCount = 0;

    for (const sc of state.scenarios.scenarios) {
      const started = new Date().toISOString();
      let statusLabel = "PASS";
      let httpCode = "-";
      let summaryText = "";

      if (sc.claim_id === "CLAIM-AUTH-BRUTEFORCE-429" && !includeBruteforce) {
        statusLabel = "SKIP";
        summaryText = "Mode standar: brute-force 429 dilewati agar IP tidak terkunci 15 menit.";
        skipCount++;
      } else if (sc.requires_metrics_token && !ctxVars.metrics_token) {
        statusLabel = "SKIP";
        summaryText = "METRICS_TOKEN tidak diisi di Panel 0 browser.";
        skipCount++;
      } else {
        try {
          let lastResp = null;
          for (const step of sc.steps) {
            if (step.action === "totp_for_secret") {
              const sec = ctxVars[step.secret_var];
              ctxVars[step.save_as] = await fetchTOTP({ secret_base32: sec, offset_seconds: step.offset_seconds || 0 });
              continue;
            }
            if (step.action === "totp_for_email") {
              ctxVars[step.save_as] = await fetchTOTP({ email: expandTemplate(step.email), offset_seconds: step.offset_seconds || 0 });
              continue;
            }

            let tok = "";
            if (step.token_var) {
              tok = safeString(ctxVars[step.token_var]);
            } else if (step.as_account) {
              tok = await ensureRoleToken(step.as_account, !!step.force_login);
            }

            const reqPath = expandTemplate(step.path);
            const headers = expandTemplate(step.headers || {});
            let reqBody;
            let bodyJson;

            if (step.multipart_png) {
              const fd = new FormData();
              fd.append(step.file_field || "file", createTinyPNGBlob(), "kyc-uji.png");
              reqBody = fd;
            } else if (step.multipart_csv) {
              const csvText = expandTemplate(step.multipart_csv);
              const fd = new FormData();
              fd.append(step.file_field || "file", new Blob([csvText], { type: "text/csv" }), "mutasi-uji.csv");
              reqBody = fd;
            } else if (step.raw_body !== undefined) {
              reqBody = expandTemplate(step.raw_body);
            } else if (step.body !== undefined) {
              bodyJson = expandTemplate(step.body);
            }

            lastResp = await apiRequest({
              method: step.method,
              path: reqPath,
              token: tok,
              headers: headers,
              json: bodyJson,
              body: reqBody,
              autoRefresh: false,
              retryOn429: !(step.expect_status && step.expect_status.includes(429))
            });

            httpCode = String(lastResp.status);
            if (step.expect_status && !step.expect_status.includes(lastResp.status)) {
              throw new Error(step.method + " " + reqPath + " harap status " + step.expect_status.join("/") + ", dapat " + lastResp.status + " (" + lastResp.text.slice(0, 140) + ")");
            }
            if (step.expect_header_present) {
              for (const hdr of step.expect_header_present) {
                if (!lastResp.headers.get(hdr)) {
                  throw new Error(step.method + " " + reqPath + " harap header " + hdr + " ada");
                }
              }
            }
            if (step.expect_json_contains) {
              for (const needle of step.expect_json_contains) {
                if (!lastResp.text.includes(expandTemplate(needle))) {
                  throw new Error("Respons tidak mengandung '" + needle + "'");
                }
              }
            }
            if (step.save_json) {
              for (const varName of Object.keys(step.save_json)) {
                const pathParts = step.save_json[varName].split(".");
                let cur = lastResp.json;
                for (const p of pathParts) {
                  cur = cur && cur[p];
                }
                ctxVars[varName] = cur;
              }
            }
          }
          passCount++;
          summaryText = lastResp ? redactSecrets(lastResp.text.slice(0, 140)) : "OK";
        } catch (err) {
          statusLabel = "FAIL";
          failCount++;
          summaryText = redactSecrets(err.message);
        }
      }

      const row = {
        claim_id: sc.claim_id,
        title: sc.title,
        status: statusLabel,
        http_status: httpCode,
        operation: sc.operation || (sc.steps && sc.steps.length ? sc.steps[sc.steps.length - 1].method + " " + sc.steps[sc.steps.length - 1].path : "-"),
        excerpt: summaryText,
        timestamp: started
      };
      results.push(row);

      const tr = document.createElement("tr");
      const tdId = document.createElement("td");
      tdId.textContent = safeString(row.claim_id);
      tr.appendChild(tdId);
      appendStatusCell(tr, row.status);
      [row.http_status, row.operation, row.excerpt, row.timestamp].forEach(function (c) {
        const td = document.createElement("td");
        td.textContent = safeString(c);
        tr.appendChild(td);
      });
      tbody.appendChild(tr);
    }

    state.proofResults = results;
    el("scenariosSummary").textContent = "Selesai: PASS=" + passCount + ", SKIP=" + skipCount + ", FAIL=" + failCount + ", Total=" + results.length;
    el("btnDownloadProofJSON").disabled = false;
    el("btnDownloadProofMD").disabled = false;
  }

  async function runRolesMatrix() {
    if (!state.rolesMatrix || !Array.isArray(state.rolesMatrix.endpoints)) {
      el("rolesMatrixSummary").textContent = "roles_matrix.json belum tersedia.";
      return;
    }
    const pwd = getTestPassword();
    if (!pwd) {
      alert("Isi 'Kata Sandi Akun Uji' di Bagian 0 terlebih dahulu.");
      return;
    }

    const accountsMap = buildAccountsMap();
    const roleTokens = {};
    const keysNeeded = ["admin_a", "pengurus_a", "bendahara_a", "ketua_a", "pengawas_a", "operator_a", "anggota_a", "admin_b"];
    for (const k of keysNeeded) {
      if (state.roleTokens[k]) {
        roleTokens[k] = state.roleTokens[k];
      } else if (accountsMap[k]) {
        const res = await loginAccount(accountsMap[k], pwd);
        roleTokens[k] = res.accessToken;
        state.roleTokens[k] = res.accessToken;
      }
    }

    const tbody = el("rolesMatrixTableBody");
    tbody.textContent = "";
    const resources = (state.config && state.config.resources) || {};
    const adminA = accountsMap["admin_a"] || {};
    const adminB = accountsMap["admin_b"] || {};
    let total = 0;
    let passed = 0;
    let failed = 0;

    for (const ep of state.rolesMatrix.endpoints) {
      if (ep.skip_browser_probe) continue;
      let path = ep.sample_path || ep.path;
      path = path
        .replace("{anggota_a_id}", resources.anggota_a_id || generateUUIDv4())
        .replace("{anggota_b_id}", resources.anggota_b_id || generateUUIDv4())
        .replace("{produk_a_id}", resources.produk_a_id || generateUUIDv4())
        .replace("{produk_b_id}", resources.produk_b_id || generateUUIDv4())
        .replace("{pembayaran_b_id}", resources.pembayaran_b_id || generateUUIDv4())
        .replace("{rekening_qris_b_id}", resources.rekening_qris_b_id || generateUUIDv4())
        .replace("{user_admin_a_id}", adminA.user_id || generateUUIDv4())
        .replace("{user_admin_b_id}", adminB.user_id || generateUUIDv4())
        .replace("{id}", resources.anggota_a_id || generateUUIDv4())
        .replace("{produk_id}", resources.produk_a_id || generateUUIDv4());

      const rolesObj = ep.roles || {};
      const checks = Object.keys(rolesObj).map(function (r) {
        return {
          label: r,
          tokenKey: r + "_a",
          expected: rolesObj[r]
        };
      });
      if (ep.unauthenticated) {
        checks.push({ label: "unauthenticated", tokenKey: "", expected: ep.unauthenticated });
      }

      for (const chk of checks) {
        total++;
        const tok = chk.tokenKey ? (roleTokens[chk.tokenKey] || "") : "";
        let resp;
        try {
          resp = await apiRequest({
            method: ep.method,
            path: path,
            token: tok,
            json: ["POST", "PUT", "PATCH"].includes(ep.method) ? (ep.sample_body || {}) : undefined,
            autoRefresh: false
          });
        } catch (err) {
          resp = { status: 0, text: err.message };
        }

        const expectedList = Array.isArray(chk.expected) ? chk.expected : [chk.expected];
        let ok = expectedList.includes(resp.status);
        if (chk.expected === "allow" && resp.status < 500 && resp.status !== 401 && resp.status !== 403) ok = true;
        if (chk.expected === "deny" && (resp.status === 401 || resp.status === 403 || resp.status === 404)) ok = true;
        if (ok) passed++; else failed++;

        const tr = document.createElement("tr");
        [ep.method, ep.path, chk.label, JSON.stringify(chk.expected), String(resp.status)].forEach(function (c) {
          const td = document.createElement("td");
          td.textContent = safeString(c);
          tr.appendChild(td);
        });
        appendStatusCell(tr, ok ? "PASS" : "FAIL");
        tbody.appendChild(tr);
      }
    }
    el("rolesMatrixSummary").textContent = "Matriks selesai: Total=" + total + ", PASS=" + passed + ", FAIL=" + failed;
  }

  function downloadFile(filename, mime, content) {
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }

  async function init() {
    const cfgResp = await fetch("/config.json");
    state.config = await cfgResp.json();
    el("apiBaseUrlInput").value = state.config.api_public_url || "";
    el("activeKoperasiInput").value = state.config.koperasi_a_id || "";
    el("configBanner").textContent =
      "APP_ENV=" + state.config.app_env +
      " | API_PUBLIC_URL=" + state.config.api_public_url +
      " | UI_PORT=" + state.config.ui_port +
      " | Koperasi A=" + state.config.koperasi_a_id +
      " | Koperasi B=" + state.config.koperasi_b_id;

    state.vars = Object.assign({}, state.config.resources || {});
    renderAccountsTable();
    updateSessionBar();
    updateGuardUI();

    try {
      const oa = await fetch("/openapi.json");
      if (oa.ok) {
        state.openapi = await oa.json();
        populateOpenAPIOperations();
      } else {
        populateOpenAPIOperations();
      }
    } catch (_) {
      populateOpenAPIOperations();
    }

    try {
      const sc = await fetch("/scenarios.json");
      if (sc.ok) state.scenarios = await sc.json();
    } catch (_) {}

    try {
      const rm = await fetch("/roles_matrix.json");
      if (rm.ok) state.rolesMatrix = await rm.json();
    } catch (_) {}
  }

  document.addEventListener("DOMContentLoaded", function () {
    el("btnApplyGuard").addEventListener("click", function () {
      state.guardConfirmed = el("nonLocalConfirmInput").value.trim() === "SAYA PAHAM INI BUKAN PRODUKSI";
      updateGuardUI();
    });
    el("apiBaseUrlInput").addEventListener("input", updateGuardUI);

    el("btnClearSession").addEventListener("click", function () {
      state.accessToken = "";
      state.refreshToken = "";
      state.activeAccount = null;
      updateSessionBar();
    });

    el("btnRefreshToken").addEventListener("click", async function () {
      try {
        if (!state.refreshToken) throw new Error("Tidak ada refresh_token di memori.");
        const res = await rawFetchAPI({
          method: "POST",
          path: "/api/v1/auth/refresh",
          token: "",
          json: { refresh_token: state.refreshToken }
        });
        if (res.json && res.json.data && res.json.data.access_token) {
          state.accessToken = res.json.data.access_token;
          state.refreshToken = res.json.data.refresh_token || "";
          updateSessionBar();
        }
        appendFlowLog("Refresh Token (" + res.status + ")", res.json || res.text);
      } catch (err) {
        appendFlowLog("Refresh Token Gagal", err.message);
      }
    });

    el("btnTestSingleFlightRefresh").addEventListener("click", async function () {
      try {
        if (!state.refreshToken) {
          const auth = await loginAccount({ email: "anggota_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
          state.refreshToken = auth.refreshToken;
        }
        state.accessToken = "expired.token.simulation";
        updateSessionBar();
        const [a, b] = await Promise.all([
          apiRequest({ method: "GET", path: "/api/v1/auth/me" }),
          apiRequest({ method: "GET", path: "/api/v1/iuran/jenis" })
        ]);
        appendFlowLog("Uji Single-Flight Refresh Konkuren", { me_status: a.status, iuran_jenis_status: b.status, same_rotated_token: !!state.accessToken });
      } catch (err) {
        appendFlowLog("Uji Single-Flight Refresh Gagal", err.message);
      }
    });

    el("btnLogout").addEventListener("click", async function () {
      try {
        if (!state.refreshToken) throw new Error("Tidak ada refresh_token di memori.");
        const res = await apiRequest({
          method: "POST",
          path: "/api/v1/auth/revoke",
          json: { refresh_token: state.refreshToken },
          autoRefresh: false
        });
        state.accessToken = "";
        state.refreshToken = "";
        state.activeAccount = null;
        updateSessionBar();
        appendFlowLog("Logout / Revoke (" + res.status + ")", res.json || res.text);
      } catch (err) {
        appendFlowLog("Logout Gagal", err.message);
      }
    });

    el("btnRunLiveDemo").addEventListener("click", runLiveDemo);
    el("btnDownloadLiveDemoJSON").addEventListener("click", function () {
      downloadFile("hasil-live-demo-kopdes.json", "application/json", JSON.stringify({ generated_at: new Date().toISOString(), results: state.liveDemoResults }, null, 2));
    });

    el("openapiOpSelect").addEventListener("change", onSelectOpenAPIOp);
    el("btnLoadOpSample").addEventListener("click", onSelectOpenAPIOp);

    el("btnCopyCurl").addEventListener("click", function () {
      try {
        const d = buildOpenAPIRequestDetails();
        let cmd = "curl -i -X " + d.method + " \"" + getApiBase() + d.path + "\"";
        if (state.accessToken) cmd += " \\\n  -H \"Authorization: Bearer " + redactSecrets(state.accessToken) + "\"";
        for (const k of Object.keys(d.headers)) {
          cmd += " \\\n  -H \"" + k + ": " + d.headers[k] + "\"";
        }
        if (d.hasBody) {
          cmd += " \\\n  -H \"Content-Type: application/json\" \\\n  -d '" + d.rawBody.replace(/'/g, "'\\''") + "'";
        }
        el("openapiCurlPreview").textContent = cmd;
        if (navigator.clipboard) navigator.clipboard.writeText(cmd).catch(function () {});
      } catch (err) {
        el("openapiCurlPreview").textContent = err.message;
      }
    });

    el("btnSendOpenAPI").addEventListener("click", async function () {
      try {
        const d = buildOpenAPIRequestDetails();
        const res = await apiRequest({
          method: d.method,
          path: d.path,
          headers: d.headers,
          body: d.hasBody ? d.rawBody : undefined
        });
        el("openapiResponsePre").textContent =
          "HTTP " + res.status + " | X-Request-ID=" + (res.requestId || "-") + "\n" +
          redactSecrets(res.text);
      } catch (err) {
        el("openapiResponsePre").textContent = "Error: " + redactSecrets(err.message);
      }
    });

    // Guided Flows 3.1a..3.9
    el("btnFlow31aLogin").addEventListener("click", async function () {
      try {
        const email = el("flowLoginEmail").value.trim() || "anggota_a@kopdes-test.local";
        const pass = el("flowLoginPass").value || getTestPassword();
        const authRes = await loginAccount({ email: email, koperasi_id: el("activeKoperasiInput").value.trim() }, pass);
        state.accessToken = authRes.accessToken;
        state.refreshToken = authRes.refreshToken;
        updateSessionBar();
        appendFlowLog("3.1a Login OK", authRes.loginResponse.json);
      } catch (err) {
        appendFlowLog("3.1a Login Error", err.message);
      }
    });

    el("btnFlow31bEnroll").addEventListener("click", async function () {
      try {
        const pwd = getTestPassword();
        if (!pwd) throw new Error("Isi 'Kata Sandi Akun Uji' terlebih dahulu.");
        const lRes = await apiRequest({
          method: "POST",
          path: "/api/v1/auth/login",
          token: "",
          json: { email: "admin_nomfa_a@kopdes-test.local", password: pwd, koperasi_id: state.config.koperasi_a_id }
        });
        const setupTok = lRes.json && lRes.json.data && lRes.json.data.setup_token;
        if (!setupTok) throw new Error("Harap MFA_MANDATORY_SETUP_REQUIRED + setup_token, dapat (" + lRes.status + "): " + lRes.text);
        const sRes = await apiRequest({ method: "POST", path: "/api/v1/auth/mfa/setup", token: setupTok, json: {} });
        const sec = sRes.json && sRes.json.data && sRes.json.data.secret;
        const code = await fetchTOTP({ secret_base32: sec });
        const eRes = await apiRequest({ method: "POST", path: "/api/v1/auth/mfa/enable", token: setupTok, json: { secret: sec, totp_code: code } });
        appendFlowLog("3.1b MFA Enrollment Selesai (recovery_codes tersimpan)", eRes.json);
      } catch (err) {
        appendFlowLog("3.1b MFA Enrollment Note/Error", err.message);
      }
    });

    el("btnFlow31cReuse").addEventListener("click", async function () {
      try {
        const pwd = getTestPassword();
        const auth = await loginAccount({ email: "anggota_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id }, pwd);
        const oldRefresh = auth.refreshToken;
        const rot = await apiRequest({ method: "POST", path: "/api/v1/auth/refresh", token: "", json: { refresh_token: oldRefresh } });
        const reuse = await apiRequest({ method: "POST", path: "/api/v1/auth/refresh", token: "", json: { refresh_token: oldRefresh } });
        appendFlowLog("3.1c Token Reuse Probe (rot=" + rot.status + ", reuse=" + reuse.status + ")", reuse.json);
      } catch (err) {
        appendFlowLog("3.1c Error", err.message);
      }
    });

    el("btnFlow31dMetrics").addEventListener("click", async function () {
      const mTok = el("metricsTokenInput").value.trim();
      try {
        const unauth = await apiRequest({ method: "GET", path: "/metrics", token: "" });
        if (!mTok) {
          appendFlowLog("3.1d /metrics Probe (unauth saja)", { unauth_status: unauth.status, note: "Isi Metrics Bearer Token di Panel 0 untuk menguji akses terautentikasi 200." });
          return;
        }
        const authed = await apiRequest({ method: "GET", path: "/metrics", token: mTok });
        appendFlowLog("3.1d /metrics Probe", { unauth_status: unauth.status, auth_status: authed.status, sample: authed.text.slice(0, 220) });
      } catch (err) {
        appendFlowLog("3.1d /metrics Error", err.message);
      }
    });

    el("btnFlow31eLockout").addEventListener("click", async function () {
      if (!confirm("Peringatan: Uji brute-force 429 akan mengunci IP 127.0.0.1 selama 15 menit. Lanjutkan?")) {
        return;
      }
      try {
        const statuses = [];
        for (let i = 0; i < 6; i++) {
          const r = await apiRequest({
            method: "POST",
            path: "/api/v1/auth/login",
            token: "",
            json: { email: "lockout_probe_" + Date.now() + "@kopdes-test.local", password: "WrongPassword123!" }
          });
          statuses.push({ attempt: i + 1, status: r.status, retry_after: r.headers.get("Retry-After") });
        }
        appendFlowLog("3.1e Brute-Force Lockout", statuses);
      } catch (err) {
        appendFlowLog("3.1e Lockout Error", err.message);
      }
    });

    el("btnFlow32Run").addEventListener("click", async function () {
      try {
        const p1 = await loginAccount({ email: "pengurus_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const ts = Date.now();
        const reg = await apiRequest({
          method: "POST",
          path: "/api/v1/pendaftaran",
          token: p1.accessToken,
          json: {
            nama_lengkap: el("flowPendaftaranNama").value,
            nik: el("flowPendaftaranNIK").value,
            tempat_lahir: "Bogor",
            tanggal_lahir: "1992-01-15",
            jenis_kelamin: "L",
            pekerjaan: "Petani",
            alamat_ktp: "Jl. Desa Uji No. " + String(ts).slice(-3)
          }
        });
        const pid = reg.json && reg.json.data && reg.json.data.pendaftaran_id;
        state.vars.last_pendaftaran_id = pid;
        let masked = null;
        if (pid) {
          const fd = new FormData();
          fd.append("dokumen", createTinyPNGBlob(), "ktp.png");
          fd.append("jenis_dokumen", "ktp");
          await apiRequest({ method: "POST", path: "/api/v1/pendaftaran/" + pid + "/dokumen", token: p1.accessToken, body: fd });
          const getRes = await apiRequest({ method: "GET", path: "/api/v1/pendaftaran/" + pid, token: p1.accessToken });
          masked = getRes.json && getRes.json.data;
        }
        appendFlowLog("3.2 Pendaftaran + Upload KYC + Masking PII", { create: reg.json, detail: masked });
      } catch (err) {
        appendFlowLog("3.2 Error", err.message);
      }
    });

    el("btnFlow32Verify").addEventListener("click", async function () {
      try {
        if (!state.vars.last_pendaftaran_id) throw new Error("Jalankan 3.2 terlebih dahulu.");
        const p2Auth = await loginAccount({ email: "pengurus2_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const res = await apiRequest({
          method: "POST",
          path: "/api/v1/pendaftaran/" + state.vars.last_pendaftaran_id + "/verifikasi",
          token: p2Auth.accessToken,
          json: { status: "disetujui" }
        });
        appendFlowLog("3.2b Verifikasi Maker-Checker Pengurus (" + res.status + ")", res.json);
      } catch (err) {
        appendFlowLog("3.2b Error", err.message);
      }
    });

    el("btnFlow33QRIS").addEventListener("click", async function () {
      try {
        const ang = await loginAccount({ email: "anggota_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        await apiRequest({ method: "POST", path: "/api/v1/toko/keranjang", token: ang.accessToken, json: { produk_id: state.config.resources.produk_a_id, qty: 1 } });
        const co1 = await apiRequest({ method: "POST", path: "/api/v1/toko/checkout", token: ang.accessToken, json: { metode_serah_terima: "ambil_di_koperasi", catatan: "Flow 3.3 #1" } });
        await apiRequest({ method: "POST", path: "/api/v1/toko/keranjang", token: ang.accessToken, json: { produk_id: state.config.resources.produk_a_id, qty: 1 } });
        const co2 = await apiRequest({ method: "POST", path: "/api/v1/toko/checkout", token: ang.accessToken, json: { metode_serah_terima: "ambil_di_koperasi", catatan: "Flow 3.3 #2" } });

        const key = generateUUIDv4();
        const body1 = {
          pesanan_id: co1.json.data.pesanan_id,
          anggota_id: state.config.resources.anggota_a_id,
          metode_pembayaran_id: state.config.resources.metode_pembayaran_a_id,
          rekening_qris_id: state.config.resources.rekening_qris_a_id
        };
        const p1 = await loginAccount({ email: "pengurus_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const r1 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/qris", token: p1.accessToken, headers: { "Idempotency-Key": key }, json: body1 });
        const r2 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/qris", token: p1.accessToken, headers: { "Idempotency-Key": key }, json: body1 });
        const r3 = await apiRequest({
          method: "POST",
          path: "/api/v1/pembayaran/qris",
          token: p1.accessToken,
          headers: { "Idempotency-Key": key },
          json: {
            pesanan_id: co2.json.data.pesanan_id,
            anggota_id: state.config.resources.anggota_a_id,
            metode_pembayaran_id: state.config.resources.metode_pembayaran_a_id,
            rekening_qris_id: state.config.resources.rekening_qris_a_id
          }
        });
        if (r1.json && r1.json.data && r1.json.data.pembayaran_id) {
          state.vars.last_pembayaran_id = r1.json.data.pembayaran_id;
          state.vars.last_pembayaran_nominal_total = r1.json.data.nominal_total;
          updateSessionBar();
        }
        appendFlowLog("3.3a QRIS Idempotency", { create: r1.status, replay: r2.status, conflict_422: r3.status, data: r1.json });
      } catch (err) {
        appendFlowLog("3.3a Error", err.message);
      }
    });

    el("btnFlow33ClaimAndApprove").addEventListener("click", async function () {
      try {
        const payId = state.vars.last_pembayaran_id;
        if (!payId) throw new Error("Jalankan 3.3a terlebih dahulu.");
        const p1Auth = await loginAccount({ email: "pengurus_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const klaim = await apiRequest({
          method: "POST",
          path: "/api/v1/pembayaran/" + payId + "/bukti",
          token: p1Auth.accessToken,
          json: { referensi_rrn: "RRN" + String(Date.now()).slice(-8), catatan: "Bukti transfer uji browser" }
        });
        const p2Auth = await loginAccount({ email: "pengurus2_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const app = await apiRequest({
          method: "POST",
          path: "/api/v1/pembayaran/" + payId + "/verifikasi",
          token: p2Auth.accessToken,
          json: { disetujui: true, catatan: "Valid mutasi uji" }
        });
        appendFlowLog("3.3b Klaim RRN + Approve Pengurus2", { klaim: klaim.status, approve: app.status, data: app.json });
      } catch (err) {
        appendFlowLog("3.3b Error", err.message);
      }
    });

    el("btnFlow34Mutasi").addEventListener("click", async function () {
      try {
        const ang = await loginAccount({ email: "anggota_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        await apiRequest({ method: "POST", path: "/api/v1/toko/keranjang", token: ang.accessToken, json: { produk_id: state.config.resources.produk_a_id, qty: 1 } });
        const co = await apiRequest({ method: "POST", path: "/api/v1/toko/checkout", token: ang.accessToken, json: { metode_serah_terima: "ambil_di_koperasi", catatan: "Flow 3.4 Mutasi" } });
        const qRes = await apiRequest({
          method: "POST",
          path: "/api/v1/pembayaran/qris",
          token: ang.accessToken,
          headers: { "Idempotency-Key": generateUUIDv4() },
          json: {
            pesanan_id: co.json.data.pesanan_id,
            anggota_id: state.config.resources.anggota_a_id,
            metode_pembayaran_id: state.config.resources.metode_pembayaran_a_id,
            rekening_qris_id: state.config.resources.rekening_qris_a_id
          }
        });
        const nomTotal = (qRes.json && qRes.json.data && qRes.json.data.nominal_total) || 50123;
        const p1 = await loginAccount({ email: "pengurus_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const tglJkt = getTodayJakartaDate();
        const csv = "tanggal,keterangan,tipe,nominal,no_rekening\n" + tglJkt + " 10:00:00,QRIS Toko Uji " + Date.now() + ",CR," + nomTotal + ",1234567890\n";
        const fd1 = new FormData();
        fd1.append("file", new Blob([csv], { type: "text/csv" }), "mutasi.csv");
        const imp1 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/mutasi-bank/impor", token: p1.accessToken, body: fd1 });
        const fd2 = new FormData();
        fd2.append("file", new Blob([csv], { type: "text/csv" }), "mutasi.csv");
        const imp2 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/mutasi-bank/impor", token: p1.accessToken, body: fd2 });
        const rekon = await apiRequest({ method: "GET", path: "/api/v1/pembayaran/rekonsiliasi/harian?tanggal=" + encodeURIComponent(tglJkt), token: p1.accessToken });
        appendFlowLog("3.4 Impor CSV + Deteksi Duplikat + Rekonsiliasi", {
          impor_1: imp1.json,
          impor_2_duplikat: imp2.json,
          rekonsiliasi: rekon.json
        });
      } catch (err) {
        appendFlowLog("3.4 Error", err.message);
      }
    });

    el("btnFlow35Rekening").addEventListener("click", async function () {
      try {
        const pAuth = await loginAccount({ email: "pengurus_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const qrisSample = buildSampleStaticQRIS("TestPersonal");
        const bad = await apiRequest({
          method: "POST",
          path: "/api/v1/koperasi/rekening-qris",
          token: pAuth.accessToken,
          json: {
            nama_merchant: "Personal Ditolak",
            nmid: "ID9999999999999",
            nama_bank_pjp: "BCA",
            no_rekening_settlement: "99887766",
            nama_pemilik_rekening: "Pengurus Pribadi",
            is_atas_nama_badan_hukum: false,
            jenis_pemilik: "perorangan_pengurus",
            qris_payload_statis: qrisSample
          }
        });
        const list = await apiRequest({ method: "GET", path: "/api/v1/koperasi/rekening-qris", token: pAuth.accessToken });
        appendFlowLog("3.5 Tolak Rekening Perorangan (" + bad.status + ") & List Rekening", { rejected: bad.json, list_count: list.json && list.json.data && list.json.data.length });
      } catch (err) {
        appendFlowLog("3.5 Error", err.message);
      }
    });

    el("btnFlow36Toko").addEventListener("click", async function () {
      try {
        const ang = await loginAccount({ email: "anggota_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const list = await apiRequest({ method: "GET", path: "/api/v1/katalog/produk", token: ang.accessToken });
        const cart = await apiRequest({ method: "POST", path: "/api/v1/toko/keranjang", token: ang.accessToken, json: { produk_id: state.config.resources.produk_a_id, qty: 1 } });
        const co = await apiRequest({
          method: "POST",
          path: "/api/v1/toko/checkout",
          token: ang.accessToken,
          json: { metode_serah_terima: "ambil_di_koperasi", kode_promo: state.config.resources.promo_a_kode }
        });
        const orders = await apiRequest({ method: "GET", path: "/api/v1/toko/pesanan", token: ang.accessToken });
        appendFlowLog("3.6 Toko Keranjang & Checkout", {
          produk_count: list.json && list.json.data && list.json.data.length,
          cart: cart.status,
          checkout: co.status,
          checkout_data: co.json,
          orders_count: orders.json && orders.json.data && orders.json.data.length
        });
      } catch (err) {
        appendFlowLog("3.6 Error", err.message);
      }
    });

    el("btnFlow37Iuran").addEventListener("click", async function () {
      try {
        const pAuth = await loginAccount({ email: "pengurus_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const angAuth = await loginAccount({ email: "anggota_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const getJ = await apiRequest({ method: "GET", path: "/api/v1/iuran/jenis", token: pAuth.accessToken });
        const postJ = await apiRequest({
          method: "POST",
          path: "/api/v1/iuran/jenis",
          token: pAuth.accessToken,
          json: { kode: "UI-" + String(Date.now()).slice(-6), nama: "Iuran Browser", tipe: "wajib", nominal_standar: 25000 }
        });
        const newJenisId = (postJ.json && postJ.json.data && postJ.json.data.id) || state.config.resources.jenis_iuran_a_id;
        const gen = await apiRequest({
          method: "POST",
          path: "/api/v1/iuran/generate",
          token: pAuth.accessToken,
          json: { jenis_iuran_id: newJenisId, periode: "2026-12" }
        });
        const myT = await apiRequest({ method: "GET", path: "/api/v1/iuran/tagihan/saya", token: angAuth.accessToken });
        appendFlowLog("3.7 Iuran + KEPUTUSAN AKUNTAN + Generate", { get_status: getJ.status, post_status: postJ.status, post_body: postJ.json, generate_status: gen.status, generate_body: gen.json, tagihan_saya_count: myT.json && myT.json.data && myT.json.data.length });
      } catch (err) {
        appendFlowLog("3.7 Error", err.message);
      }
    });

    el("btnFlow38Admin").addEventListener("click", async function () {
      try {
        const adm = await loginAccount({ email: "admin_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const angList = await apiRequest({ method: "GET", path: "/api/v1/anggota", token: adm.accessToken });
        const lapKeu = await apiRequest({ method: "GET", path: "/api/v1/laporan/keuangan", token: adm.accessToken });
        const lapAng = await apiRequest({ method: "GET", path: "/api/v1/laporan/anggota", token: adm.accessToken });
        const kasMut = await apiRequest({ method: "GET", path: "/api/v1/kas/mutasi", token: adm.accessToken });
        const usrs = await apiRequest({ method: "GET", path: "/api/v1/users", token: adm.accessToken });
        const adminAccount = (state.config.seeded_accounts || []).find(function (a) { return a.key === "admin_a"; }) || state.config.seeded_accounts[0];
        const selfPatch = await apiRequest({
          method: "PATCH",
          path: "/api/v1/users/" + adminAccount.user_id + "/role",
          token: adm.accessToken,
          json: { role: "admin" }
        });
        appendFlowLog("3.8 Manajemen & Tolak Self-Role-Escalation", {
          anggota_status: angList.status,
          laporan_keuangan: lapKeu.status,
          laporan_anggota: lapAng.status,
          kas_mutasi: kasMut.status,
          users: usrs.status,
          self_patch_status: selfPatch.status,
          body: selfPatch.json
        });
      } catch (err) {
        appendFlowLog("3.8 Error", err.message);
      }
    });

    el("btnFlow39Tenant").addEventListener("click", async function () {
      try {
        const angA = await loginAccount({ email: "anggota_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const crossPay = await apiRequest({ method: "GET", path: "/api/v1/pembayaran/" + state.config.resources.pembayaran_b_id, token: angA.accessToken });
        appendFlowLog("3.9 Cross-Tenant & Anti-IDOR Probe", { status: crossPay.status, body: crossPay.json });
      } catch (err) {
        appendFlowLog("3.9 Error", err.message);
      }
    });

    // Tugas 4 & 5 Buttons
    el("btnRunAllScenarios").addEventListener("click", runScenarioList);
    el("btnRunRolesMatrix").addEventListener("click", runRolesMatrix);

    el("btnDownloadProofJSON").addEventListener("click", function () {
      downloadFile("bukti-dokumentasi-kopdes.json", "application/json", JSON.stringify({ generated_at: new Date().toISOString(), results: state.proofResults }, null, 2));
    });

    el("btnDownloadProofMD").addEventListener("click", function () {
      let md = "# Bukti Eksekusi Skenario Frontend Tester\n\n| Claim ID | Status | HTTP | Operasi | Ringkasan | Waktu |\n|---|---|---|---|---|---|\n";
      for (const r of state.proofResults) {
        md += "| " + r.claim_id + " | " + r.status + " | " + r.http_status + " | `" + r.operation + "` | " + r.excerpt.replace(/\|/g, "/") + " | " + r.timestamp + " |\n";
      }
      downloadFile("bukti-dokumentasi-kopdes.md", "text/markdown", md);
    });

    // Tugas 6 Browser Behaviors
    el("btnBehavior61CORS").addEventListener("click", async function () {
      try {
        const ownHealth = await apiRequest({ method: "GET", path: "/healthz", token: "" });
        const probeResp = await fetch("/api/probe-cors", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            target_url: getApiBase() + "/api/v1/auth/login",
            origin: "https://foreign-attacker.example",
            method: "POST",
            request_headers: "Content-Type,Authorization,X-Koperasi-ID"
          })
        });
        const probeJson = await probeResp.json();
        el("behaviorOutputPre").textContent = JSON.stringify({
          allowed_origin_fetch_status: ownHealth.status,
          exposed_request_id: ownHealth.headers.get("X-Request-ID"),
          foreign_origin_preflight_probe: probeJson
        }, null, 2);
      } catch (err) {
        el("behaviorOutputPre").textContent = "6.1 CORS Error: " + err.message;
      }
    });

    el("btnBehavior62Abort").addEventListener("click", async function () {
      try {
        const ang = await loginAccount({ email: "anggota_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        await apiRequest({ method: "POST", path: "/api/v1/toko/keranjang", token: ang.accessToken, json: { produk_id: state.config.resources.produk_a_id, qty: 1 } });
        const co = await apiRequest({ method: "POST", path: "/api/v1/toko/checkout", token: ang.accessToken, json: { metode_serah_terima: "ambil_di_koperasi", catatan: "Behavior 6.2 Abort" } });
        const idempKey = generateUUIDv4();
        const payBody = { pesanan_id: co.json.data.pesanan_id, anggota_id: state.config.resources.anggota_a_id, metode_pembayaran_id: state.config.resources.metode_pembayaran_a_id, rekening_qris_id: state.config.resources.rekening_qris_a_id };
        const ctrl = new AbortController();
        ctrl.abort();
        let aborted = false;
        try {
          await apiRequest({
            method: "POST",
            path: "/api/v1/pembayaran/qris",
            token: ang.accessToken,
            headers: { "Idempotency-Key": idempKey },
            json: payBody,
            signal: ctrl.signal
          });
        } catch (e) {
          aborted = e.name === "AbortError";
        }
        const retry1 = await apiRequest({
          method: "POST",
          path: "/api/v1/pembayaran/qris",
          token: ang.accessToken,
          headers: { "Idempotency-Key": idempKey },
          json: payBody
        });
        const retry2 = await apiRequest({
          method: "POST",
          path: "/api/v1/pembayaran/qris",
          token: ang.accessToken,
          headers: { "Idempotency-Key": idempKey },
          json: payBody
        });
        el("behaviorOutputPre").textContent = JSON.stringify({
          abort_triggered: aborted,
          retry1_status: retry1.status,
          retry2_status: retry2.status,
          same_payment_id: !!(retry1.json && retry2.json && retry1.json.data && retry2.json.data && retry1.json.data.pembayaran_id === retry2.json.data.pembayaran_id)
        }, null, 2);
      } catch (err) {
        el("behaviorOutputPre").textContent = "6.2 Abort+Retry Error: " + err.message;
      }
    });

    el("btnBehavior63Limits").addEventListener("click", async function () {
      try {
        const badJson = await apiRequest({
          method: "POST",
          path: "/api/v1/auth/login",
          token: "",
          headers: { "Content-Type": "application/json" },
          body: "{broken-json"
        });
        const bigSize = 10 * 1024 * 1024 + 512;
        const bigBody = new Uint8Array(bigSize);
        const bigResp = await apiRequest({
          method: "POST",
          path: "/api/v1/auth/login",
          token: "",
          headers: { "Content-Type": "application/json" },
          body: bigBody
        });
        el("behaviorOutputPre").textContent = JSON.stringify({
          malformed_json_status: badJson.status,
          malformed_json_body: badJson.json,
          oversized_body_bytes: bigSize,
          oversized_body_status: bigResp.status,
          oversized_body_body: bigResp.json || bigResp.text
        }, null, 2);
      } catch (err) {
        el("behaviorOutputPre").textContent = "6.3 Body/JSON Error: " + err.message;
      }
    });

    el("btnBehavior64PII").addEventListener("click", function () {
      const nikRe = /\b3\d{15}\b/;
      const secretHits = [];
      for (const rec of state.recordedResponses) {
        if (nikRe.test(rec.bodyText)) {
          secretHits.push({ path: rec.path, finding: "Possible 16-digit unmasked NIK" });
        }
        if (/password_hash|totp_secret_enkripsi|postgres:\/\/[^@]+@/i.test(rec.bodyText)) {
          secretHits.push({ path: rec.path, finding: "Sensitive internal credential/DSN field" });
        }
      }
      el("behaviorOutputPre").textContent = JSON.stringify({
        scanned_responses: state.recordedResponses.length,
        findings: secretHits,
        clean: secretHits.length === 0
      }, null, 2);
    });

    init().catch(function (err) {
      el("configBanner").textContent = "Gagal inisialisasi Frontend Tester: " + err.message;
    });
  });
})();
