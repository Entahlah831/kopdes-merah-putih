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
    activeAccount: null,
    lastIdempotencyKey: "",
    vars: {},
    recordedResponses: [],
    proofResults: [],
    roleTokens: {}
  };

  function el(id) {
    return document.getElementById(id);
  }

  function safeString(v) {
    return v === null || v === undefined ? "" : String(v);
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
    const len2 = String(m.length).padStart(2, "0");
    const base =
      "00020101021126570011ID.CO.QRIS.WWW01189360052300000123450215ID10203040506070303UMI5204541153033605802ID59" +
      len2 + m + "6007Jakarta6304";
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
    el("varsDumpPre").textContent = redactSecrets(JSON.stringify(state.vars, null, 2));
  }

  function recordTrace(entry) {
    state.recordedResponses.push({
      time: new Date().toISOString(),
      method: entry.method,
      path: entry.path,
      status: entry.status,
      bodyText: redactSecrets(entry.bodyText)
    });
  }

  function appendFlowLog(title, detail) {
    const pre = el("flowOutputPre");
    const stamp = new Date().toISOString();
    const msg = "[" + stamp + "] " + title + "\n" + redactSecrets(typeof detail === "string" ? detail : JSON.stringify(detail, null, 2)) + "\n\n";
    pre.textContent = msg + pre.textContent;
  }

  async function apiRequest(opts) {
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
    let parsed = null;
    try {
      parsed = JSON.parse(text);
    } catch (_) {
      parsed = null;
    }
    recordTrace({ method: method, path: rawPath, status: resp.status, bodyText: text });
    return {
      status: resp.status,
      headers: resp.headers,
      text: text,
      json: parsed
    };
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
      const code = await fetchTOTP({ email: account.email });
      const verifyRes = await apiRequest({
        method: "POST",
        path: "/api/v1/auth/mfa/verify",
        token: "",
        json: {
          mfa_session_token: res.json.data.mfa_session_token,
          totp_code: code
        }
      });
      if (verifyRes.status === 200 && verifyRes.json && verifyRes.json.data && verifyRes.json.data.access_token) {
        return {
          accessToken: verifyRes.json.data.access_token,
          refreshToken: verifyRes.json.data.refresh_token || "",
          loginResponse: verifyRes
        };
      }
      throw new Error("MFA verify gagal (" + verifyRes.status + "): " + verifyRes.text);
    }

    throw new Error("Login gagal (" + res.status + "): " + res.text);
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
      opt.textContent = "(openapi.json belum tersedia — jalankan scripts/generate_api_artifacts.py pada Tugas 2)";
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

    const tbody = el("scenariosTableBody");
    tbody.textContent = "";
    const results = [];
    const tokensByKey = {};
    const accountsMap = {};
    for (const a of (state.config.seeded_accounts || [])) {
      accountsMap[a.key] = a;
    }

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

    const expandTemplate = function (val) {
      if (typeof val === "string") {
        let out = val;
        if (out === "{{new_uuid}}") return generateUUIDv4();
        out = out.replace(/\{\{([a-zA-Z0-9_]+)\}\}/g, function (_, k) {
          if (k === "new_uuid") return generateUUIDv4();
          if (k === "unix_ts") return String(Math.floor(Date.now() / 1000));
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

      if (sc.requires_metrics_token && !ctxVars.metrics_token) {
        statusLabel = "SKIP";
        summaryText = "METRICS_TOKEN tidak diisi di Panel 0 browser.";
        skipCount++;
      } else {
        try {
          let lastResp = null;
          for (const step of sc.steps) {
            if (step.action === "totp_for_secret") {
              const sec = ctxVars[step.secret_var];
              ctxVars[step.save_as] = await fetchTOTP({ secret_base32: sec });
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
            } else if (step.body !== undefined) {
              bodyJson = expandTemplate(step.body);
            }

            lastResp = await apiRequest({
              method: step.method,
              path: reqPath,
              token: tok,
              headers: headers,
              json: bodyJson,
              body: reqBody
            });

            httpCode = String(lastResp.status);
            if (step.expect_status && !step.expect_status.includes(lastResp.status)) {
              throw new Error(step.method + " " + reqPath + " harap status " + step.expect_status.join("/") + ", dapat " + lastResp.status + " (" + lastResp.text.slice(0, 140) + ")");
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
      [row.claim_id, row.status, row.http_status, row.operation, row.excerpt, row.timestamp].forEach(function (c) {
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

    const accountsMap = {};
    for (const a of (state.config.seeded_accounts || [])) {
      accountsMap[a.role + "_" + a.tenant.toLowerCase()] = a;
      accountsMap[a.key] = a;
    }
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
            json: ["POST", "PUT", "PATCH"].includes(ep.method) ? (ep.sample_body || {}) : undefined
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
        [ep.method, ep.path, chk.label, JSON.stringify(chk.expected), String(resp.status), ok ? "PASS" : "FAIL"].forEach(function (c) {
          const td = document.createElement("td");
          td.textContent = safeString(c);
          tr.appendChild(td);
        });
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
        const res = await apiRequest({
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

    el("btnLogout").addEventListener("click", async function () {
      try {
        if (!state.refreshToken) throw new Error("Tidak ada refresh_token di memori.");
        const res = await apiRequest({
          method: "POST",
          path: "/api/v1/auth/revoke",
          json: { refresh_token: state.refreshToken }
        });
        state.accessToken = "";
        state.refreshToken = "";
        updateSessionBar();
        appendFlowLog("Logout / Revoke (" + res.status + ")", res.json || res.text);
      } catch (err) {
        appendFlowLog("Logout Gagal", err.message);
      }
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
          "HTTP " + res.status + " | X-Request-ID=" + (res.headers.get("X-Request-ID") || "-") + "\n" +
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
        if (!setupTok) throw new Error("Harap MFA_MANDATORY_SETUP_REQUIRED + setup_token, dapat: " + lRes.text);
        const sRes = await apiRequest({ method: "POST", path: "/api/v1/auth/mfa/setup", token: setupTok, json: {} });
        const sec = sRes.json && sRes.json.data && sRes.json.data.secret;
        const code = await fetchTOTP({ secret_base32: sec });
        const eRes = await apiRequest({ method: "POST", path: "/api/v1/auth/mfa/enable", token: setupTok, json: { secret: sec, totp_code: code } });
        appendFlowLog("3.1b MFA Enrollment Selesai (recovery_codes tersimpan)", eRes.json);
      } catch (err) {
        appendFlowLog("3.1b MFA Enrollment Error", err.message);
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
      if (!mTok) {
        appendFlowLog("3.1d /metrics dilewati", "Isi Metrics Bearer Token di Panel 0 terlebih dahulu.");
        return;
      }
      try {
        const unauth = await apiRequest({ method: "GET", path: state.config.metrics_url, token: "" });
        const authed = await apiRequest({ method: "GET", path: state.config.metrics_url, token: mTok });
        appendFlowLog("3.1d /metrics Probe", { unauth_status: unauth.status, auth_status: authed.status, sample: authed.text.slice(0, 200) });
      } catch (err) {
        appendFlowLog("3.1d /metrics note (port metrik berbeda dari origin CORS API)", err.message);
      }
    });

    el("btnFlow31eLockout").addEventListener("click", async function () {
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
        const ts = Date.now();
        const reg = await apiRequest({
          method: "POST",
          path: "/api/v1/pendaftaran",
          json: {
            nama_lengkap: el("flowPendaftaranNama").value,
            nik: el("flowPendaftaranNIK").value,
            no_hp: "0812345" + String(ts).slice(-5),
            alamat_ktp: "Jl. Desa Uji No. " + String(ts).slice(-3),
            tempat_lahir: "Bogor",
            tanggal_lahir: "1992-01-15",
            jenis_kelamin: "L"
          }
        });
        const pid = reg.json && reg.json.data && reg.json.data.pendaftaran_id;
        state.vars.last_pendaftaran_id = pid;
        let masked = null;
        if (pid) {
          const fd = new FormData();
          fd.append("dokumen", createTinyPNGBlob(), "ktp.png");
          fd.append("jenis_dokumen", "ktp");
          await apiRequest({ method: "POST", path: "/api/v1/pendaftaran/" + pid + "/dokumen", body: fd });
          const getRes = await apiRequest({ method: "GET", path: "/api/v1/pendaftaran/" + pid });
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
        appendFlowLog("3.2b Verifikasi Maker-Checker (" + res.status + ")", res.json);
      } catch (err) {
        appendFlowLog("3.2b Error", err.message);
      }
    });

    el("btnFlow33QRIS").addEventListener("click", async function () {
      try {
        const key = generateUUIDv4();
        const body = {
          tagihan_iuran_id: state.config.resources.tagihan_iuran_a_id,
          rekening_qris_id: state.config.resources.rekening_qris_a_id
        };
        const r1 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/qris", headers: { "Idempotency-Key": key }, json: body });
        const r2 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/qris", headers: { "Idempotency-Key": key }, json: body });
        const r3 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/qris", headers: { "Idempotency-Key": key }, json: { tagihan_iuran_id: state.config.resources.tagihan_iuran_a2_id, rekening_qris_id: state.config.resources.rekening_qris_a_id } });
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
        const klaim = await apiRequest({
          method: "POST",
          path: "/api/v1/pembayaran/" + payId + "/bukti",
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
        const qRes = await apiRequest({
          method: "POST",
          path: "/api/v1/pembayaran/qris",
          token: ang.accessToken,
          headers: { "Idempotency-Key": generateUUIDv4() },
          json: { tagihan_iuran_id: state.config.resources.tagihan_iuran_a2_id, rekening_qris_id: state.config.resources.rekening_qris_a_id }
        });
        const nomTotal = (qRes.json && qRes.json.data && qRes.json.data.nominal_total) || 50123;
        const p1 = await loginAccount({ email: "pengurus_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const csv = "tanggal,keterangan,tipe,nominal,no_rekening\n2026-11-14,QRIS Iuran Uji " + Date.now() + ",CR," + nomTotal + ",1234567890\n";
        const fd1 = new FormData();
        fd1.append("file", new Blob([csv], { type: "text/csv" }), "mutasi.csv");
        const imp1 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/mutasi-bank/impor", token: p1.accessToken, body: fd1 });
        const fd2 = new FormData();
        fd2.append("file", new Blob([csv], { type: "text/csv" }), "mutasi.csv");
        const imp2 = await apiRequest({ method: "POST", path: "/api/v1/pembayaran/mutasi-bank/impor", token: p1.accessToken, body: fd2 });
        const rekon = await apiRequest({ method: "GET", path: "/api/v1/pembayaran/rekonsiliasi/harian", token: p1.accessToken });
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
        const getJ = await apiRequest({ method: "GET", path: "/api/v1/iuran/jenis", token: pAuth.accessToken });
        const postJ = await apiRequest({
          method: "POST",
          path: "/api/v1/iuran/jenis",
          token: pAuth.accessToken,
          json: { kode: "UI-" + String(Date.now()).slice(-6), nama: "Iuran Browser", tipe: "wajib", nominal: 25000 }
        });
        const newJenisId = (postJ.json && postJ.json.data && postJ.json.data.id) || state.config.resources.jenis_iuran_a_id;
        const gen = await apiRequest({
          method: "POST",
          path: "/api/v1/iuran/generate",
          token: pAuth.accessToken,
          json: { jenis_iuran_id: newJenisId, periode: "2026-12" }
        });
        appendFlowLog("3.7 Iuran + KEPUTUSAN AKUNTAN + Generate", { get_status: getJ.status, post_status: postJ.status, post_body: postJ.json, generate_status: gen.status, generate_body: gen.json });
      } catch (err) {
        appendFlowLog("3.7 Error", err.message);
      }
    });

    el("btnFlow38Admin").addEventListener("click", async function () {
      try {
        const adm = await loginAccount({ email: "admin_a@kopdes-test.local", koperasi_id: state.config.koperasi_a_id });
        const lap = await apiRequest({ method: "GET", path: "/api/v1/laporan/keuangan", token: adm.accessToken });
        const usrs = await apiRequest({ method: "GET", path: "/api/v1/users", token: adm.accessToken });
        const selfPatch = await apiRequest({
          method: "PATCH",
          path: "/api/v1/users/" + state.config.seeded_accounts[0].user_id + "/role",
          token: adm.accessToken,
          json: { role: "admin" }
        });
        appendFlowLog("3.8 Laporan & Tolak Self-Role-Escalation", { laporan: lap.status, users: usrs.status, self_patch_status: selfPatch.status, body: selfPatch.json });
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
        const idempKey = generateUUIDv4();
        const ctrl = new AbortController();
        ctrl.abort();
        let aborted = false;
        try {
          await apiRequest({
            method: "POST",
            path: "/api/v1/pembayaran/qris",
            token: ang.accessToken,
            headers: { "Idempotency-Key": idempKey },
            json: { tagihan_iuran_id: state.config.resources.tagihan_iuran_a_id, rekening_qris_id: state.config.resources.rekening_qris_a_id },
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
          json: { tagihan_iuran_id: state.config.resources.tagihan_iuran_a_id, rekening_qris_id: state.config.resources.rekening_qris_a_id }
        });
        const retry2 = await apiRequest({
          method: "POST",
          path: "/api/v1/pembayaran/qris",
          token: ang.accessToken,
          headers: { "Idempotency-Key": idempKey },
          json: { tagihan_iuran_id: state.config.resources.tagihan_iuran_a_id, rekening_qris_id: state.config.resources.rekening_qris_a_id }
        });
        el("behaviorOutputPre").textContent = JSON.stringify({
          abort_triggered: aborted,
          retry1_status: retry1.status,
          retry2_status: retry2.status,
          same_payment_id: retry1.json && retry2.json && retry1.json.data && retry2.json.data && retry1.json.data.pembayaran_id === retry2.json.data.pembayaran_id
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
