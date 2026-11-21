import { APIClient, startTerminalPoll } from "../api";
import { blockedBadge, clear, el, formatRp, statusBadge } from "../dom";
import { validateAndCompressImage } from "../image";
import { CartEntry, ProdukItem, RouteKey, UserSession } from "../types";

export async function renderKatalog(
  container: HTMLElement,
  api: APIClient,
  user: UserSession | null,
  cart: CartEntry[],
  onCartChange: () => void,
  navigate: (r: RouteKey) => void
): Promise<void> {
  const feedback = el("div");
  const searchInput = el("input", { type: "text", placeholder: "Cari produk...", "data-testid": "kat-search" });
  const katSelect = el("select", { "data-testid": "kat-kategori" }, el("option", { value: "" }, "Semua Kategori"));
  const availCheck = el("input", { type: "checkbox", style: "width:auto;", "data-testid": "kat-tersedia-only" });
  const maxHarga = el("input", { type: "number", placeholder: "Harga maksimal (opsional)", "data-testid": "kat-max-harga" });
  const btnReset = el("button", { class: "btn", type: "button", "data-testid": "btn-reset-filter" }, "Reset filter");
  const grid = el("div", { class: "grid-cards" });
  const cartPanel = el("div", { class: "card card-muted", "data-testid": "cart-panel" });

  let allProducts: ProdukItem[] = [];

  const renderCart = (): void => {
    clear(cartPanel);
    cartPanel.append(
      el("h3", null, `Ringkasan Pilihan Lokal (${cart.reduce((s, c) => s + c.kuantitas, 0)} item)`),
      el("p", { class: "alert alert-note" }, "Checkout langsung tanpa keranjang server (aturan backend ramping).")
    );
    if (cart.length === 0) {
      cartPanel.appendChild(el("div", { class: "alert alert-note" }, "Belum ada produk dipilih. Klik Pesan pada kartu produk."));
      return;
    }
    let subtotal = 0;
    const ul = el("ul");
    for (const c of cart) {
      const unitPrice = user?.anggota_id && c.produk.harga_anggota ? c.produk.harga_anggota : (c.produk.harga_umum || c.produk.harga_aktif || 0);
      subtotal += unitPrice * c.kuantitas;
      const rmBtn = el("button", { class: "btn", type: "button", style: "padding:2px 8px;margin-left:6px;" }, "Hapus");
      rmBtn.addEventListener("click", () => {
        const idx = cart.indexOf(c);
        if (idx >= 0) cart.splice(idx, 1);
        onCartChange();
        renderCart();
      });
      ul.appendChild(el("li", null, `${c.produk.nama} x ${c.kuantitas} = ${formatRp(unitPrice * c.kuantitas)} `, rmBtn));
    }
    cartPanel.appendChild(ul);
    cartPanel.appendChild(el("strong", null, `Perkiraan Subtotal: ${formatRp(subtotal)}`));

    if (!user) {
      const btnGoLogin = el("button", { class: "btn btn-primary", type: "button", style: "margin-top:8px;" }, "Masuk untuk Checkout");
      btnGoLogin.addEventListener("click", () => navigate("masuk"));
      cartPanel.append(
        el("div", { class: "alert alert-note", style: "margin-top:8px;" }, "KEPUTUSAN PEMILIK: Tamu wajib memiliki akun terautentikasi untuk checkout."),
        btnGoLogin
      );
      return;
    }

    const inNama = el("input", { type: "text", value: user.nama || "Pembeli", "data-testid": "co-nama" });
    const inHP = el("input", { type: "text", placeholder: "0812xxxxxxxx", value: "081234567890", "data-testid": "co-hp" });
    const inKirim = el("select", { "data-testid": "co-kirim" },
      el("option", { value: "ambil_sendiri" }, "Ambil di gerai"),
      el("option", { value: "diantar" }, "Diantar")
    );
    const inBayar = el("select", { "data-testid": "co-bayar" },
      el("option", { value: "qris" }, "QRIS Statis"),
      ...(user.peran === "admin" || user.peran === "bendahara" || user.peran === "petugas_toko"
        ? [el("option", { value: "tunai" }, "Tunai (kasir)")]
        : [])
    );
    const inAlamat = el("input", { type: "text", placeholder: "Alamat pengiriman (bila diantar)", "data-testid": "co-alamat" });
    const coErr = el("div");
    const btnCheckout = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-checkout" }, "Buat Pesanan");

    btnCheckout.addEventListener("click", async () => {
      coErr.textContent = "";
      btnCheckout.disabled = true;
      try {
        await api.request("POST", "/api/v1/toko/checkout", {
          headers: { "Idempotency-Key": api.newIdempotencyKey("co") },
          body: {
            items: cart.map((c) => ({ produk_id: c.produk.id, kuantitas: c.kuantitas })),
            metode_pengiriman: inKirim.value,
            metode_pembayaran: inBayar.value,
            nama_penerima: inNama.value.trim(),
            hp_penerima: inHP.value.trim(),
            alamat_pengantaran: inAlamat.value.trim()
          }
        });
        cart.splice(0, cart.length);
        onCartChange();
        navigate("pesanan");
      } catch (err) {
        coErr.appendChild(el("div", { class: "alert alert-error", "data-testid": "checkout-error" }, (err as Error).message));
      } finally {
        btnCheckout.disabled = false;
      }
    });

    cartPanel.append(
      coErr,
      el("div", { class: "field", style: "margin-top:8px;" }, el("label", null, "Nama Penerima"), inNama),
      el("div", { class: "field" }, el("label", null, "HP Penerima"), inHP),
      el("div", { class: "field" }, el("label", null, "Pengiriman"), inKirim),
      el("div", { class: "field" }, el("label", null, "Metode Bayar"), inBayar),
      el("div", { class: "field" }, el("label", null, "Alamat (jika diantar)"), inAlamat),
      btnCheckout
    );
  };

  const applyFilter = (): void => {
    clear(grid);
    const q = searchInput.value.trim().toLowerCase();
    const kat = katSelect.value;
    const onlyAvail = availCheck.checked;
    const maxP = Number(maxHarga.value || 0);

    const filtered = allProducts.filter((p) => {
      if (q && !p.nama.toLowerCase().includes(q) && !p.sku.toLowerCase().includes(q)) return false;
      if (kat && p.kategori_id !== kat) return false;
      if (onlyAvail && p.stok_tersedia <= 0) return false;
      const eff = p.harga_anggota ?? p.harga_umum ?? 0;
      if (maxP > 0 && eff > maxP) return false;
      return true;
    });

    if (filtered.length === 0) {
      grid.appendChild(el("div", { class: "alert alert-note", "data-testid": "katalog-empty" }, "Tidak ada produk sesuai filter pencarian. Tekan Reset filter untuk melihat semua item."));
      return;
    }

    for (const p of filtered) {
      const avail = p.stok_tersedia > 0 && p.status === "aktif";
      const btnPesan = el(
        "button",
        { class: "btn btn-primary", type: "button", disabled: !avail, "data-testid": `pesan-${p.sku}` },
        avail ? "Pesan" : "Stok habis"
      );
      btnPesan.addEventListener("click", () => {
        const existing = cart.find((c) => c.produk.id === p.id);
        if (existing) {
          if (existing.kuantitas < p.stok_tersedia) existing.kuantitas += 1;
        } else {
          cart.push({ produk: p, kuantitas: 1 });
        }
        onCartChange();
        renderCart();
      });
      grid.appendChild(
        el(
          "div",
          { class: "card", "data-testid": `prod-card-${p.sku}` },
          el("strong", null, p.nama),
          el("div", { style: "font-size:12px;color:var(--subtle);" }, `SKU: ${p.sku} • ${p.nama_kategori || "Umum"}`),
          el("div", null, p.harga_aktif !== undefined ? `Harga (${p.tier_harga || "umum"}): ${formatRp(p.harga_aktif)}` : "Khusus anggota"),
          ...(p.harga_umum !== undefined ? [el("div", { style: "font-size:12px;" }, `Umum: ${formatRp(p.harga_umum)} | Anggota: ${p.harga_anggota !== undefined ? formatRp(p.harga_anggota) : "-"}`)] : []),
          el("div", { style: "margin:6px 0;" }, statusBadge(avail ? (p.stok_tersedia <= 3 ? "stok_terbatas" : "tersedia") : "habis"), ` Stok tersedia: ${p.stok_tersedia}`),
          btnPesan
        )
      );
    }
  };

  searchInput.addEventListener("input", applyFilter);
  katSelect.addEventListener("change", applyFilter);
  availCheck.addEventListener("change", applyFilter);
  maxHarga.addEventListener("input", applyFilter);
  btnReset.addEventListener("click", () => {
    searchInput.value = "";
    katSelect.value = "";
    availCheck.checked = false;
    maxHarga.value = "";
    applyFilter();
  });

  container.append(
    el("div", { class: "row", style: "justify-content:space-between;" }, el("h2", null, "05 — Katalog Produk"), blockedBadge("Sort Terpopuler & Varian: BLOCKED")),
    feedback,
    el(
      "div",
      { class: "grid-2" },
      el(
        "div",
        null,
        el(
          "div",
          { class: "card row" },
          el("div", { style: "flex:1;min-width:160px;" }, searchInput),
          el("div", { style: "width:170px;" }, katSelect),
          el("div", { style: "width:150px;" }, maxHarga),
          el("label", { class: "row", style: "width:auto;" }, availCheck, "Tersedia saja"),
          btnReset
        ),
        grid
      ),
      cartPanel
    )
  );
  renderCart();

  try {
    const [prodRes, katRes] = await Promise.all([
      api.request<ProdukItem[]>("GET", "/api/v1/katalog/produk"),
      api.request<Array<{ id: string; nama: string }>>("GET", "/api/v1/katalog/produk?tab=kategori")
    ]);
    allProducts = prodRes.data || [];
    for (const k of katRes.data || []) {
      katSelect.appendChild(el("option", { value: k.id }, k.nama));
    }
    applyFilter();
  } catch (err) {
    feedback.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
  }
}

export async function renderPesanan(
  container: HTMLElement,
  api: APIClient,
  user: UserSession | null,
  registerCleanup: (fn: () => void) => void
): Promise<void> {
  if (!user) {
    container.appendChild(el("div", { class: "alert alert-error" }, "Silakan login terlebih dahulu untuk melihat daftar pesanan."));
    return;
  }

  const feedback = el("div");
  const listWrap = el("div", { class: "card" }, "Memuat daftar pesanan...");
  const detailWrap = el("div", { class: "card", style: "display:none;" });
  let stopPoll: (() => void) | null = null;
  registerCleanup(() => {
    if (stopPoll) stopPoll();
  });

  interface PesananRow {
    id: string;
    nomor_pesanan: string;
    status: string;
    stok_status?: string;
    metode_pengiriman: string;
    metode_pembayaran: string;
    total_bayar: number;
    subtotal: number;
    diskon: number;
    ongkir: number;
    nama_penerima: string;
    alamat_pengantaran?: string;
    items?: Array<{ nama_produk: string; kuantitas: number; harga_satuan: number; subtotal: number }>;
  }

  const openDetail = async (id: string): Promise<void> => {
    if (stopPoll) stopPoll();
    detailWrap.style.display = "block";
    clear(detailWrap);
    detailWrap.appendChild(el("div", { class: "alert alert-note" }, "Memuat detail pesanan..."));

    const drawDetail = (ord: PesananRow): void => {
      clear(detailWrap);
      const steps = ["baru", "menunggu_pembayaran", "dibayar", "diproses", ord.metode_pengiriman === "diantar" ? "diantar" : "siap_diambil", "selesai"];
      const idx = steps.indexOf(ord.status);
      const stepper = el("div", { class: "stepper" },
        ...steps.map((s, i) => el("div", { class: `step-item ${i <= idx ? "done" : ""}` }, s))
      );

      const tbl = el("table", null,
        el("thead", null, el("tr", null, el("th", null, "Produk"), el("th", null, "Varian"), el("th", null, "Qty"), el("th", null, "Harga"), el("th", null, "Subtotal"))),
        el("tbody", null,
          ...(ord.items || []).map((it) =>
            el("tr", null,
              el("td", null, it.nama_produk),
              el("td", null, blockedBadge("SKU tunggal")),
              el("td", null, it.kuantitas),
              el("td", null, formatRp(it.harga_satuan)),
              el("td", null, formatRp(it.subtotal))
            )
          )
        )
      );

      const actionBar = el("div", { class: "row", style: "margin-top:10px;" });
      if (ord.status === "baru" || ord.status === "menunggu_pembayaran") {
        const btnBayar = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-bayar-pesanan" }, "Buat Pembayaran QRIS & Upload Bukti");
        const payZone = el("div", { style: "margin-top:10px;width:100%;" });
        btnBayar.addEventListener("click", async () => {
          btnBayar.disabled = true;
          clear(payZone);
          try {
            const pRes = await api.request<{ pembayaran: { id: string; nomor_pembayaran: string; nominal_pokok: number; kode_unik: number; nominal_total: number; status: string } }>("POST", "/api/v1/pembayaran", {
              headers: { "Idempotency-Key": api.newIdempotencyKey("payord") },
              body: { jenis_transaksi: "pesanan", metode: "qris", pesanan_id: ord.id }
            });
            const pay = pRes.data.pembayaran;
            const inFile = el("input", { type: "file", accept: "image/jpeg,image/png", "data-testid": "order-bukti-file" });
            const btnUp = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-upload-order-bukti" }, "Unggah Bukti Bayar");
            const upMsg = el("div");
            btnUp.addEventListener("click", async () => {
              upMsg.textContent = "";
              const f = inFile.files?.[0];
              if (!f) {
                upMsg.appendChild(el("div", { class: "alert alert-error" }, "Pilih foto JPG/PNG bukti bayar."));
                return;
              }
              btnUp.disabled = true;
              try {
                const img = await validateAndCompressImage(f);
                const fd = new FormData();
                fd.append("pembayaran_id", pay.id);
                fd.append("file", img.blob, img.filename);
                await api.request("POST", "/api/v1/pembayaran", { formData: fd });
                upMsg.appendChild(el("div", { class: "alert alert-ok", "data-testid": "order-bukti-ok" }, "Bukti terenkripsi diterima. Menunggu verifikasi bendahara."));
                stopPoll = startTerminalPoll(
                  async (sig) => (await api.request<{ pembayaran: { status: string } }>("GET", `/api/v1/pembayaran?id=${encodeURIComponent(pay.id)}`, { signal: sig })).data.pembayaran,
                  (p) => p.status,
                  (p, stopped) => {
                    upMsg.textContent = `Status pembayaran: ${p.status}${stopped ? " (polling berhenti otomatis)" : ""}`;
                  }
                );
              } catch (e) {
                upMsg.appendChild(el("div", { class: "alert alert-error", "data-testid": "upload-error" }, (e as Error).message));
              } finally {
                btnUp.disabled = false;
              }
            });
            payZone.append(
              el("div", { class: "alert alert-ok", "data-testid": "order-pay-info" },
                `Instruksi ${pay.nomor_pembayaran}: Total transfer tepat ${formatRp(pay.nominal_total)} (pokok ${formatRp(pay.nominal_pokok)} + kode unik ${pay.kode_unik}).`
              ),
              upMsg,
              el("div", { class: "row" }, inFile, btnUp)
            );
          } catch (e) {
            payZone.appendChild(el("div", { class: "alert alert-error" }, (e as Error).message));
          } finally {
            btnBayar.disabled = false;
          }
        });
        actionBar.append(btnBayar);

        const btnBatal = el("button", { class: "btn btn-danger", type: "button", "data-testid": "btn-batal-pesanan" }, "Batalkan Pesanan");
        btnBatal.addEventListener("click", async () => {
          try {
            await api.request("PATCH", `/api/v1/pesanan/${encodeURIComponent(ord.id)}/status`, {
              body: { status: "batal", catatan: "Dibatalkan melalui portal" }
            });
            await loadOrders();
            await openDetail(ord.id);
          } catch (e) {
            feedback.appendChild(el("div", { class: "alert alert-error" }, (e as Error).message));
          }
        });
        actionBar.append(btnBatal, payZone);
      }

      if (["admin", "petugas_toko"].includes(user.peran)) {
        for (const nextSt of ["diproses", "siap_diambil", "diantar", "selesai"]) {
          if (ord.status === "dibayar" || ord.status === "diproses" || ord.status === "diantar" || ord.status === "siap_diambil") {
            const b = el("button", { class: "btn", type: "button", "data-testid": `btn-status-${nextSt}` }, `Set ${nextSt}`);
            b.addEventListener("click", async () => {
              try {
                await api.request("PATCH", `/api/v1/pesanan/${encodeURIComponent(ord.id)}/status`, { body: { status: nextSt } });
                await loadOrders();
                await openDetail(ord.id);
              } catch (e) {
                feedback.appendChild(el("div", { class: "alert alert-error" }, (e as Error).message));
              }
            });
            actionBar.append(b);
          }
        }
      }

      detailWrap.append(
        el("h3", { "data-testid": "detail-pesanan-judul" }, `06 — Pesanan ${ord.nomor_pesanan} `, statusBadge(ord.status)),
        stepper,
        el("div", { class: "table-wrap" }, tbl),
        el("div", { class: "row", style: "margin-top:8px;" },
          el("span", null, `Metode: ${ord.metode_pengiriman}`),
          el("span", null, `Penerima: ${ord.nama_penerima}`),
          el("span", null, `Total: ${formatRp(ord.total_bayar)}`),
          el("span", null, "Gudang/gerai: ", blockedBadge("1 lokasi tunggal"))
        ),
        actionBar
      );
    };

    try {
      const res = await api.request<PesananRow>("GET", `/api/v1/pesanan?id=${encodeURIComponent(id)}`);
      drawDetail(res.data);
    } catch (e) {
      clear(detailWrap);
      detailWrap.appendChild(el("div", { class: "alert alert-error" }, (e as Error).message));
    }
  };

  const loadOrders = async (): Promise<void> => {
    try {
      const res = await api.request<PesananRow[]>("GET", "/api/v1/pesanan");
      const rows = res.data || [];
      clear(listWrap);
      if (rows.length === 0) {
        listWrap.appendChild(el("div", { class: "alert alert-note" }, "Belum ada pesanan."));
        return;
      }
      const tbl = el("table", null,
        el("thead", null, el("tr", null, el("th", null, "No. Pesanan"), el("th", null, "Penerima"), el("th", null, "Total"), el("th", null, "Status"), el("th", null, "Aksi"))),
        el("tbody", null,
          ...rows.map((r) => {
            const btn = el("button", { class: "btn", type: "button", "data-testid": `detail-ord-${r.nomor_pesanan}` }, "Detail");
            btn.addEventListener("click", () => { void openDetail(r.id); });
            return el("tr", null,
              el("td", null, r.nomor_pesanan),
              el("td", null, r.nama_penerima),
              el("td", null, formatRp(r.total_bayar)),
              el("td", null, statusBadge(r.status)),
              el("td", null, btn)
            );
          })
        )
      );
      listWrap.appendChild(el("div", { class: "table-wrap" }, tbl));
    } catch (err) {
      clear(listWrap);
      listWrap.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  };

  container.append(el("h2", null, "06 — Pesanan Saya & Pengantaran"), feedback, listWrap, detailWrap);
  await loadOrders();
}
