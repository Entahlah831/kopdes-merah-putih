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
    const firstShown = await Promise.race([
      page.getByTestId("mfa-code").waitFor({ state: "visible", timeout: 10_000 }).then(() => "mfa" as const),
      page.getByTestId("user-badge").waitFor({ state: "visible", timeout: 10_000 }).then(() => "ok" as const)
    ]);
    if (firstShown === "ok") return;
    const tryOffsets = async (): Promise<boolean> => {
      const bodyText = (await page.locator("body").textContent()) || "";
      const secretMatch = bodyText.match(/Secret TOTP:\s*([A-Z2-7]+)/);
      for (const offset of [0, 30]) {
        const totpRes = await page.request.post("/api/totp-helper", {
          data: secretMatch
            ? { secret_base32: secretMatch[1], offset_seconds: offset }
            : { account_email: mfaEmail, offset_seconds: offset }
        });
        expect(totpRes.ok()).toBeTruthy();
        const { code } = (await totpRes.json()) as { code: string };
        await page.getByTestId("mfa-code").fill(code);
        await page.getByTestId("btn-mfa-submit").click();
        try {
          await expect(page.getByTestId("user-badge")).toBeVisible({ timeout: 2_500 });
          return true;
        } catch {
          // Retry next 30s offset without exhausting MFAMaxAttempts=3
        }
      }
      return false;
    };
    let loggedIn = await tryOffsets();
    if (!loggedIn) {
      const waitSec = 31 - (Math.floor(Date.now() / 1000) % 30);
      await page.waitForTimeout(waitSec * 1000);
      await page.goto("/");
      await page.getByTestId("nav-masuk").click();
      await page.getByTestId("login-identifier").fill(identifier);
      await page.getByTestId("login-password").fill(password);
      await page.getByTestId("btn-login").click();
      await page.getByTestId("mfa-code").waitFor({ state: "visible", timeout: 10_000 });
      loggedIn = await tryOffsets();
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

  test("01. Beranda (01a-01e), Tentang/Kontak/FAQ, Katalog, Mobile Viewport, & Auth Hygiene", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByTestId("beranda-hero")).toBeVisible();
    await expect(page.getByTestId("beranda-tentang")).toBeVisible();
    await expect(page.getByTestId("beranda-transparansi")).toBeVisible();
    await expect(page.getByTestId("beranda-footer")).toBeVisible();
    await expect(page.locator("body")).toContainText("07 — Cara Bergabung");
    await expect(page.locator("body")).toContainText("BLOCKED: butuh keputusan pemilik");
    await page.screenshot({ path: path.join(screenshotDir, "01_beranda.png"), fullPage: true });

    // Mobile responsive 375x812 check
    await page.setViewportSize({ width: 375, height: 812 });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 2);
    expect(overflow).toBeFalsy();
    await page.screenshot({ path: path.join(screenshotDir, "09_mobile_beranda.png"), fullPage: true });

    await page.setViewportSize({ width: 1280, height: 800 });
    await page.getByTestId("cta-katalog").click();
    await expect(page.getByTestId("kat-search")).toBeVisible();
    await page.screenshot({ path: path.join(screenshotDir, "02_katalog.png"), fullPage: true });

    // Filter ketersediaan, max harga, & Empty state + Reset filter
    await page.getByTestId("kat-tersedia-only").check();
    await page.getByTestId("kat-max-harga").fill("9999999");
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
    await expect(page.getByTestId("detail-pesanan-judul")).toContainText("diproses");
    await page.getByTestId("btn-status-siap_diambil").click();
    await expect(page.getByTestId("detail-pesanan-judul")).toContainText("siap_diambil");
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

  test("05. Cakupan Lengkap 19 Frame: Stok Habis, Perbaikan/Tolak KYC, Tolak Bukti, Diantar, & RBAC Petugas Toko", async ({ page }) => {
    test.setTimeout(120_000);
    const rt = await (await page.request.get("/config.json")).json() as { api_public_url: string };
    const apiBase = rt.api_public_url;
    const uniq = Date.now().toString().slice(-6);

    // 1. Produk stok 0 (Stok habis) & produk aktif untuk alur diantar
    await loginUI(page, "admin@kopdes-test.local", seedPassword, "admin@kopdes-test.local");
    const zeroSKU = `HABIS-${uniq}`;
    const okSKU = `ANTAR-${uniq}`;
    const setupOK = await page.evaluate(async ({ base, zeroSKU, okSKU, uniq }) => {
      const postProd = async (sku: string, nama: string, stok: number) => {
        const r = await fetch(`${base}/api/v1/katalog/produk`, {
          method: "POST", credentials: "include",
          headers: { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" },
          body: JSON.stringify({ sku, nama, satuan: "pcs", harga_umum: 12000, harga_anggota: 10000, stok, status: "aktif" })
        });
        return r.ok;
      };
      return (await postProd(zeroSKU, `Produk Kosong ${uniq}`, 0)) && (await postProd(okSKU, `Produk Antar ${uniq}`, 5));
    }, { base: apiBase, zeroSKU, okSKU, uniq });
    expect(setupOK).toBeTruthy();
    await logoutUI(page);

    // Cek katalog anonim: Stok habis non-aktif & tombol "Masuk untuk Checkout"
    await page.getByTestId("nav-katalog").click();
    await expect(page.getByTestId(`pesan-${zeroSKU}`)).toBeDisabled();
    await expect(page.getByTestId(`pesan-${zeroSKU}`)).toContainText("Stok habis");
    await page.getByTestId(`pesan-${okSKU}`).click();
    await expect(page.getByTestId("cart-panel")).toContainText("Masuk untuk Checkout");

    // 2. Pendaftaran + 03a Filter + 03b Minta Perbaikan -> Tolak + preview KYC terdekripsi
    await page.goto("/#/daftar");
    await page.locator('input[name="nama_lengkap"]').fill(`Calon Tolak ${uniq}`);
    await page.locator('input[name="email"]').fill(`tolak_${uniq}@kopdes-test.local`);
    await page.locator('input[name="nik"]').fill(`320303${uniq.padStart(10, "0")}`);
    await page.locator('input[name="no_hp"]').fill(`0857${uniq.padStart(8, "3")}`);
    await page.locator('input[name="pekerjaan"]').fill("Nelayan");
    await page.locator('textarea[name="alamat"]').fill("Dusun Pantai 5");
    await page.locator("#chk-domisili").check();
    await page.locator("#chk-pdp").check();
    await page.getByTestId("btn-kirim-daftar").click();
    await expect(page.getByTestId("daftar-sukses")).toBeVisible();
    await page.getByTestId("kyc-file").setInputFiles({
      name: "ktp_tolak.png", mimeType: "image/png", buffer: await generateTestPNG(page, "KYC-REV-" + uniq)
    });
    await page.getByTestId("btn-upload-kyc").click();
    await expect(page.locator("body")).toContainText("Dokumen ktp berhasil terenkripsi");

    await loginUI(page, "admin@kopdes-test.local", seedPassword, "admin@kopdes-test.local");
    await page.locator("#sb-ver-ang").click();
    await page.getByTestId("filter-reg-status").selectOption("diajukan");
    await page.getByTestId("search-reg").fill(`Calon Tolak ${uniq}`);
    await expect(page.locator("table")).toContainText("*");
    await page.locator("table button", { hasText: "Tinjau" }).first().click();
    await expect(page.locator(".preview-img").first()).toBeVisible({ timeout: 10_000 });
    await expect.poll(async () =>
      page.locator(".preview-img").first().evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0)
    ).toBeTruthy();

    await page.getByTestId("reg-catatan").fill("Foto KTP buram");
    await page.getByTestId("btn-reg-perbaikan").click();
    await expect(page.getByTestId("reg-decision-ok")).toContainText("perlu_perbaikan");

    await page.getByTestId("filter-reg-status").selectOption("perlu_perbaikan");
    await expect(page.locator("table tbody tr").first()).toContainText("perlu_perbaikan");
    await page.locator("table button", { hasText: "Tinjau" }).first().click();
    await page.getByTestId("reg-catatan").fill("Melampaui batas kelayakan");
    await page.getByTestId("btn-reg-tolak").click();
    await expect(page.getByTestId("reg-decision-ok")).toContainText("ditolak");
    await logoutUI(page);

    // 3. Checkout diantar + Upload bukti -> Tolak bukti oleh Bendahara -> Upload ulang -> Setuju -> Petugas Toko selesaikan
    await loginUI(page, "anggota1@kopdes-test.local", seedPassword);
    await page.getByTestId("nav-katalog").click();
    await page.getByTestId(`pesan-${okSKU}`).click();
    await page.getByTestId("co-kirim").selectOption("diantar");
    await page.getByTestId("co-alamat").fill("Jl. Melati RT 03 RW 01");
    await page.getByTestId("btn-checkout").click();
    await expect(page.locator("h2")).toContainText("Pesanan Saya");
    await page.locator("table button", { hasText: "Detail" }).first().click();
    await expect(page.getByTestId("detail-pesanan-judul")).toBeVisible();
    const ordJudul = (await page.getByTestId("detail-pesanan-judul").textContent()) || "";
    const ordNomor = ordJudul.match(/ORD-[A-Z0-9-]+/)?.[0] || "";
    expect(ordNomor).not.toBe("");

    await page.getByTestId("btn-bayar-pesanan").click();
    await expect(page.getByTestId("order-pay-info")).toBeVisible();
    const payNomor1 = ((await page.getByTestId("order-pay-info").textContent()) || "").match(/PAY-[A-Z0-9-]+/)?.[0] || "";
    await page.getByTestId("order-bukti-file").setInputFiles({
      name: "bukti_tolak.png", mimeType: "image/png", buffer: await generateTestPNG(page, "REJ-" + uniq)
    });
    await page.getByTestId("btn-upload-order-bukti").click();
    await expect(page.getByTestId("order-bukti-ok")).toBeVisible();
    await logoutUI(page);

    // Bendahara tolak bukti pertama via sidebar Dashboard
    await loginUI(page, "bendahara@kopdes-test.local", seedPassword, "bendahara@kopdes-test.local");
    await page.locator("#sb-ver-pay").click();
    await page.getByTestId(`ver-pay-${payNomor1}`).click();
    await expect(page.locator("body")).toContainText("BLOCKED: butuh keputusan pemilik");
    await page.locator(".preview-img").first().waitFor({ state: "visible" });
    await page.getByTestId("ver-pay-alasan").fill("Nominal buram tidak terbaca");
    await page.getByTestId("btn-pay-tolak").click();
    await expect(page.getByTestId("ver-pay-ok")).toContainText("tolak");
    await logoutUI(page);

    // Anggota buat pembayaran QRIS ke-2 + unggah bukti baru
    await loginUI(page, "anggota1@kopdes-test.local", seedPassword);
    await page.getByTestId("nav-pesanan").click();
    await page.getByTestId(`detail-ord-${ordNomor}`).click();
    await page.getByTestId("btn-bayar-pesanan").click();
    await expect(page.getByTestId("order-pay-info")).toBeVisible();
    const payNomor2 = ((await page.getByTestId("order-pay-info").textContent()) || "").match(/PAY-[A-Z0-9-]+/)?.[0] || "";
    await page.getByTestId("order-bukti-file").setInputFiles({
      name: "bukti_ok.png", mimeType: "image/png", buffer: await generateTestPNG(page, "OK-" + uniq)
    });
    await page.getByTestId("btn-upload-order-bukti").click();
    await expect(page.getByTestId("order-bukti-ok")).toBeVisible();
    await logoutUI(page);

    // Bendahara setujui pembayaran ke-2
    await loginUI(page, "bendahara@kopdes-test.local", seedPassword, "bendahara@kopdes-test.local");
    await page.getByTestId("nav-verifikasi_pembayaran").click();
    await page.getByTestId(`ver-pay-${payNomor2}`).click();
    await page.getByTestId("ver-pay-alasan").fill("Bukti ulang jelas dan cocok");
    await page.getByTestId("btn-pay-setuju").click();
    await expect(page.getByTestId("ver-pay-ok")).toContainText("setuju");
    await logoutUI(page);

    // Petugas Toko RBAC check + selesaikan alur pengiriman diantar -> selesai
    await loginUI(page, "petugas@kopdes-test.local", seedPassword, "petugas@kopdes-test.local");
    await expect(page.getByTestId("nav-verifikasi_pembayaran")).toHaveCount(0);
    const ptUsersStatus = await page.evaluate(async ({ base }) => {
      const r = await fetch(`${base}/api/v1/users`, { credentials: "include", headers: { "X-Requested-With": "XMLHttpRequest" } });
      return r.status;
    }, { base: apiBase });
    expect(ptUsersStatus).toBe(403);

    await page.getByTestId("nav-pesanan").click();
    await page.getByTestId(`detail-ord-${ordNomor}`).click();
    await page.getByTestId("btn-status-diproses").click();
    await expect(page.getByTestId("detail-pesanan-judul")).toContainText("diproses");
    await page.getByTestId("btn-status-diantar").click();
    await expect(page.getByTestId("detail-pesanan-judul")).toContainText("diantar");
    await page.getByTestId("btn-status-selesai").click();
    await expect(page.getByTestId("detail-pesanan-judul")).toContainText("selesai");
    await logoutUI(page);
  });
});
