import { test, expect, Page } from "@playwright/test";
import path from "node:path";
import fs from "node:fs";
import { fileURLToPath } from "node:url";

const seedPassword = process.env.E2E_SEED_PASSWORD || "";
const screenshotDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../reports/wireframe");

function ensureScreenshotDir(): void {
  fs.mkdirSync(screenshotDir, { recursive: true });
}

async function generateTestPNG(page: Page, label: string): Promise<Buffer> {
  const b64 = await page.evaluate((txt) => {
    const c = document.createElement("canvas");
    c.width = 160;
    c.height = 100;
    const ctx = c.getContext("2d")!;
    ctx.fillStyle = "#f4f4f5";
    ctx.fillRect(0, 0, 160, 100);
    ctx.strokeStyle = "#18181b";
    ctx.strokeRect(4, 4, 152, 92);
    ctx.fillStyle = "#18181b";
    ctx.font = "12px sans-serif";
    ctx.fillText(txt, 14, 50);
    return c.toDataURL("image/png").split(",")[1];
  }, label);
  return Buffer.from(b64, "base64");
}

async function loginUI(page: Page, identifier: string, password: string, mfaEmail?: string): Promise<void> {
  await page.getByTestId("nav-masuk").click();
  await page.getByTestId("login-identifier").fill(identifier);
  await page.getByTestId("login-password").fill(password);
  await page.getByTestId("btn-login").click();

  if (mfaEmail) {
    await expect(page.getByTestId("mfa-code")).toBeVisible({ timeout: 10_000 });
    let loggedIn = false;
    for (const offset of [-30, 0, 30]) {
      const totpRes = await page.request.post("/api/totp-helper", {
        data: { account_email: mfaEmail, offset_seconds: offset }
      });
      expect(totpRes.ok()).toBeTruthy();
      const { code } = (await totpRes.json()) as { code: string };
      await page.getByTestId("mfa-code").fill(code);
      await page.getByTestId("btn-mfa-submit").click();
      try {
        await expect(page.getByTestId("user-badge")).toBeVisible({ timeout: 3_500 });
        loggedIn = true;
        break;
      } catch {
        // Retry with adjacent TOTP step if same 30s window was already consumed
      }
    }
    expect(loggedIn).toBeTruthy();
  } else {
    await expect(page.getByTestId("user-badge")).toBeVisible({ timeout: 10_000 });
  }
}

async function logoutUI(page: Page): Promise<void> {
  const btn = page.getByTestId("btn-logout");
  if (await btn.isVisible()) {
    await btn.click();
    await expect(page.getByTestId("nav-masuk")).toBeVisible();
  }
}

test.describe("Kopdes Wireframe E2E (Set B Cookie-Primary)", () => {
  test.beforeAll(() => {
    if (!seedPassword) {
      throw new Error("E2E_SEED_PASSWORD wajib disetel dari test runner");
    }
    ensureScreenshotDir();
  });

  test("01. Beranda, Katalog, Mobile Viewport, & Auth Cookie/XSS Hygiene", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByTestId("beranda-hero")).toBeVisible();
    await expect(page.locator("body")).toContainText("BLOCKED: butuh keputusan pemilik");
    await page.screenshot({ path: path.join(screenshotDir, "01_beranda.png"), fullPage: true });

    // Mobile responsive 375x812 check
    await page.setViewportSize({ width: 375, height: 812 });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 2);
    expect(overflow).toBeFalsy();
    await page.screenshot({ path: path.join(screenshotDir, "09_mobile_beranda.png"), fullPage: true });

    await page.setViewportSize({ width: 1280, height: 800 });
    await page.getByTestId("nav-katalog").click();
    await expect(page.getByTestId("kat-search")).toBeVisible();
    await page.screenshot({ path: path.join(screenshotDir, "02_katalog.png"), fullPage: true });

    // Empty state + Reset filter
    await page.getByTestId("kat-search").fill("ZZZ_TIDAK_ADA_PRODUK_999");
    await expect(page.getByTestId("katalog-empty")).toBeVisible();
    await page.getByTestId("btn-reset-filter").click();
  });

  test("02. Happy Path Pendaftaran + KYC Canvas + XSS Literal Check + Approval Admin + Aktivasi", async ({ page }) => {
    const uniq = Date.now().toString().slice(-6);
    const xssName = `Warga <img src=x onerror="window.__xss=1"> ${uniq}`;
    await page.goto("/#/daftar");

    await page.locator('input[name="nama_lengkap"]').fill(xssName);
    await page.locator('input[name="email"]').fill(`calon_${uniq}@kopdes-test.local`);
    await page.locator('input[name="nik"]').fill(`320101${uniq.padStart(10, "0")}`);
    await page.locator('input[name="no_hp"]').fill(`0852${uniq.padStart(8, "1")}`);
    await page.locator('input[name="pekerjaan"]').fill("Petani");
    await page.locator('textarea[name="alamat"]').fill("Jl. Desa Maju No. 1");
    await page.locator("#chk-domisili").check();
    await page.locator("#chk-pdp").check();
    await page.getByTestId("btn-kirim-daftar").click();
    await expect(page.getByTestId("daftar-sukses")).toBeVisible();

    const pngKTP = await generateTestPNG(page, "KTP-" + uniq);
    const pngSelfie = await generateTestPNG(page, "SELFIE-" + uniq);
    const pngKK = await generateTestPNG(page, "KK-" + uniq);

    for (const [tipe, buf] of [
      ["ktp", pngKTP],
      ["selfie_ktp", pngSelfie],
      ["kk", pngKK]
    ] as const) {
      await page.getByTestId("kyc-tipe").selectOption(tipe);
      await page.getByTestId("kyc-file").setInputFiles({
        name: `${tipe}.png`,
        mimeType: "image/png",
        buffer: buf
      });
      await page.getByTestId("btn-upload-kyc").click();
      await expect(page.locator("body")).toContainText(`Dokumen ${tipe} berhasil terenkripsi`);
    }
    await page.screenshot({ path: path.join(screenshotDir, "03_daftar_kyc.png"), fullPage: true });

    // Admin login via HttpOnly cookie + MFA
    await loginUI(page, "admin@kopdes-test.local", seedPassword, "admin@kopdes-test.local");
    await expect(page.locator("body")).toContainText("Dashboard Pengurus");
    await page.screenshot({ path: path.join(screenshotDir, "04_dashboard_pengurus.png"), fullPage: true });

    // Ensure ZERO tokens in localStorage/sessionStorage (D3 compliance)
    const storageCount = await page.evaluate(() => window.localStorage.length + window.sessionStorage.length);
    expect(storageCount).toBe(0);

    await page.getByTestId("nav-verifikasi_anggota").click();
    await page.getByTestId("search-reg").fill(uniq);
    const tinjauBtn = page.locator("table button", { hasText: "Tinjau" }).first();
    await tinjauBtn.click();

    // Negative scenario #6: XSS payload rendered as text only, NEVER executed
    const xssFired = await page.evaluate(() => (window as unknown as { __xss?: number }).__xss);
    expect(xssFired).toBeUndefined();
    await expect(page.locator("body")).toContainText(`<img src=x`);
    await page.screenshot({ path: path.join(screenshotDir, "05_verifikasi_anggota.png"), fullPage: true });

    await page.getByTestId("reg-catatan").fill("Berkas lengkap");
    await page.getByTestId("btn-reg-setuju").click();
    await expect(page.getByTestId("reg-decision-ok")).toBeVisible();
    const actCode = (await page.getByTestId("reg-activation-code").textContent())?.trim() || "";
    expect(actCode.length).toBeGreaterThan(4);

    await logoutUI(page);

    // Aktivasi sandi & login anggota baru
    await page.goto("/#/daftar");
    await page.getByTestId("act-code").fill(actCode);
    await page.getByTestId("act-pass").fill(`SandiKuat_${uniq}!9`);
    await page.getByTestId("btn-aktivasi").click();
    await expect(page.getByTestId("act-sukses")).toBeVisible();

    await loginUI(page, `calon_${uniq}@kopdes-test.local`, `SandiKuat_${uniq}!9`);
    await logoutUI(page);
  });

  test("03. Happy Path Toko -> Pesanan -> Bukti Canvas -> Verifikasi Bendahara -> Selesai -> Iuran", async ({ page }) => {
    await page.goto("/");
    const rt = await (await page.request.get("/config.json")).json() as { api_public_url: string };
    const apiBase = rt.api_public_url;

    // Siapkan rekening QRIS aktif, produk, & jenis/tagihan iuran via admin session (cookie-primary)
    await loginUI(page, "admin@kopdes-test.local", seedPassword, "admin@kopdes-test.local");
    const sku = `WFSKU-${Date.now().toString().slice(-6)}`;
    const setupOK = await page.evaluate(async ({ base, sku }) => {
      const pubRes = await fetch(`${base}/api/v1/public/config`, { credentials: "include" });
      const pubJson = await pubRes.json();
      const kop = pubJson?.data?.koperasi || {};
      if (!kop?.active_qris?.rekening_id) {
        const rekRes = await fetch(`${base}/api/v1/pembayaran`, {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" },
          body: JSON.stringify({
            action: "create_rekening",
            nama_merchant: kop?.nama_koperasi || "Kopdes Set B",
            nmid: "ID102003004005",
            jenis_pemilik: "badan_hukum",
            nama_bank: "BRI",
            nomor_rekening: "000101000123",
            nama_pemilik: "Koperasi Desa Merah Putih"
          })
        });
        if (!rekRes.ok) return false;
        const rekJson = await rekRes.json();
        const pengRes = await fetch(`${base}/api/v1/pengaturan`, {
          method: "PUT",
          credentials: "include",
          headers: { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" },
          body: JSON.stringify({
            nama_koperasi: kop?.nama_koperasi || "Koperasi Desa Merah Putih",
            qris_unique_digits: kop?.qris_unique_digits || 4,
            ongkir_tetap: kop?.ongkir_tetap || 5000,
            active_rekening_qris_id: rekJson.data.id
          })
        });
        if (!pengRes.ok) return false;
      }
      const r = await fetch(`${base}/api/v1/katalog/produk`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" },
        body: JSON.stringify({
          sku, nama: `Beras Wireframe ${sku}`, satuan: "kg",
          harga_umum: 15000, harga_anggota: 13000, stok: 10, status: "aktif"
        })
      });
      return r.ok;
    }, { base: apiBase, sku });
    expect(setupOK).toBeTruthy();

    await page.evaluate(async ({ base }) => {
      const ji = await fetch(`${base}/api/v1/iuran/jenis`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" },
        body: JSON.stringify({
          kode: `IUR_${Date.now().toString().slice(-4)}`, nama: "Iuran Wajib Bulanan",
          nominal: 25000, hari_jatuh_tempo: 10, klasifikasi_akuntansi: "simpanan_wajib"
        })
      });
      if (ji.ok) {
        const y = 2030 + Math.floor(Math.random() * 50);
        const m = 1 + Math.floor(Math.random() * 12);
        await fetch(`${base}/api/v1/iuran/jenis`, {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" },
          body: JSON.stringify({ action: "generate", periode: `${y}-${String(m).padStart(2, "0")}` })
        });
      }
    }, { base: apiBase });
    await logoutUI(page);

    // Anggota1 checkout + bayar QRIS + upload bukti
    await loginUI(page, "anggota1@kopdes-test.local", seedPassword);
    await page.getByTestId("nav-katalog").click();
    await page.getByTestId(`pesan-${sku}`).click();
    await page.getByTestId("co-nama").fill("Anggota Satu Wireframe");
    await page.getByTestId("co-hp").fill("08110000004");
    await page.getByTestId("btn-checkout").click();

    await expect(page.locator("h2")).toContainText("Pesanan Saya");
    await page.locator("table button", { hasText: "Detail" }).first().click();
    await expect(page.getByTestId("detail-pesanan-judul")).toBeVisible();
    await page.getByTestId("btn-bayar-pesanan").click();
    await expect(page.getByTestId("order-pay-info")).toBeVisible();
    const orderPayText = (await page.getByTestId("order-pay-info").textContent()) || "";
    const orderPayNomor = orderPayText.match(/PAY-[A-Z0-9-]+/)?.[0] || "pesanan";

    const buktiPNG = await generateTestPNG(page, "BUKTI-ORD-" + sku);
    await page.getByTestId("order-bukti-file").setInputFiles({
      name: "bukti_order.png",
      mimeType: "image/png",
      buffer: buktiPNG
    });
    await page.getByTestId("btn-upload-order-bukti").click();
    await expect(page.getByTestId("order-bukti-ok")).toBeVisible();
    await page.screenshot({ path: path.join(screenshotDir, "06_pesanan_upload_bukti.png"), fullPage: true });

    // Buka 04a Tagihan & bayar salah satu tagihan belum_bayar
    await page.getByTestId("nav-tagihan").click();
    await expect(page.locator("table input[type='checkbox']").first()).toBeVisible();
    const firstChk = page.locator('table input[type="checkbox"]:not([disabled])').first();
    await expect(firstChk).toBeVisible();
    await firstChk.check();
    await page.getByTestId("btn-buat-bayar-iuran").click();
    const iurPNG = await generateTestPNG(page, "BUKTI-IUR-" + sku);
    await page.getByTestId("iuran-bukti-file").setInputFiles({
      name: "bukti_iuran.png",
      mimeType: "image/png",
      buffer: iurPNG
    });
    await page.getByTestId("btn-kirim-bukti-iuran").click();
    await expect(page.locator("body")).toContainText("Status");
    await page.screenshot({ path: path.join(screenshotDir, "08_tagihan_iuran.png"), fullPage: true });
    await logoutUI(page);

    // Bendahara memverifikasi pembayaran pesanan (dual control) + iuran
    await loginUI(page, "bendahara@kopdes-test.local", seedPassword, "bendahara@kopdes-test.local");
    await page.getByTestId("nav-verifikasi_pembayaran").click();
    const pesPayBtn = page.locator("table tr").filter({ hasText: orderPayNomor }).filter({ hasText: "menunggu_verifikasi" }).first().locator("button", { hasText: "Periksa" });
    await expect(pesPayBtn).toBeVisible();
    await pesPayBtn.click();
    await expect(page.getByTestId("btn-pay-setuju")).toBeVisible();
    await page.screenshot({ path: path.join(screenshotDir, "07_verifikasi_pembayaran.png"), fullPage: true });
    await page.getByTestId("ver-pay-alasan").fill("Nominal QRIS sesuai");
    await page.getByTestId("btn-pay-setuju").click();
    await expect(page.getByTestId("ver-pay-ok")).toBeVisible();
    const iurPayBtn = page.locator("table tr").filter({ hasText: "iuran" }).filter({ hasText: "menunggu_verifikasi" }).first().locator("button", { hasText: "Periksa" });
    if (await iurPayBtn.count() > 0) {
      await iurPayBtn.click();
      await page.getByTestId("ver-pay-alasan").fill("Iuran QRIS terverifikasi");
      await page.getByTestId("btn-pay-setuju").click();
      await expect(page.getByTestId("ver-pay-ok")).toBeVisible();
    }
    await logoutUI(page);

    // Admin memproses pesanan terbayar hingga selesai (06 Detail Pesanan staf)
    await loginUI(page, "admin@kopdes-test.local", seedPassword, "admin@kopdes-test.local");
    await page.getByTestId("nav-pesanan").click();
    await page.locator("table tr", { hasText: "dibayar" }).first().locator("button", { hasText: "Detail" }).click();
    await page.getByTestId("btn-status-diproses").click();
    await page.getByTestId("btn-status-siap_diambil").click();
    await page.getByTestId("btn-status-selesai").click();
    await expect(page.getByTestId("detail-pesanan-judul")).toContainText("selesai");
    await page.screenshot({ path: path.join(screenshotDir, "08b_pesanan_selesai.png"), fullPage: true });
    await logoutUI(page);
  });

  test("04. 7 Skenario Negatif (Login salah, BFLA, File PDF ditolak, Idempotency, Batal, CSRF, Logout)", async ({ page }) => {
    const rt = await (await page.request.get("/config.json")).json() as { api_public_url: string };
    const apiBase = rt.api_public_url;

    await page.goto("/");

    // Negatif 1: Kata sandi salah
    await page.getByTestId("nav-masuk").click();
    await page.getByTestId("login-identifier").fill("anggota2@kopdes-test.local");
    await page.getByTestId("login-password").fill("PasswordSalahSekali!999");
    await page.getByTestId("btn-login").click();
    await expect(page.getByTestId("login-error")).toBeVisible();

    // Negatif 3: File non-gambar (PDF) ditolak di kanvas sebelum upload
    await page.goto("/#/daftar");
    const ts = Date.now().toString().slice(-6);
    await page.locator('input[name="nama_lengkap"]').fill("Uji PDF");
    await page.locator('input[name="email"]').fill(`pdf_${ts}@kopdes-test.local`);
    await page.locator('input[name="nik"]').fill(`320202${ts.padStart(10, "0")}`);
    await page.locator('input[name="no_hp"]').fill(`0853${ts.padStart(8, "2")}`);
    await page.locator('input[name="pekerjaan"]').fill("Warga");
    await page.locator('textarea[name="alamat"]').fill("Jl. Uji No. 2");
    await page.locator("#chk-domisili").check();
    await page.locator("#chk-pdp").check();
    await page.getByTestId("btn-kirim-daftar").click();
    await expect(page.getByTestId("daftar-sukses")).toBeVisible();

    await page.getByTestId("kyc-file").setInputFiles({
      name: "fake.pdf",
      mimeType: "application/pdf",
      buffer: Buffer.from("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n")
    });
    await page.getByTestId("btn-upload-kyc").click();
    await expect(page.locator("body")).toContainText("Format berkas ditolak: hanya JPG atau PNG");

    // Negatif 2 (BFLA) + Negatif 4 (Idempotency) + Negatif 5 (Batal) + Negatif 6/7 (CSRF & Logout revoked)
    await loginUI(page, "anggota1@kopdes-test.local", seedPassword);
    await page.goto("/#/dashboard");
    await expect(page.getByTestId("forbidden-view")).toBeVisible();

    const bflaStatus = await page.evaluate(async ({ base }) => {
      const r = await fetch(`${base}/api/v1/users`, {
        method: "GET",
        credentials: "include",
        headers: { "X-Requested-With": "XMLHttpRequest" }
      });
      return r.status;
    }, { base: apiBase });
    expect(bflaStatus).toBe(403);

    // Negatif 7a: Request tulis cookie TANPA X-Requested-With wajib ditolak
    const noCSRFStatus = await page.evaluate(async ({ base }) => {
      const r = await fetch(`${base}/api/v1/toko/checkout`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: "{}"
      });
      return r.status;
    }, { base: apiBase });
    expect([400, 403]).toContain(noCSRFStatus);

    // Negatif 4 + Negatif 5: Checkout + idempotency replay lalu batal order di UI
    const prodList = await page.evaluate(async ({ base }) => {
      const r = await fetch(`${base}/api/v1/katalog/produk`, { credentials: "include" });
      const j = await r.json();
      return (j.data || []) as Array<{ id: string; stok_tersedia: number }>;
    }, { base: apiBase });
    const availProd = prodList.find((p) => p.stok_tersedia > 0);
    expect(availProd).toBeTruthy();

    const idemRes = await page.evaluate(async ({ base, pid }) => {
      const key = "neg-idem-" + Date.now();
      const body = JSON.stringify({
        items: [{ produk_id: pid, kuantitas: 1 }],
        metode_pengiriman: "ambil_sendiri",
        metode_pembayaran: "qris",
        nama_penerima: "Uji Batal",
        hp_penerima: "08110000004"
      });
      const r1 = await fetch(`${base}/api/v1/toko/checkout`, {
        method: "POST", credentials: "include",
        headers: { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest", "Idempotency-Key": key },
        body
      });
      const j1 = await r1.json();
      const r2 = await fetch(`${base}/api/v1/toko/checkout`, {
        method: "POST", credentials: "include",
        headers: { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest", "Idempotency-Key": key },
        body
      });
      const j2 = await r2.json();
      return { s1: r1.status, s2: r2.status, id1: j1.data?.id, id2: j2.data?.id };
    }, { base: apiBase, pid: availProd!.id });
    expect(idemRes.s1).toBe(201);
    expect([200, 201]).toContain(idemRes.s2);
    expect(idemRes.id1).toBe(idemRes.id2);

    await page.getByTestId("nav-pesanan").click();
    await page.locator("table button", { hasText: "Detail" }).first().click();
    await page.getByTestId("btn-batal-pesanan").click();
    await expect(page.getByTestId("btn-batal-pesanan")).toHaveCount(0);

    // Negatif 7b: Logout mencabut cookie sesi sehingga /auth/me berikutnya menjadi 401
    await logoutUI(page);
    const afterLogout = await page.evaluate(async ({ base }) => {
      const r = await fetch(`${base}/api/v1/auth/me`, {
        credentials: "include",
        headers: { "X-Requested-With": "XMLHttpRequest" }
      });
      return r.status;
    }, { base: apiBase });
    expect(afterLogout).toBe(401);
  });
});
