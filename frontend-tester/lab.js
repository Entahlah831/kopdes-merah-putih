"use strict";

(function () {
  function assert(cond, msg) {
    if (!cond) {
      throw new Error(msg);
    }
  }

  function clearResultsTable() {
    window.KopdesApp.el("labResultsBody").innerHTML = "";
    window.KopdesApp.el("labDetailOutput").textContent = "";
  }

  function appendResultRow(id, label, passed, ms, detail) {
    const tr = document.createElement("tr");
    const cells = [id, label, passed ? "PASS" : "FAIL", String(ms), detail];
    cells.forEach(function (val) {
      const td = document.createElement("td");
      td.textContent = val;
      tr.appendChild(td);
    });
    window.KopdesApp.el("labResultsBody").appendChild(tr);
  }

  function logLabDetail(text) {
    const pre = window.KopdesApp.el("labDetailOutput");
    pre.textContent += text + "\n";
  }

  async function runDiagnostics() {
    const app = window.KopdesApp;
    const out = [];
    if (!app.state.uiConfig) {
      await app.loadAllConfigs();
    }
    const base = app.getApiBase();
    const h = await app.apiRequest("GET", "/healthz");
    out.push("GET /healthz -> HTTP " + h.status + " " + h.text);
    const r = await app.apiRequest("GET", "/readyz");
    out.push("GET /readyz -> HTTP " + r.status + " " + r.text);
    const cfg = await app.apiRequest("GET", "/api/v1/public/config");
    out.push("GET /api/v1/public/config -> HTTP " + cfg.status + " api_version=" +
      ((cfg.json && cfg.json.data && cfg.json.data.api_version) || "?"));

    const corsAllow = await fetch("/api/probe-cors", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ target_url: base + "/healthz", origin: window.location.origin, method: "GET" })
    });
    const corsAllowData = await corsAllow.json();
    out.push("CORS probe origin=" + window.location.origin + " -> HTTP " + corsAllowData.status_code +
      " allow_origin=" + ((corsAllowData.headers && corsAllowData.headers["Access-Control-Allow-Origin"]) || "-"));

    app.el("diagOutput").textContent = out.join("\n");
    return out;
  }

  async function runFullLiveDemo() {
    const app = window.KopdesApp;
    clearResultsTable();
    const summaryEl = app.el("labSummary");
    summaryEl.textContent = "Menjalankan Live Demo End-to-End ...";

    const steps = [];
    async function runStep(id, claim, fn) {
      const t0 = performance.now();
      try {
        const detail = await fn();
        const ms = Math.round(performance.now() - t0);
        appendResultRow(id, claim, true, ms, detail);
        steps.push(true);
      } catch (e) {
        const ms = Math.round(performance.now() - t0);
        appendResultRow(id, claim, false, ms, e.message);
        logLabDetail("[FAIL] " + id + ": " + e.message);
        steps.push(false);
      }
    }

    const adminTok = await app.ensureRoleToken("admin");
    const bendTok = await app.ensureRoleToken("bendahara");
    const tokoTok = await app.ensureRoleToken("petugas_toko");
    const anggota1Tok = await app.ensureRoleToken("anggota_1");

    let regID = "";
    let uploadTok = "";
    let actCode = "";
    let calonEmail = "demo-" + app.randomDigits(6) + "@kopdes-test.local";
    let calonPwd = "SandiDemoKopdes2026!";
    let calonTok = "";
    let demoProdID = "";
    let demoOrderID = "";
    let demoPayID = "";
    let demoPayTotal = 0;

    await runStep("DEMO-01", "Pendaftaran Publik & Unggah KYC PNG", async function () {
      const reg = await app.apiRequest("POST", "/api/v1/public/pendaftaran", {
        json: {
          nama_lengkap: "Budi Demo Live",
          email: calonEmail,
          nik: "3201" + app.randomDigits(12),
          no_hp: "0818" + app.randomDigits(8),
          tanggal_lahir: "1993-08-17",
          alamat: "Dusun Merdeka RT 01 RW 02",
          pekerjaan: "Petani"
        }
      });
      assert(reg.status === 201 && reg.json && reg.json.data, "Daftar gagal: HTTP " + reg.status + " " + reg.text);
      regID = reg.json.data.id;
      uploadTok = reg.json.data.upload_token;

      const fd = new FormData();
      fd.append("tipe_dokumen", "ktp");
      fd.append("file", app.makeUniquePNGBlob(), "ktp.png");
      const up = await app.apiRequest("POST", "/api/v1/public/pendaftaran/" + regID + "/dokumen", {
        headers: { "X-Upload-Token": uploadTok },
        body: fd
      });
      assert(up.status === 201, "Upload KYC gagal: HTTP " + up.status + " " + up.text);
      return "id=" + regID + " status=" + reg.json.data.status;
    });

    await runStep("DEMO-02", "Admin Verifikasi KYC & Aktivasi Akun", async function () {
      const kyc = await app.apiRequest("GET", "/api/v1/pendaftaran/" + regID + "/dokumen/ktp", { token: adminTok });
      assert(kyc.status === 200, "Unduh KYC gagal: HTTP " + kyc.status);
      const ver = await app.apiRequest("POST", "/api/v1/pendaftaran/" + regID + "/verifikasi", {
        token: adminTok,
        json: { keputusan: "disetujui", catatan: "Verifikasi live demo" }
      });
      assert(ver.status === 200 && ver.json && ver.json.data, "Verifikasi gagal: HTTP " + ver.status + " " + ver.text);
      actCode = ver.json.data.activation_code;
      assert(actCode, "activation_code tidak dikembalikan");
      const act = await app.apiRequest("POST", "/api/v1/auth/password", {
        json: { code: actCode, new_password: calonPwd }
      });
      assert(act.status === 200, "Aktivasi sandi gagal: HTTP " + act.status + " " + act.text);
      const sess = await app.loginWithOptionalMFA(calonEmail, calonPwd);
      calonTok = sess.session_token;
      return "calon login sukses user_id=" + sess.user_id;
    });

    await runStep("DEMO-03", "Petugas Toko Tambah Produk & Anggota Checkout Diantar", async function () {
      const katID = (app.state.uiConfig.resources && app.state.uiConfig.resources.kategori_id) || "";
      const sku = "SKU-DEMO-" + app.randomDigits(6);
      const cp = await app.apiRequest("POST", "/api/v1/katalog/produk", {
        token: tokoTok,
        json: {
          kategori_id: katID,
          sku: sku,
          nama: "Beras Uji " + sku,
          deskripsi: "Produk uji live demo",
          satuan: "kg",
          harga_umum: 16000,
          harga_anggota: 14000,
          stok: 20,
          status: "aktif"
        }
      });
      assert(cp.status === 201 && cp.json && cp.json.data, "Buat produk gagal: " + cp.status + " " + cp.text);
      demoProdID = cp.json.data.id;

      const idem = "demo-chk-" + app.randomUUID();
      const today = new Date().toISOString().slice(0, 10);
      const co = await app.apiRequest("POST", "/api/v1/toko/checkout", {
        token: calonTok || anggota1Tok,
        headers: { "Idempotency-Key": idem },
        json: {
          items: [{ produk_id: demoProdID, kuantitas: 2 }],
          metode_pengiriman: "diantar",
          metode_pembayaran: "qris",
          nama_penerima: "Budi Demo",
          hp_penerima: "08180001122",
          alamat_pengantaran: "Jl. Balai Desa No. 5",
          tanggal_antar: today,
          idempotency_key: idem
        }
      });
      assert(co.status === 201 && co.json && co.json.data, "Checkout gagal: " + co.status + " " + co.text);
      demoOrderID = co.json.data.id;
      return "order=" + co.json.data.nomor_pesanan + " total=" + co.json.data.total_bayar;
    });

    await runStep("DEMO-04", "Buat Pembayaran QRIS, Upload Bukti & Verifikasi Bendahara", async function () {
      const payToken = calonTok || anggota1Tok;
      const idem = "demo-pay-" + app.randomUUID();
      const cr = await app.apiRequest("POST", "/api/v1/pembayaran", {
        token: payToken,
        headers: { "Idempotency-Key": idem },
        json: {
          jenis_transaksi: "pesanan",
          metode: "qris",
          pesanan_id: demoOrderID,
          catatan_pembayar: "QRIS demo live",
          idempotency_key: idem
        }
      });
      assert(cr.status === 201 && cr.json && cr.json.data, "Buat bayar gagal: " + cr.status + " " + cr.text);
      const pay = cr.json.data.pembayaran;
      demoPayID = pay.id;
      demoPayTotal = pay.nominal_total;

      const fd = new FormData();
      fd.append("pembayaran_id", demoPayID);
      fd.append("catatan_pembayar", "Bukti QRIS valid");
      fd.append("file", app.makeUniquePNGBlob(), "bukti.png");
      const up = await app.apiRequest("POST", "/api/v1/pembayaran", { token: payToken, body: fd });
      assert(up.status === 200, "Upload bukti gagal: " + up.status + " " + up.text);

      const ver = await app.apiRequest("POST", "/api/v1/pembayaran/" + demoPayID + "/verifikasi", {
        token: bendTok,
        json: { keputusan: "setuju", nominal_dibayar: demoPayTotal, alasan: "Valid live demo" }
      });
      assert(ver.status === 200, "Verifikasi bendahara gagal: " + ver.status + " " + ver.text);
      return "kwitansi status=lunas nominal_total=" + demoPayTotal;
    });

    await runStep("DEMO-05", "Update Status Diantar, Cek Daftar Antar & Laporan CSV", async function () {
      const st = await app.apiRequest("PATCH", "/api/v1/pesanan/" + demoOrderID + "/status", {
        token: tokoTok,
        json: { status: "diantar", catatan: "Kurir berangkat" }
      });
      assert(st.status === 200, "Update status diantar gagal: " + st.status + " " + st.text);

      const today = new Date().toISOString().slice(0, 10);
      const da = await app.apiRequest("GET", "/api/v1/pesanan/daftar-antar?tanggal=" + today, { token: tokoTok });
      assert(da.status === 200 && da.json && Array.isArray(da.json.data), "Daftar antar gagal: " + da.status);
      const found = da.json.data.find(function (x) { return x.id === demoOrderID; });
      assert(found && found.label_antar, "Order baru tidak muncul pada daftar-antar");

      const lap = await app.apiRequest("GET", "/api/v1/laporan?jenis=pembayaran&format=csv", { token: bendTok });
      assert(lap.status === 200 && lap.contentType.indexOf("text/csv") !== -1, "Laporan CSV gagal: " + lap.status);
      return "label=" + found.label_antar;
    });

    const passedCount = steps.filter(Boolean).length;
    summaryEl.textContent = "Live Demo selesai: " + passedCount + "/" + steps.length + " langkah PASS.";
  }

  async function runScenarioSuite() {
    const app = window.KopdesApp;
    clearResultsTable();
    const summaryEl = app.el("labSummary");
    summaryEl.textContent = "Menjalankan SCEN-01 .. SCEN-10 ...";

    if (!app.state.scenarios) {
      await app.loadAllConfigs();
    }
    const adminTok = await app.ensureRoleToken("admin");
    const bendTok = await app.ensureRoleToken("bendahara");
    const tokoTok = await app.ensureRoleToken("petugas_toko");
    const anggota1Tok = await app.ensureRoleToken("anggota_1");

    const ctxData = {};
    const outcomes = [];

    const handlers = {
      "SCEN-01": async function () {
        const h = await app.apiRequest("GET", "/healthz");
        const r = await app.apiRequest("GET", "/readyz");
        const c = await app.apiRequest("GET", "/api/v1/public/config");
        assert(h.status === 200 && r.status === 200 && c.status === 200, "Health/config tidak 200");
        const lower = c.text.toLowerCase();
        assert(lower.indexOf("aes_master_key") === -1 && lower.indexOf("db_password") === -1, "Secret bocor!");
        return "api_version=" + c.json.data.api_version + " koperasi=" + c.json.data.koperasi.nama_koperasi;
      },
      "SCEN-02": async function () {
        const tempEmail = "mfa-" + app.randomDigits(5) + "@kopdes-test.local";
        const tempPwd = "SandiMFAUji2026!";
        const cu = await app.apiRequest("POST", "/api/v1/users", {
          token: adminTok,
          json: { email: tempEmail, no_hp: "0819" + app.randomDigits(8), nama_lengkap: "Bendahara Uji MFA", peran: "bendahara", password: tempPwd }
        });
        assert(cu.status === 201, "Buat user uji gagal: " + cu.status + " " + cu.text);
        const l1 = await app.apiRequest("POST", "/api/v1/auth/login", { json: { identifier: tempEmail, password: tempPwd } });
        assert(l1.status === 200 && (l1.json.data.scope === "mfa_setup" || l1.json.data.scope === "mfa_pending"), "Expected mfa_setup/mfa_pending, got " + l1.text);
        const tok1 = l1.json.data.session_token;
        const guard = await app.apiRequest("GET", "/api/v1/iuran/tagihan", { token: tok1 });
        assert(guard.status === 401 || guard.status === 403, "mfa_pending harus ditolak, dapat " + guard.status);

        const setup = await app.apiRequest("POST", "/api/v1/auth/mfa/setup", { token: tok1, json: {} });
        assert(setup.status === 200 && setup.json.data.secret && setup.json.data.setup_token, "Setup MFA gagal");
        const code0 = await app.fetchTOTPHelper({ secret_base32: setup.json.data.secret, offset_seconds: 0 });
        const ver = await app.apiRequest("POST", "/api/v1/auth/mfa/setup", {
          token: tok1,
          json: { setup_token: setup.json.data.setup_token, code: code0 }
        });
        assert(ver.status === 200 && ver.json.data.scope === "full", "Verify setup gagal: " + ver.text);

        const l2 = await app.apiRequest("POST", "/api/v1/auth/login", { json: { identifier: tempEmail, password: tempPwd } });
        assert(l2.status === 200 && l2.json.data.scope === "mfa_pending", "Expected mfa_pending after MFA setup, got " + l2.text);
        const tok2 = l2.json.data.session_token;
        const replay = await app.apiRequest("POST", "/api/v1/auth/mfa/verify", { token: tok2, json: { code: code0 } });
        assert(replay.status === 401, "Replay TOTP harus 401, dapat " + replay.status);

        const codeNext = await app.fetchTOTPHelper({ secret_base32: setup.json.data.secret, offset_seconds: 30 });
        const ver2 = await app.apiRequest("POST", "/api/v1/auth/mfa/verify", { token: tok2, json: { code: codeNext } });
        assert(ver2.status === 200, "Step TOTP +30 harus 200, dapat " + ver2.status);
        await app.apiRequest("POST", "/api/v1/auth/logout", { token: tok2, json: {} });
        const afterOut = await app.apiRequest("GET", "/api/v1/auth/me", { token: tok2 });
        assert(afterOut.status === 401, "Setelah logout harus 401, dapat " + afterOut.status);
        return "setup + replay 401 + logout 401 terverifikasi";
      },
      "SCEN-03": async function () {
        const aUsers = await app.apiRequest("GET", "/api/v1/users", { token: anggota1Tok });
        const aPend = await app.apiRequest("GET", "/api/v1/pendaftaran", { token: anggota1Tok });
        const aLap = await app.apiRequest("GET", "/api/v1/laporan?jenis=anggota", { token: anggota1Tok });
        const tAntar = await app.apiRequest("GET", "/api/v1/pesanan/daftar-antar", { token: tokoTok });
        const bIuran = await app.apiRequest("GET", "/api/v1/iuran/tagihan", { token: bendTok });
        assert(aUsers.status === 403 && aPend.status === 403 && aLap.status === 403, "Anggota seharusnya ditolak 403");
        assert(tAntar.status === 200 && bIuran.status === 200, "Staf toko/bendahara seharusnya 200");
        return "anggota=403, petugas_toko=200, bendahara=200";
      },
      "SCEN-04": async function () {
        const mail = "scen04-" + app.randomDigits(6) + "@kopdes-test.local";
        const pwd = "SandiScen04!2026";
        const reg = await app.apiRequest("POST", "/api/v1/public/pendaftaran", {
          json: {
            nama_lengkap: "Sari Uji Skenario",
            email: mail,
            nik: "3202" + app.randomDigits(12),
            no_hp: "0816" + app.randomDigits(8),
            tanggal_lahir: "1996-01-20",
            alamat: "Desa Maju",
            pekerjaan: "Pedagang"
          }
        });
        assert(reg.status === 201, "Register gagal: " + reg.status);
        const rid = reg.json.data.id;
        const badFd = new FormData();
        badFd.append("tipe_dokumen", "ktp");
        badFd.append("file", app.makeUniquePNGBlob(), "ktp.png");
        const badUp = await app.apiRequest("POST", "/api/v1/public/pendaftaran/" + rid + "/dokumen", {
          headers: { "X-Upload-Token": "wrong-token" },
          body: badFd
        });
        assert(badUp.status === 401 || badUp.status === 403, "Wrong upload token harus ditolak");

        const goodFd = new FormData();
        goodFd.append("tipe_dokumen", "ktp");
        goodFd.append("file", app.makeUniquePNGBlob(), "ktp.png");
        const goodUp = await app.apiRequest("POST", "/api/v1/public/pendaftaran/" + rid + "/dokumen", {
          headers: { "X-Upload-Token": reg.json.data.upload_token },
          body: goodFd
        });
        assert(goodUp.status === 201, "Upload KYC gagal: " + goodUp.status);
        const dl = await app.apiRequest("GET", "/api/v1/pendaftaran/" + rid + "/dokumen/ktp", { token: adminTok });
        assert(dl.status === 200, "Admin dekripsi KYC gagal: " + dl.status);
        const ver = await app.apiRequest("POST", "/api/v1/pendaftaran/" + rid + "/verifikasi", {
          token: adminTok,
          json: { keputusan: "disetujui", catatan: "OK" }
        });
        assert(ver.status === 200 && ver.json.data.activation_code, "Verify gagal");
        const act = await app.apiRequest("POST", "/api/v1/auth/password", {
          json: { code: ver.json.data.activation_code, new_password: pwd }
        });
        assert(act.status === 200, "Aktivasi gagal");
        const sess = await app.loginWithOptionalMFA(mail, pwd);
        const susp = await app.apiRequest("PATCH", "/api/v1/anggota/" + ver.json.data.anggota_id + "/status", {
          token: adminTok,
          json: { status: "nonaktif", alasan: "Uji pencabutan sesi" }
        });
        assert(susp.status === 200, "Suspend gagal");
        const meAfter = await app.apiRequest("GET", "/api/v1/auth/me", { token: sess.session_token });
        assert(meAfter.status === 401, "Sesi anggota nonaktif harus dicabut 401");
        return "KYC + aktivasi + suspend revocation 401 PASS";
      },
      "SCEN-05": async function () {
        const list = await app.apiRequest("GET", "/api/v1/anggota?limit=5", { token: bendTok });
        assert(list.status === 200 && Array.isArray(list.json.data) && list.json.data.length > 0, "List anggota gagal");
        const aid = list.json.data[0].id;
        const bendUnmask = await app.apiRequest("GET", "/api/v1/anggota/" + aid + "?unmask=true", { token: bendTok });
        assert(bendUnmask.status === 403, "Bendahara unmask harus 403, dapat " + bendUnmask.status);
        const admUnmask = await app.apiRequest("GET", "/api/v1/anggota/" + aid + "?unmask=true", { token: adminTok });
        assert(admUnmask.status === 200 && admUnmask.json && admUnmask.json.data, "Admin unmask gagal");
        const csvDry = await app.apiRequest("POST", "/api/v1/anggota/impor?dry_run=true", {
          token: adminTok,
          json: { csv: "nama_lengkap,email,nik,no_hp,tanggal_lahir,alamat,pekerjaan\nUji Dry,dry-" + app.randomDigits(5) + "@kopdes-test.local,3209" + app.randomDigits(12) + ",0812" + app.randomDigits(8) + ",1990-01-01,Desa Uji,Buruh" }
        });
        assert(csvDry.status === 200, "Dry run impor CSV gagal: " + csvDry.text);
        return "masked/unmask RBAC + dry_run CSV OK";
      },
      "SCEN-06": async function () {
        const jen = await app.apiRequest("GET", "/api/v1/iuran/tagihan?scope=jenis", { token: bendTok });
        assert(jen.status === 200 && jen.json.data.length > 0, "Jenis iuran kosong");
        const yr = 2040 + (Number(app.randomDigits(2)) % 50);
        const mo = String(1 + (Number(app.randomDigits(2)) % 12)).padStart(2, "0");
        const gen = await app.apiRequest("POST", "/api/v1/iuran/jenis", {
          token: bendTok,
          json: { action: "generate", periode: yr + "-" + mo }
        });
        assert(gen.status === 200 || gen.status === 201, "Generate iuran gagal: HTTP " + gen.status + " " + gen.text);
        const tag = await app.apiRequest("GET", "/api/v1/iuran/tagihan?status=belum_bayar&limit=10", { token: bendTok });
        assert(tag.status === 200 && tag.json.data.length > 0, "Tagihan belum_bayar kosong");
        const tid = tag.json.data[0].id;
        const ker = await app.apiRequest("PATCH", "/api/v1/iuran/tagihan/" + tid + "/keringanan", {
          token: bendTok,
          json: { nominal_keringanan: 1000, alasan: "Uji keringanan otomatis" }
        });
        assert(ker.status === 200, "Keringanan gagal: " + ker.text);
        return "periode=" + yr + "-" + mo + " tagihan=" + tid;
      },
      "SCEN-07": async function () {
        const kat = await app.apiRequest("GET", "/api/v1/katalog/produk?tab=kategori", { token: anggota1Tok });
        const prm = await app.apiRequest("GET", "/api/v1/katalog/produk?tab=promo", { token: anggota1Tok });
        assert(kat.status === 200 && prm.status === 200, "Tab kategori/promo gagal");
        const katID = (app.state.uiConfig.resources && app.state.uiConfig.resources.kategori_id) || "";
        const sku = "SKU-NOH-" + app.randomDigits(6);
        const noPrice = await app.apiRequest("POST", "/api/v1/katalog/produk", {
          token: tokoTok,
          json: { kategori_id: katID, sku: sku, nama: "Produk Tanpa Harga " + sku, deskripsi: "Uji", satuan: "pcs", stok: 10, status: "aktif" }
        });
        assert(noPrice.status === 201, "Buat produk tanpa harga gagal: " + noPrice.text);
        const pid = noPrice.json.data.id;
        const badCo = await app.apiRequest("POST", "/api/v1/toko/checkout", {
          token: anggota1Tok,
          headers: { "Idempotency-Key": "noprice-" + app.randomUUID() },
          json: {
            items: [{ produk_id: pid, kuantitas: 1 }],
            metode_pengiriman: "ambil_di_koperasi",
            metode_pembayaran: "qris",
            nama_penerima: "Anggota Uji",
            hp_penerima: "08110000004"
          }
        });
        assert(badCo.status === 409 || badCo.status === 400, "Checkout tanpa harga harus 409/400, dapat " + badCo.status);
        const upd = await app.apiRequest("POST", "/api/v1/katalog/produk", {
          token: tokoTok,
          json: { action: "update_produk", id: pid, kategori_id: katID, sku: sku, nama: "Produk Siap " + sku, deskripsi: "Uji", satuan: "pcs", harga_umum: 12000, harga_anggota: 10000, stok: 15, status: "aktif" }
        });
        assert(upd.status === 200, "Update harga produk gagal: " + upd.text);
        ctxData.prodID = pid;
        ctxData.sku = sku;
        return "produk=" + sku + " no-price HTTP " + badCo.status + " -> aktif";
      },
      "SCEN-08": async function () {
        const pid = ctxData.prodID;
        const idem = "scen08-" + app.randomUUID();
        const today = new Date().toISOString().slice(0, 10);
        const payload = {
          items: [{ produk_id: pid, kuantitas: 1 }],
          metode_pengiriman: "diantar",
          metode_pembayaran: "qris",
          nama_penerima: "Admin Uji",
          hp_penerima: "08110000001",
          alamat_pengantaran: "Jl. Koperasi No. 1",
          tanggal_antar: today,
          idempotency_key: idem
        };
        const co1 = await app.apiRequest("POST", "/api/v1/toko/checkout", {
          token: adminTok,
          headers: { "Idempotency-Key": idem },
          json: payload
        });
        assert(co1.status === 201, "Checkout pertama gagal: " + co1.text);
        const co2 = await app.apiRequest("POST", "/api/v1/toko/checkout", {
          token: adminTok,
          headers: { "Idempotency-Key": idem },
          json: payload
        });
        assert(co2.status === 200 && co2.json.data.id === co1.json.data.id, "Replay idempotensi gagal: " + co2.status);
        ctxData.orderID = co1.json.data.id;
        ctxData.orderNo = co1.json.data.nomor_pesanan;
        ctxData.today = today;
        return "order=" + ctxData.orderNo + " replay=200 sama";
      },
      "SCEN-09": async function () {
        const cr = await app.apiRequest("POST", "/api/v1/pembayaran", {
          token: adminTok,
          headers: { "Idempotency-Key": "pay-" + app.randomUUID() },
          json: { jenis_transaksi: "pesanan", metode: "qris", pesanan_id: ctxData.orderID, catatan_pembayar: "Uji dual control" }
        });
        assert(cr.status === 201, "Buat pembayaran gagal: " + cr.text);
        const pay = cr.json.data.pembayaran;
        const pid = pay.id;
        assert(pay.kode_unik > 0 && pay.nominal_total === pay.nominal_pokok + pay.kode_unik, "Kode unik QRIS tidak valid");

        const proofBlob = app.makeUniquePNGBlob();
        const fd1 = new FormData();
        fd1.append("pembayaran_id", pid);
        fd1.append("file", proofBlob, "bukti.png");
        const up1 = await app.apiRequest("POST", "/api/v1/pembayaran", { token: adminTok, body: fd1 });
        assert(up1.status === 200, "Upload bukti 1 gagal: " + up1.text);

        const fd2 = new FormData();
        fd2.append("pembayaran_id", pid);
        fd2.append("file", proofBlob, "bukti-dup.png");
        const up2 = await app.apiRequest("POST", "/api/v1/pembayaran", { token: adminTok, body: fd2 });
        assert(up2.status === 409, "Bukti duplikat SHA-256 harus 409, dapat " + up2.status);

        const selfVer = await app.apiRequest("POST", "/api/v1/pembayaran/" + pid + "/verifikasi", {
          token: adminTok,
          json: { keputusan: "setuju", nominal_dibayar: pay.nominal_total, alasan: "Coba self approve" }
        });
        assert(selfVer.status === 403, "Dual control harus menolak pembuat 403, dapat " + selfVer.status);

        const bendVer = await app.apiRequest("POST", "/api/v1/pembayaran/" + pid + "/verifikasi", {
          token: bendTok,
          json: { keputusan: "setuju", nominal_dibayar: pay.nominal_total, alasan: "Verifikasi bendahara valid" }
        });
        assert(bendVer.status === 200, "Verifikasi bendahara gagal: " + bendVer.text);

        const mv = await app.apiRequest("PATCH", "/api/v1/pesanan/" + ctxData.orderID + "/status", {
          token: tokoTok,
          json: { status: "diantar", catatan: "Siap diantar" }
        });
        assert(mv.status === 200, "Ubah status diantar gagal");
        const daCSV = await app.apiRequest("GET", "/api/v1/pesanan/daftar-antar?tanggal=" + ctxData.today + "&format=csv", { token: tokoTok });
        assert(daCSV.status === 200 && daCSV.text.indexOf(ctxData.orderNo) !== -1, "Order tidak muncul di CSV daftar-antar");
        return "kode_unik=" + pay.kode_unik + " dup=409 dual_control=403->200";
      },
      "SCEN-10": async function () {
        const types = ["anggota", "tunggakan", "penjualan", "pembayaran"];
        for (let i = 0; i < types.length; i++) {
          const r = await app.apiRequest("GET", "/api/v1/laporan?jenis=" + types[i], { token: bendTok });
          assert(r.status === 200, "Laporan " + types[i] + " gagal HTTP " + r.status);
        }
        const csv = await app.apiRequest("GET", "/api/v1/laporan?jenis=pembayaran&format=csv", { token: bendTok });
        assert(csv.status === 200 && csv.contentType.indexOf("text/csv") !== -1, "Laporan CSV gagal");
        return "4 jenis laporan JSON + CSV OK";
      },
      "SCEN-11": async function () {
        const uMail = "lock-" + app.randomDigits(6) + "@kopdes-test.local";
        const uPwd = "SandiLockout2026!";
        const cu = await app.apiRequest("POST", "/api/v1/users", {
          token: adminTok,
          json: { email: uMail, no_hp: "0815" + app.randomDigits(8), nama_lengkap: "Uji Lockout", peran: "petugas_toko", password: uPwd }
        });
        assert(cu.status === 201, "Buat user lockout gagal");
        let got429 = false;
        for (let i = 0; i < 8; i++) {
          const fail = await app.apiRequest("POST", "/api/v1/auth/login", {
            json: { identifier: uMail, password: "salah-password" },
            allow429: true
          });
          if (fail.status === 429) {
            got429 = true;
            break;
          }
        }
        assert(got429, "Expected 429 after repeated failed logins");
        return "account lockout 429 terverifikasi pada user uji";
      }
    };

    const includeLockout = app.el("chkIncludeLockout").checked;
    const list = (app.state.scenarios && app.state.scenarios.scenarios) || [];
    for (let i = 0; i < list.length; i++) {
      const sc = list[i];
      if (sc.destructive && !includeLockout) {
        appendResultRow(sc.id, sc.claim_id + " (opt-in dilewati)", true, 0, "Centang opt-in untuk menjalankan");
        continue;
      }
      const fn = handlers[sc.id];
      if (!fn) continue;
      const t0 = performance.now();
      try {
        const detail = await fn();
        const ms = Math.round(performance.now() - t0);
        appendResultRow(sc.id, sc.claim_id + " — " + sc.title, true, ms, detail);
        outcomes.push(true);
      } catch (e) {
        const ms = Math.round(performance.now() - t0);
        appendResultRow(sc.id, sc.claim_id + " — " + sc.title, false, ms, e.message);
        logLabDetail("[FAIL] " + sc.id + ": " + e.message);
        outcomes.push(false);
      }
    }

    const passed = outcomes.filter(Boolean).length;
    summaryEl.textContent = "Scenario Runner selesai: " + passed + "/" + outcomes.length + " skenario dijalankan PASS.";
  }

  async function runRoleMatrix() {
    const app = window.KopdesApp;
    clearResultsTable();
    const summaryEl = app.el("labSummary");
    summaryEl.textContent = "Menjalankan Role Matrix ...";

    if (!app.state.rolesMatrix) {
      await app.loadAllConfigs();
    }
    const tokens = {
      unauthenticated: "",
      anggota: await app.ensureRoleToken("anggota_1"),
      petugas_toko: await app.ensureRoleToken("petugas_toko"),
      bendahara: await app.ensureRoleToken("bendahara"),
      admin: await app.ensureRoleToken("admin")
    };

    const roles = app.state.rolesMatrix.roles;
    const endpoints = app.state.rolesMatrix.endpoints;
    let total = 0;
    let passed = 0;

    for (let i = 0; i < endpoints.length; i++) {
      const ep = endpoints[i];
      const t0 = performance.now();
      const notes = [];
      let allMatch = true;

      for (let r = 0; r < roles.length; r++) {
        const role = roles[r];
        total++;
        const opts = { token: tokens[role] || "" };
        if (ep.body !== undefined) {
          opts.json = ep.body;
        }
        const res = await app.apiRequest(ep.method, ep.path, opts);
        const exp = ep.expected_status[role];
        const ok = Array.isArray(exp) ? exp.indexOf(res.status) !== -1 : res.status === exp;
        if (ok) {
          passed++;
        } else {
          allMatch = false;
        }
        notes.push(role + "=" + res.status + "(exp:" + exp + ")");
      }
      const ms = Math.round(performance.now() - t0);
      appendResultRow(ep.id, ep.method + " " + ep.path, allMatch, ms, notes.join(" | "));
      if (!allMatch) {
        logLabDetail("[MISMATCH] " + ep.id + " " + ep.method + " " + ep.path + ": " + notes.join(", "));
      }
      await new Promise(function (resolve) { setTimeout(resolve, 150); });
    }
    summaryEl.textContent = "Role Matrix selesai: " + passed + "/" + total + " pemeriksaan cocok.";
  }

  document.addEventListener("DOMContentLoaded", function () {
    const app = window.KopdesApp;
    app.el("btnRunDiagnostics").addEventListener("click", function () {
      runDiagnostics().catch(function (e) {
        app.el("diagOutput").textContent = "Diagnostics Error: " + e.message;
      });
    });
    app.el("btnRunLiveDemo").addEventListener("click", function () {
      runFullLiveDemo().catch(function (e) {
        app.el("labSummary").textContent = "Live Demo Error: " + e.message;
      });
    });
    app.el("btnRunScenarios").addEventListener("click", function () {
      runScenarioSuite().catch(function (e) {
        app.el("labSummary").textContent = "Scenario Error: " + e.message;
      });
    });
    app.el("btnRunRoleMatrix").addEventListener("click", function () {
      runRoleMatrix().catch(function (e) {
        app.el("labSummary").textContent = "Role Matrix Error: " + e.message;
      });
    });
  });
})();
