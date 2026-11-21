import { APIClient } from "../api";
import { blockedBadge, el, formatRp, statusBadge } from "../dom";
import { ProdukItem, RouteKey } from "../types";

export async function renderBeranda(
  container: HTMLElement,
  api: APIClient,
  navigate: (route: RouteKey) => void
): Promise<void> {
  const pub = api.getPublic();
  const koperasiNama = pub?.koperasi?.nama_koperasi || "Koperasi Desa Merah Putih";

  const hero = el(
    "section",
    { class: "hero", "data-testid": "beranda-hero" },
    el(
      "div",
      null,
      el("div", { class: "sidebar-label" }, "02 — HERO"),
      el("h1", null, "Belanja kebutuhan dan kelola keanggotaan bersama koperasi kita"),
      el(
        "p",
        null,
        `${koperasiNama} — Satu tempat untuk keanggotaan, iuran, dan belanja kebutuhan warga. Transparan dan dikelola bersama.`
      ),
      el(
        "div",
        { class: "row" },
        el("button", { class: "btn btn-primary", type: "button", "data-testid": "cta-daftar" }, "Gabung Menjadi Anggota"),
        el("button", { class: "btn", type: "button", "data-testid": "cta-katalog" }, "Lihat Katalog")
      )
    ),
    el("div", { class: "placeholder-box" }, pub?.koperasi?.alamat_koperasi || "Foto kegiatan / produk asli koperasi")
  );
  hero.querySelector('[data-testid="cta-daftar"]')?.addEventListener("click", () => navigate("daftar"));
  hero.querySelector('[data-testid="cta-katalog"]')?.addEventListener("click", () => navigate("katalog"));
  container.appendChild(hero);

  // 03 — Ringkasan Koperasi (tanpa angka dummy)
  container.appendChild(
    el(
      "section",
      { class: "card" },
      el("div", { class: "row", style: "justify-content:space-between;" }, el("h3", null, "03 — Ringkasan Koperasi"), blockedBadge()),
      el(
        "div",
        { class: "grid-cards" },
        el("div", { class: "card card-muted" }, el("strong", null, "Status Pembayaran"), el("div", null, pub?.features?.payments_enabled ? "Aktif (QRIS / Tunai)" : "Nonaktif")),
        el("div", { class: "card card-muted" }, el("strong", null, "Status Pendaftaran"), el("div", null, pub?.features?.registration_enabled ? "Dibuka" : "Ditutup")),
        el("div", { class: "card card-muted" }, el("strong", null, "Ongkir Tetap"), el("div", null, formatRp(pub?.koperasi?.ongkir_tetap || 0))),
        el("div", { class: "card card-muted" }, el("strong", null, "Metrik Wilayah / Tahun Berdiri"), blockedBadge("BLOCKED: butuh keputusan pemilik"))
      )
    )
  );

  // 06 — Produk Pilihan
  const prodWrap = el("div", { class: "grid-cards" }, el("div", { class: "alert alert-note" }, "Memuat produk pilihan..."));
  container.appendChild(
    el(
      "section",
      { class: "card" },
      el(
        "div",
        { class: "row", style: "justify-content:space-between; margin-bottom:10px;" },
        el("h3", { style: "margin:0;" }, "06 — Produk Pilihan"),
        el("button", { class: "btn", type: "button", id: "btn-all-cat" }, "Lihat semua katalog ->")
      ),
      prodWrap
    )
  );
  container.querySelector("#btn-all-cat")?.addEventListener("click", () => navigate("katalog"));

  // 07 — Cara Bergabung
  container.appendChild(
    el(
      "section",
      { class: "card" },
      el("h3", null, "07 — Cara Bergabung"),
      el(
        "div",
        { class: "grid-cards" },
        el("div", { class: "card card-muted" }, "1. Isi formulir pendaftaran"),
        el("div", { class: "card card-muted" }, "2. Unggah KTP, Selfie KTP & KK"),
        el("div", { class: "card card-muted" }, "3. Verifikasi pengurus"),
        el("div", { class: "card card-muted" }, "4. Keputusan & kode aktivasi"),
        el("div", { class: "card card-muted" }, "5. Aktivasi sandi & login anggota")
      )
    )
  );

  // 08–09 Berita/RAT & Transparansi
  container.appendChild(
    el(
      "section",
      { class: "card" },
      el("div", { class: "row", style: "justify-content:space-between;" }, el("h3", null, "08–09 — Berita, RAT & Transparansi"), blockedBadge()),
      el(
        "p",
        { class: "alert alert-note" },
        "Catatan Privasi (01d): Tidak menampilkan NIK, alamat pribadi, kontak pribadi, bukti bayar, atau data keluarga di halaman publik. Berita/Agenda/RAT: BLOCKED: butuh keputusan pemilik."
      )
    )
  );

  try {
    const res = await api.request<ProdukItem[]>("GET", "/api/v1/katalog/produk");
    while (prodWrap.firstChild) prodWrap.removeChild(prodWrap.firstChild);
    const items = (res.data || []).slice(0, 4);
    if (items.length === 0) {
      prodWrap.appendChild(el("div", { class: "alert alert-note" }, "Belum ada produk aktif di katalog saat ini."));
      return;
    }
    for (const p of items) {
      const priceLabel = p.harga_aktif !== undefined
        ? `Harga (${p.tier_harga || "umum"}): ${formatRp(p.harga_aktif)}`
        : "Khusus anggota (masuk untuk melihat harga)";
      const staffHint = p.harga_umum !== undefined || p.harga_anggota !== undefined
        ? `Umum: ${p.harga_umum !== undefined ? formatRp(p.harga_umum) : "-"} | Anggota: ${p.harga_anggota !== undefined ? formatRp(p.harga_anggota) : "-"}`
        : priceLabel;
      prodWrap.appendChild(
        el(
          "div",
          { class: "card card-muted" },
          el("strong", null, p.nama),
          el("div", null, `SKU: ${p.sku} (${p.satuan})`),
          el("div", null, staffHint),
          el("div", { style: "margin-top:6px;" }, statusBadge(p.stok_tersedia > 0 ? "tersedia" : "habis"), " ", `Stok tersedia: ${p.stok_tersedia}`)
        )
      );
    }
  } catch (err) {
    while (prodWrap.firstChild) prodWrap.removeChild(prodWrap.firstChild);
    prodWrap.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
  }
}
