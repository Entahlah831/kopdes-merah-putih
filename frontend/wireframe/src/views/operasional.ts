import { APIClient } from "../api";
import { clear, el, formatRp, statusBadge } from "../dom";
import { PublicConfigData, UserSession } from "../types";

interface DaftarAntarRow {
  id: string;
  nomor_pesanan: string;
  status: string;
  total_bayar: number;
  nama_penerima: string;
  hp_penerima: string;
  alamat_pengantaran: string;
  label_antar: string;
}

interface AdminUserRow {
  id: string;
  nama_lengkap: string;
  email: string;
  no_hp: string;
  peran: string;
  status_aktif: boolean;
  mfa_enabled: boolean;
  nomor_anggota?: string;
}

function triggerTextDownload(filename: string, content: string): void {
  const blob = new Blob([content], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export async function renderOperasional(
  container: HTMLElement,
  api: APIClient,
  user: UserSession | null
): Promise<void> {
  if (!user || !["admin", "bendahara", "petugas_toko"].includes(user.peran)) {
    container.appendChild(el("div", { class: "alert alert-error", "data-testid": "forbidden-view" }, "Halaman operasional hanya untuk admin, bendahara, atau petugas toko."));
    return;
  }

  container.appendChild(el("h2", null, "07 — Operasional: Laporan, Daftar Antar, Produk, Iuran & Kasir Tunai"));
  container.appendChild(buildLaporanCard(api));

  if (["admin", "petugas_toko"].includes(user.peran)) {
    container.appendChild(buildDaftarAntarCard(api));
    container.appendChild(buildProdukCard(api));
  }
  if (["admin", "bendahara"].includes(user.peran)) {
    container.appendChild(buildIuranCard(api));
    container.appendChild(buildCatatTunaiCard(api));
  }
}

function buildLaporanCard(api: APIClient): HTMLElement {
  const selJenis = el("select", { "data-testid": "lap-jenis" },
    el("option", { value: "anggota" }, "Rekap Anggota"),
    el("option", { value: "tunggakan" }, "Tunggakan Iuran"),
    el("option", { value: "penjualan" }, "Penjualan Harian"),
    el("option", { value: "pembayaran" }, "Rekap Pembayaran")
  );
  const inDari = el("input", { type: "date", "data-testid": "lap-dari" });
  const inSampai = el("input", { type: "date", "data-testid": "lap-sampai" });
  const btnLoad = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-laporan-load" }, "Muat Laporan");
  const btnCSV = el("button", { class: "btn", type: "button", "data-testid": "btn-laporan-csv" }, "Unduh CSV");
  const outWrap = el("div", { style: "margin-top:10px;" }, "Klik 'Muat Laporan' untuk menampilkan data.");

  const buildQS = (asCSV: boolean): string => {
    const params = new URLSearchParams({ jenis: selJenis.value });
    if (inDari.value) params.set("dari", inDari.value);
    if (inSampai.value) params.set("sampai", inSampai.value);
    if (asCSV) params.set("format", "csv");
    return `/api/v1/laporan?${params.toString()}`;
  };

  btnLoad.addEventListener("click", async () => {
    clear(outWrap);
    try {
      const res = await api.request<Array<Record<string, unknown>>>("GET", buildQS(false));
      const rows = res.data || [];
      if (rows.length === 0) {
        outWrap.appendChild(el("div", { class: "alert alert-note" }, "Tidak ada baris data pada rentang laporan ini."));
        return;
      }
      const cols = Object.keys(rows[0]);
      const tbl = el("table", { "data-testid": "laporan-table" },
        el("thead", null, el("tr", null, ...cols.map((c) => el("th", null, c)))),
        el("tbody", null,
          ...rows.map((r) =>
            el("tr", null, ...cols.map((c) => el("td", null, r[c] === null || r[c] === undefined ? "-" : String(r[c]))))
          )
        )
      );
      outWrap.appendChild(el("div", { class: "table-wrap" }, tbl));
    } catch (err) {
      outWrap.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  btnCSV.addEventListener("click", async () => {
    try {
      const csvText = await api.fetchText(buildQS(true));
      triggerTextDownload(`laporan_${selJenis.value}.csv`, csvText);
    } catch (err) {
      clear(outWrap);
      outWrap.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  return el("div", { class: "card" },
    el("h3", null, "Laporan Operasional (GET /api/v1/laporan)"),
    el("div", { class: "grid-cards" },
      el("div", { class: "field" }, el("label", null, "Jenis Laporan"), selJenis),
      el("div", { class: "field" }, el("label", null, "Dari Tanggal (opsional)"), inDari),
      el("div", { class: "field" }, el("label", null, "Sampai Tanggal (opsional)"), inSampai)
    ),
    el("div", { class: "row" }, btnLoad, btnCSV),
    outWrap
  );
}

function buildDaftarAntarCard(api: APIClient): HTMLElement {
  const inTgl = el("input", { type: "date", "data-testid": "antar-tanggal" });
  const btnLoad = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-daftar-antar-load" }, "Muat Daftar Antar");
  const btnCSV = el("button", { class: "btn", type: "button", "data-testid": "btn-daftar-antar-csv" }, "Unduh CSV Antar");
  const outWrap = el("div", { style: "margin-top:10px;" }, "Klik 'Muat Daftar Antar' untuk melihat pesanan siap kirim.");

  const buildQS = (asCSV: boolean): string => {
    const params = new URLSearchParams();
    if (inTgl.value) params.set("tanggal", inTgl.value);
    if (asCSV) params.set("format", "csv");
    const q = params.toString();
    return q ? `/api/v1/pesanan/daftar-antar?${q}` : "/api/v1/pesanan/daftar-antar";
  };

  btnLoad.addEventListener("click", async () => {
    clear(outWrap);
    try {
      const res = await api.request<DaftarAntarRow[]>("GET", buildQS(false));
      const rows = res.data || [];
      if (rows.length === 0) {
        outWrap.appendChild(el("div", { class: "alert alert-note" }, "Tidak ada antaran aktif pada tanggal tersebut."));
        return;
      }
      const tbl = el("table", { "data-testid": "daftar-antar-table" },
        el("thead", null, el("tr", null, el("th", null, "Nomor"), el("th", null, "Penerima / HP"), el("th", null, "Alamat"), el("th", null, "Status"), el("th", null, "Label Antar"), el("th", null, "Total"))),
        el("tbody", null,
          ...rows.map((r) =>
            el("tr", null,
              el("td", null, r.nomor_pesanan),
              el("td", null, `${r.nama_penerima} (${r.hp_penerima})`),
              el("td", null, r.alamat_pengantaran),
              el("td", null, statusBadge(r.status)),
              el("td", null, r.label_antar),
              el("td", null, formatRp(r.total_bayar))
            )
          )
        )
      );
      outWrap.appendChild(el("div", { class: "table-wrap" }, tbl));
    } catch (err) {
      outWrap.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  btnCSV.addEventListener("click", async () => {
    try {
      const csvText = await api.fetchText(buildQS(true));
      triggerTextDownload("daftar_antar.csv", csvText);
    } catch (err) {
      clear(outWrap);
      outWrap.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  return el("div", { class: "card" },
    el("h3", null, "Daftar Antar & Label Kirim (GET /api/v1/pesanan/daftar-antar)"),
    el("div", { class: "row", style: "margin-bottom:8px;" },
      el("div", { style: "min-width:200px;" }, inTgl),
      btnLoad,
      btnCSV
    ),
    outWrap
  );
}

function buildProdukCard(api: APIClient): HTMLElement {
  const inSKU = el("input", { type: "text", placeholder: "BRS-5KG", "data-testid": "prod-sku" });
  const inNama = el("input", { type: "text", placeholder: "Beras Premium 5 Kg", "data-testid": "prod-nama" });
  const inSatuan = el("input", { type: "text", value: "pcs", "data-testid": "prod-satuan" });
  const inHargaUmum = el("input", { type: "number", placeholder: "75000", "data-testid": "prod-harga-umum" });
  const inHargaAnggota = el("input", { type: "number", placeholder: "70000", "data-testid": "prod-harga-anggota" });
  const inStok = el("input", { type: "number", value: "10", "data-testid": "prod-stok" });
  const inFotoURL = el("input", { type: "text", placeholder: "/assets/produk.jpg atau https://... (opsional)", "data-testid": "prod-foto-url" });
  const btnProduk = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-produk-submit" }, "Simpan Produk Baru");

  const inKatSlug = el("input", { type: "text", placeholder: "sembako", "data-testid": "kat-slug" });
  const inKatNama = el("input", { type: "text", placeholder: "Sembako Pokok", "data-testid": "kat-nama" });
  const btnKat = el("button", { class: "btn", type: "button", "data-testid": "btn-kategori-submit" }, "Tambah Kategori");
  const msg = el("div");

  btnProduk.addEventListener("click", async () => {
    clear(msg);
    const body: Record<string, unknown> = {
      sku: inSKU.value.trim(),
      nama: inNama.value.trim(),
      deskripsi: "",
      satuan: inSatuan.value.trim() || "pcs",
      stok: Number(inStok.value || 0),
      foto_url: inFotoURL.value.trim(),
      status: "aktif"
    };
    const hUmum = Number(inHargaUmum.value || 0);
    const hAnggota = Number(inHargaAnggota.value || 0);
    if (hUmum > 0) body.harga_umum = hUmum;
    if (hAnggota > 0) body.harga_anggota = hAnggota;
    try {
      const res = await api.request<{ sku: string; nama: string }>("POST", "/api/v1/katalog/produk", { body });
      msg.appendChild(el("div", { class: "alert alert-ok", "data-testid": "produk-ok" }, `Produk ${res.data.sku} (${res.data.nama}) ditambahkan.`));
    } catch (err) {
      msg.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  btnKat.addEventListener("click", async () => {
    clear(msg);
    try {
      const res = await api.request<{ slug: string; nama: string }>("POST", "/api/v1/katalog/produk", {
        body: { action: "create_kategori", slug: inKatSlug.value.trim(), nama: inKatNama.value.trim() }
      });
      msg.appendChild(el("div", { class: "alert alert-ok" }, `Kategori '${res.data.nama}' (${res.data.slug}) dibuat.`));
    } catch (err) {
      msg.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  return el("div", { class: "card" },
    el("h3", null, "Manajemen Produk & Kategori (POST /api/v1/katalog/produk)"),
    msg,
    el("div", { class: "grid-cards" },
      el("div", { class: "field" }, el("label", null, "SKU"), inSKU),
      el("div", { class: "field" }, el("label", null, "Nama Produk"), inNama),
      el("div", { class: "field" }, el("label", null, "Satuan"), inSatuan),
      el("div", { class: "field" }, el("label", null, "Harga Umum"), inHargaUmum),
      el("div", { class: "field" }, el("label", null, "Harga Anggota"), inHargaAnggota),
      el("div", { class: "field" }, el("label", null, "Stok Awal"), inStok),
      el("div", { class: "field" }, el("label", null, "Foto URL (opsional)"), inFotoURL)
    ),
    btnProduk,
    el("hr", { style: "margin:14px 0;border:none;border-top:1px solid #d9e2ec;" }),
    el("div", { class: "row" },
      el("div", { style: "min-width:160px;" }, inKatSlug),
      el("div", { style: "flex:1;min-width:180px;" }, inKatNama),
      btnKat
    )
  );
}

function buildIuranCard(api: APIClient): HTMLElement {
  const inKode = el("input", { type: "text", placeholder: "WAJIB", "data-testid": "iuran-kode" });
  const inNama = el("input", { type: "text", placeholder: "Iuran Wajib Bulanan", "data-testid": "iuran-nama" });
  const inNominal = el("input", { type: "number", placeholder: "25000", "data-testid": "iuran-nominal" });
  const inHari = el("input", { type: "number", value: "10", "data-testid": "iuran-hari" });
  const btnJenis = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-jenis-iuran-submit" }, "Buat Jenis Iuran");

  const now = new Date();
  const defPeriode = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
  const inPeriode = el("input", { type: "text", value: defPeriode, placeholder: "YYYY-MM", "data-testid": "iuran-periode" });
  const btnGen = el("button", { class: "btn", type: "button", "data-testid": "btn-generate-tagihan" }, "Generate Tagihan Periode");
  const msg = el("div");
  const listWrap = el("div", { style: "margin-top:8px;" });

  const loadJenis = async (): Promise<void> => {
    try {
      const res = await api.request<Array<{ kode: string; nama: string; nominal: number }>>("GET", "/api/v1/iuran/tagihan?scope=jenis");
      const items = res.data || [];
      clear(listWrap);
      if (items.length > 0) {
        listWrap.appendChild(el("div", { class: "alert alert-note" },
          `Jenis Iuran Aktif: ${items.map((j) => `${j.kode} (${formatRp(j.nominal)})`).join(", ")}`));
      }
    } catch {
      // Abaikan jika gagal memuat daftar jenis
    }
  };

  btnJenis.addEventListener("click", async () => {
    clear(msg);
    try {
      const res = await api.request<{ kode: string; nama: string }>("POST", "/api/v1/iuran/jenis", {
        body: {
          kode: inKode.value.trim(),
          nama: inNama.value.trim(),
          nominal: Number(inNominal.value || 0),
          hari_jatuh_tempo: Number(inHari.value || 10)
        }
      });
      msg.appendChild(el("div", { class: "alert alert-ok" }, `Jenis iuran ${res.data.kode} (${res.data.nama}) dibuat.`));
      await loadJenis();
    } catch (err) {
      msg.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  btnGen.addEventListener("click", async () => {
    clear(msg);
    try {
      const res = await api.request<{ dibuat: number; dilewati: number }>("POST", "/api/v1/iuran/jenis", {
        body: { action: "generate", periode: inPeriode.value.trim() }
      });
      msg.appendChild(el("div", { class: "alert alert-ok" }, `Generate tagihan ${inPeriode.value}: dibuat ${res.data.dibuat}, dilewati ${res.data.dilewati}.`));
    } catch (err) {
      msg.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  void loadJenis();
  return el("div", { class: "card" },
    el("h3", null, "Jenis Iuran & Generate Tagihan Bulanan (POST /api/v1/iuran/jenis)"),
    msg,
    listWrap,
    el("div", { class: "grid-cards" },
      el("div", { class: "field" }, el("label", null, "Kode"), inKode),
      el("div", { class: "field" }, el("label", null, "Nama Iuran"), inNama),
      el("div", { class: "field" }, el("label", null, "Nominal (Rp)"), inNominal),
      el("div", { class: "field" }, el("label", null, "Jatuh Tempo (tgl 1-28)"), inHari)
    ),
    btnJenis,
    el("hr", { style: "margin:14px 0;border:none;border-top:1px solid #d9e2ec;" }),
    el("div", { class: "row" }, el("div", { style: "min-width:160px;" }, inPeriode), btnGen)
  );
}

function buildCatatTunaiCard(api: APIClient): HTMLElement {
  const selJenis = el("select", { "data-testid": "tunai-jenis" },
    el("option", { value: "pesanan" }, "pesanan"),
    el("option", { value: "iuran" }, "iuran")
  );
  const inTarget = el("input", { type: "text", placeholder: "UUID pesanan atau daftar UUID tagihan pisah koma", "data-testid": "tunai-target-id" });
  const inCatatan = el("input", { type: "text", placeholder: "Diterima kasir gerai", "data-testid": "tunai-catatan" });
  const btn = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-tunai-submit" }, "Catat Pembayaran Tunai");
  const msg = el("div");

  btn.addEventListener("click", async () => {
    clear(msg);
    const raw = inTarget.value.trim();
    const body = selJenis.value === "pesanan"
      ? { jenis_transaksi: "pesanan", metode: "tunai", pesanan_id: raw, catatan_pembayar: inCatatan.value.trim() }
      : {
          jenis_transaksi: "iuran",
          metode: "tunai",
          tagihan_ids: raw.split(",").map((s) => s.trim()).filter(Boolean),
          catatan_pembayar: inCatatan.value.trim()
        };
    try {
      const res = await api.request<{ pembayaran: { nomor_pembayaran: string; nominal_total: number; status: string } }>("POST", "/api/v1/pembayaran", {
        headers: { "Idempotency-Key": api.newIdempotencyKey("tunai") },
        body
      });
      const p = res.data.pembayaran;
      msg.appendChild(el("div", { class: "alert alert-ok", "data-testid": "tunai-ok" },
        `Pembayaran tunai ${p.nomor_pembayaran} (${formatRp(p.nominal_total)}) tercatat dengan status '${p.status}'. Gunakan 04b Verifikasi Pembayaran (dual-control) untuk verifikasi akhir.`));
    } catch (err) {
      msg.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  return el("div", { class: "card" },
    el("h3", null, "Catat Pembayaran Tunai Kasir (POST /api/v1/pembayaran)"),
    msg,
    el("div", { class: "grid-cards" },
      el("div", { class: "field" }, el("label", null, "Jenis Transaksi"), selJenis),
      el("div", { class: "field" }, el("label", null, "UUID Pesanan / Tagihan (pisah koma)"), inTarget),
      el("div", { class: "field" }, el("label", null, "Catatan Pembayar"), inCatatan)
    ),
    btn
  );
}

export async function renderPengaturanUser(
  container: HTMLElement,
  api: APIClient,
  user: UserSession | null
): Promise<void> {
  if (!user || user.peran !== "admin") {
    container.appendChild(el("div", { class: "alert alert-error", "data-testid": "forbidden-view" }, "Halaman Pengaturan & Pengguna hanya untuk admin."));
    return;
  }

  container.appendChild(el("h2", null, "08 — Pengaturan Koperasi & Manajemen Pengguna"));
  container.appendChild(await buildPengaturanFormCard(api));
  container.appendChild(await buildUsersManagementCard(api));
}

async function buildPengaturanFormCard(api: APIClient): Promise<HTMLElement> {
  const kop = api.getPublic()?.koperasi;
  const inNama = el("input", { type: "text", value: kop?.nama_koperasi || "Kopdes Merah Putih", "data-testid": "set-nama-koperasi" });
  const inAlamat = el("input", { type: "text", value: kop?.alamat_koperasi || "", "data-testid": "set-alamat-koperasi" });
  const inTelp = el("input", { type: "text", value: kop?.telepon_koperasi || "", "data-testid": "set-telepon-koperasi" });
  const selDigits = el("select", { "data-testid": "set-qris-digits" },
    el("option", { value: "3" }, "3 digit kode unik"),
    el("option", { value: "4" }, "4 digit kode unik")
  );
  selDigits.value = String(kop?.qris_unique_digits === 4 ? 4 : 3);
  const inOngkir = el("input", { type: "number", value: String(kop?.ongkir_tetap ?? 0), "data-testid": "set-ongkir-tetap" });
  const inKebijakan = el("textarea", { rows: "2", "data-testid": "set-kebijakan-teks" }, kop?.kebijakan_teks || "");
  const selRek = el("select", { "data-testid": "set-rekening-id" }, el("option", { value: "" }, "(Tetap gunakan rekening aktif saat ini)"));
  const btnSave = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-pengaturan-save" }, "Simpan Pengaturan Koperasi");
  const msg = el("div");

  try {
    const rekRes = await api.request<Array<{ id: string; nama_merchant: string; nama_bank: string }>>("GET", "/api/v1/pembayaran?rekening=true");
    for (const r of rekRes.data || []) {
      const opt = el("option", { value: r.id }, `${r.nama_merchant} (${r.nama_bank})`);
      if (kop?.active_qris?.rekening_id === r.id) opt.selected = true;
      selRek.appendChild(opt);
    }
  } catch {
    // Opsional bila daftar rekening kosong
  }

  btnSave.addEventListener("click", async () => {
    clear(msg);
    try {
      const body: Record<string, unknown> = {
        nama_koperasi: inNama.value.trim(),
        alamat_koperasi: inAlamat.value.trim(),
        telepon_koperasi: inTelp.value.trim(),
        qris_unique_digits: Number(selDigits.value || 3),
        ongkir_tetap: Number(inOngkir.value || 0),
        kebijakan_teks: inKebijakan.value.trim()
      };
      if (selRek.value) body.active_rekening_qris_id = selRek.value;
      await api.request("PUT", "/api/v1/pengaturan", { body });
      await api.request<PublicConfigData>("GET", "/api/v1/public/config");
      msg.appendChild(el("div", { class: "alert alert-ok", "data-testid": "pengaturan-ok" }, "Pengaturan koperasi berhasil diperbarui dan dicatat di audit log."));
    } catch (err) {
      msg.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  return el("div", { class: "card" },
    el("h3", null, "Pengaturan Koperasi Tunggal (PUT /api/v1/pengaturan)"),
    msg,
    el("div", { class: "grid-cards" },
      el("div", { class: "field" }, el("label", null, "Nama Koperasi"), inNama),
      el("div", { class: "field" }, el("label", null, "Telepon Koperasi"), inTelp),
      el("div", { class: "field" }, el("label", null, "Digit Unik QRIS"), selDigits),
      el("div", { class: "field" }, el("label", null, "Ongkir Tetap (Rp)"), inOngkir),
      el("div", { class: "field" }, el("label", null, "Rekening QRIS Aktif"), selRek)
    ),
    el("div", { class: "field" }, el("label", null, "Alamat Koperasi"), inAlamat),
    el("div", { class: "field" }, el("label", null, "Kebijakan Koperasi"), inKebijakan),
    btnSave
  );
}

async function buildUsersManagementCard(api: APIClient): Promise<HTMLElement> {
  const tblWrap = el("div", null, "Memuat daftar pengguna...");
  const actionMsg = el("div");

  const loadUsers = async (): Promise<void> => {
    clear(tblWrap);
    try {
      const res = await api.request<AdminUserRow[]>("GET", "/api/v1/users?limit=50");
      const rows = res.data || [];
      const tbl = el("table", { "data-testid": "users-table" },
        el("thead", null, el("tr", null, el("th", null, "Nama"), el("th", null, "Email / HP"), el("th", null, "Peran"), el("th", null, "Status / MFA"), el("th", null, "Aksi Admin"))),
        el("tbody", null, ...rows.map((u) => buildUserTableRow(u, api, actionMsg, loadUsers)))
      );
      tblWrap.appendChild(el("div", { class: "table-wrap" }, tbl));
    } catch (err) {
      tblWrap.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  };

  const inNama = el("input", { type: "text", placeholder: "Nama Staf Baru", "data-testid": "user-nama" });
  const inEmail = el("input", { type: "email", placeholder: "staf@kopdes.local", "data-testid": "user-email" });
  const inHP = el("input", { type: "text", placeholder: "081234567890", "data-testid": "user-hp" });
  const selPeran = el("select", { "data-testid": "user-peran" },
    el("option", { value: "petugas_toko" }, "petugas_toko"),
    el("option", { value: "bendahara" }, "bendahara"),
    el("option", { value: "admin" }, "admin"),
    el("option", { value: "anggota" }, "anggota")
  );
  const inPwd = el("input", { type: "password", placeholder: "Sandi >=10 karakter (atau kosongkan utk kode aktivasi)", "data-testid": "user-password" });
  const btnCreate = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-user-create" }, "Tambah Akun Pengguna");

  btnCreate.addEventListener("click", async () => {
    clear(actionMsg);
    try {
      const res = await api.request<{ id: string; activation_code?: string }>("POST", "/api/v1/users", {
        body: {
          nama_lengkap: inNama.value.trim(),
          email: inEmail.value.trim(),
          no_hp: inHP.value.trim(),
          peran: selPeran.value,
          password: inPwd.value
        }
      });
      const codeInfo = res.data.activation_code ? ` | Kode Aktivasi 1x Pakai: ${res.data.activation_code}` : "";
      actionMsg.appendChild(el("div", { class: "alert alert-ok", "data-testid": "user-action-ok" }, `Akun dibuat (ID: ${res.data.id})${codeInfo}`));
      await loadUsers();
    } catch (err) {
      actionMsg.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  await loadUsers();
  return el("div", { class: "card" },
    el("h3", null, "Manajemen Pengguna, Reset Sandi & Reset MFA (/api/v1/users)"),
    actionMsg,
    tblWrap,
    el("hr", { style: "margin:14px 0;border:none;border-top:1px solid #d9e2ec;" }),
    el("h4", null, "Buat Akun Staf / Pengguna Baru"),
    el("div", { class: "grid-cards" },
      el("div", { class: "field" }, el("label", null, "Nama Lengkap"), inNama),
      el("div", { class: "field" }, el("label", null, "Email"), inEmail),
      el("div", { class: "field" }, el("label", null, "No. HP"), inHP),
      el("div", { class: "field" }, el("label", null, "Peran"), selPeran),
      el("div", { class: "field" }, el("label", null, "Sandi Awal (opsional)"), inPwd)
    ),
    btnCreate
  );
}

function buildUserTableRow(
  u: AdminUserRow,
  api: APIClient,
  actionMsg: HTMLElement,
  reloadUsers: () => Promise<void>
): HTMLTableRowElement {
  const btnResetCode = el("button", { class: "btn", type: "button", style: "padding:4px 8px;" }, "Kode Reset Sandi");
  const btnResetMFA = el("button", { class: "btn", type: "button", style: "padding:4px 8px;" }, "Reset MFA");
  const btnToggle = el("button", { class: "btn btn-danger", type: "button", style: "padding:4px 8px;" }, u.status_aktif ? "Nonaktifkan" : "Aktifkan");

  const patchUser = async (payload: Record<string, unknown>, label: string): Promise<void> => {
    clear(actionMsg);
    try {
      const res = await api.request<{ reset_code?: string }>("PATCH", `/api/v1/users/${encodeURIComponent(u.id)}`, { body: payload });
      const extra = res.data.reset_code ? ` | Kode Reset 1x Pakai: ${res.data.reset_code}` : "";
      actionMsg.appendChild(el("div", { class: "alert alert-ok", "data-testid": "user-action-ok" }, `${label} untuk ${u.nama_lengkap} berhasil${extra}`));
      await reloadUsers();
    } catch (err) {
      actionMsg.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  };

  btnResetCode.addEventListener("click", () => { void patchUser({ issue_reset_code: true }, "Terbitkan kode reset sandi"); });
  btnResetMFA.addEventListener("click", () => { void patchUser({ reset_mfa: true }, "Reset MFA"); });
  btnToggle.addEventListener("click", () => { void patchUser({ status_aktif: !u.status_aktif }, u.status_aktif ? "Nonaktifkan akun" : "Aktifkan akun"); });

  return el("tr", null,
    el("td", null, u.nama_lengkap),
    el("td", null, `${u.email} / ${u.no_hp}`),
    el("td", null, u.peran),
    el("td", null, statusBadge(u.status_aktif ? "aktif" : "nonaktif"), el("span", { style: "margin-left:6px;" }, u.mfa_enabled ? "MFA: Ya" : "MFA: Belum")),
    el("td", null, el("div", { class: "row" }, btnResetCode, btnResetMFA, btnToggle))
  );
}
