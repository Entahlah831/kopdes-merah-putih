
import { APIClient } from "../api";
import { el, formatRp, statusBadge } from "../dom";
import { ProdukItem, RouteKey } from "../types";

export async function renderBeranda(
  container: HTMLElement,
  api: APIClient,
  navigate: (route: RouteKey) => void
): Promise<void> {
  const pub = api.getPublic();
  const koperasiNama =
    pub?.koperasi?.nama_koperasi || "Koperasi Desa Merah Putih";

  // HERO
  const hero = el(
    "section",
    { class: "hero", "data-testid": "beranda-hero" },
    el(
      "div",
      { class: "hero-content" },
      el("div", { class: "hero-eyebrow" }, "KOPERASI DESA MERAH PUTIH"),
      el(
        "h1",
        null,
        "Belanja kebutuhan dan berkembang bersama koperasi"
      ),
      el(
        "p",
        null,
        `${koperasiNama} hadir sebagai tempat untuk keanggotaan, pembayaran iuran, dan memenuhi kebutuhan warga. Dikelola bersama untuk memberikan manfaat bagi anggota.`
      ),
      el(
        "div",
        { class: "row hero-actions" },
        el(
          "button",
          {
            class: "btn btn-primary",
            type: "button",
            "data-testid": "cta-daftar"
          },
          "Gabung Menjadi Anggota"
        ),
        el(
          "button",
          {
            class: "btn btn-outline",
            type: "button",
            "data-testid": "cta-katalog"
          },
          "Lihat Katalog"
        )
      )
    ),
    el(
      "div",
      { class: "placeholder-box" },
      pub?.koperasi?.alamat_koperasi || "Bersama membangun ekonomi desa"
    )
  );

  hero
    .querySelector('[data-testid="cta-daftar"]')
    ?.addEventListener("click", () => navigate("daftar"));

  hero
    .querySelector('[data-testid="cta-katalog"]')
    ?.addEventListener("click", () => navigate("katalog"));

  container.appendChild(hero);

  // RINGKASAN KOPERASI
  container.appendChild(
    el(
      "section",
      { class: "card", "data-testid": "beranda-ringkasan" },
      el("h3", null, "Ringkasan Koperasi"),
      el(
        "div",
        { class: "grid-cards" },
        el(
          "div",
          { class: "card card-muted" },
          el("strong", null, "Pembayaran"),
          el(
            "div",
            null,
            pub?.features?.payments_enabled
              ? "Pembayaran tersedia"
              : "Belum tersedia"
          )
        ),
        el(
          "div",
          { class: "card card-muted" },
          el("strong", null, "Pendaftaran Anggota"),
          el(
            "div",
            null,
            pub?.features?.registration_enabled
              ? "Pendaftaran dibuka"
              : "Pendaftaran ditutup"
          )
        ),
        el(
          "div",
          { class: "card card-muted" },
          el("strong", null, "Ongkos Kirim"),
          el(
            "div",
            null,
            pub?.koperasi?.ongkir_tetap !== undefined
              ? formatRp(pub.koperasi.ongkir_tetap)
              : "Informasi belum tersedia"
          )
        )
      )
    )
  );

  // TENTANG KOPERASI & UNIT USAHA
  container.appendChild(
    el(
      "section",
      { class: "card", "data-testid": "beranda-tentang" },
      el("h3", null, "Tentang Koperasi"),
      el(
        "p",
        null,
        `${koperasiNama} merupakan wadah ekonomi bersama yang mendukung kebutuhan warga melalui layanan keanggotaan, pembayaran iuran, dan toko kebutuhan sehari-hari.`
      ),
      el(
        "div",
        { class: "grid-cards" },
        el(
          "div",
          { class: "card card-muted" },
          el("strong", null, "Layanan Toko"),
          el(
            "div",
            null,
            pub?.koperasi?.alamat_koperasi ||
              "Menyediakan kebutuhan sehari-hari bagi warga."
          )
        ),
        el(
          "div",
          { class: "card card-muted" },
          el("strong", null, "Keanggotaan"),
          el(
            "div",
            null,
            "Mendukung kegiatan ekonomi yang dikelola secara bersama."
          )
        )
      )
    )
  );

  // PRODUK PILIHAN
  const prodWrap = el(
    "div",
    { class: "grid-cards" },
    el("div", { class: "alert alert-note" }, "Memuat produk pilihan...")
  );

  container.appendChild(
    el(
      "section",
      { class: "card", "data-testid": "beranda-produk" },
      el(
        "div",
        { class: "section-heading" },
        el("h3", null, "Produk Pilihan"),
        el(
          "button",
          {
            class: "btn btn-outline",
            type: "button",
            id: "btn-all-cat"
          },
          "Lihat Semua Katalog"
        )
      ),
      prodWrap
    )
  );

  container
    .querySelector("#btn-all-cat")
    ?.addEventListener("click", () => navigate("katalog"));

  // CARA BERGABUNG
  container.appendChild(
    el(
      "section",
      { class: "card", "data-testid": "beranda-cara-bergabung" },
      el("h3", null, "Cara Bergabung"),
      el(
        "div",
        { class: "grid-cards join-steps" },
        el("div", { class: "card card-muted" }, el("span", { class: "step-number" }, "01"), el("strong", null, "Isi Formulir"), el("p", null, "Lengkapi formulir pendaftaran anggota.")),
        el("div", { class: "card card-muted" }, el("span", { class: "step-number" }, "02"), el("strong", null, "Lengkapi Dokumen"), el("p", null, "Unggah KTP, selfie dengan KTP, dan Kartu Keluarga.")),
        el("div", { class: "card card-muted" }, el("span", { class: "step-number" }, "03"), el("strong", null, "Proses Verifikasi"), el("p", null, "Pengurus akan memeriksa data pendaftaran.")),
        el("div", { class: "card card-muted" }, el("span", { class: "step-number" }, "04"), el("strong", null, "Persetujuan"), el("p", null, "Tunggu keputusan dari pengurus koperasi.")),
        el("div", { class: "card card-muted" }, el("span", { class: "step-number" }, "05"), el("strong", null, "Aktivasi Akun"), el("p", null, "Aktifkan akun untuk mulai menggunakan layanan anggota."))
      )
    )
  );

  // BERITA, RAT, STRUKTUR & TRANSPARANSI
  container.appendChild(
    el(
      "section",
      { class: "card", "data-testid": "beranda-transparansi" },
      el("h3", null, "Informasi dan Transparansi"),
      el(
        "p",
        null,
        "Koperasi mengutamakan pengelolaan yang terbuka dan bertanggung jawab. Informasi kegiatan, agenda, dan laporan koperasi dapat disampaikan melalui kanal resmi koperasi."
      ),
      el(
        "div",
        { class: "alert alert-note" },
        "Informasi pribadi anggota dan dokumen yang bersifat rahasia hanya dapat diakses oleh pihak yang berwenang."
      )
    )
  );

  // KONTAK & CTA
  const footer = el(
    "footer",
    { class: "card", "data-testid": "beranda-footer" },
    el("h3", null, "Bergabung Bersama Koperasi"),
    el(
      "p",
      null,
      "Mari ikut mendukung kegiatan ekonomi desa melalui koperasi. Daftar menjadi anggota atau lihat produk yang tersedia."
    ),
    el(
      "div",
      { class: "grid-cards" },
      el(
        "div",
        { class: "card card-muted" },
        el("strong", null, "Alamat Koperasi"),
        el(
          "div",
          null,
          pub?.koperasi?.alamat_koperasi || "Informasi alamat belum tersedia."
        )
      )
    ),
    el(
      "div",
      { class: "row footer-actions" },
      el(
        "button",
        {
          class: "btn btn-primary",
          type: "button",
          id: "btn-footer-daftar"
        },
        "Gabung Menjadi Anggota"
      ),
      el(
        "button",
        {
          class: "btn btn-outline",
          type: "button",
          id: "btn-footer-katalog"
        },
        "Lihat Katalog"
      )
    )
  );

  footer
    .querySelector("#btn-footer-daftar")
    ?.addEventListener("click", () => navigate("daftar"));

  footer
    .querySelector("#btn-footer-katalog")
    ?.addEventListener("click", () => navigate("katalog"));

  container.appendChild(footer);

  // AMBIL DATA PRODUK DARI API
  try {
    const res = await api.request<ProdukItem[]>(
      "GET",
      "/api/v1/katalog/produk"
    );

    while (prodWrap.firstChild) {
      prodWrap.removeChild(prodWrap.firstChild);
    }

    const items = (res.data || []).slice(0, 4);

    if (items.length === 0) {
      prodWrap.appendChild(
        el(
          "div",
          { class: "alert alert-note" },
          "Belum ada produk aktif di katalog saat ini."
        )
      );
      return;
    }

    for (const p of items) {
      const priceLabel =
        p.harga_aktif !== undefined
          ? `Harga (${p.tier_harga || "umum"}): ${formatRp(p.harga_aktif)}`
          : "Masuk sebagai anggota untuk melihat harga";

      const staffHint =
        p.harga_umum !== undefined || p.harga_anggota !== undefined
          ? `Umum: ${
              p.harga_umum !== undefined ? formatRp(p.harga_umum) : "-"
            } | Anggota: ${
              p.harga_anggota !== undefined ? formatRp(p.harga_anggota) : "-"
            }`
          : priceLabel;

      prodWrap.appendChild(
        el(
          "div",
          { class: "card card-muted product-card" },
          el("strong", null, p.nama),
          el("div", { class: "product-meta" }, `SKU: ${p.sku} · ${p.satuan}`),
          el("div", { class: "product-price" }, staffHint),
          el(
            "div",
            { class: "product-stock" },
            statusBadge(p.stok_tersedia > 0 ? "tersedia" : "habis"),
            " ",
            `Stok tersedia: ${p.stok_tersedia}`
          )
        )
      );
    }
  } catch (err) {
    while (prodWrap.firstChild) {
      prodWrap.removeChild(prodWrap.firstChild);
    }

    prodWrap.appendChild(
      el(
        "div",
        { class: "alert alert-error" },
        (err as Error).message
      )
    );
  }
}
