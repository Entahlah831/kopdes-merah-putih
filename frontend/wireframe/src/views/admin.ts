import { APIClient } from "../api";
import { blockedBadge, clear, el, formatRp, statusBadge } from "../dom";
import { RouteKey, UserSession } from "../types";

export async function renderDashboardPengurus(
  container: HTMLElement,
  api: APIClient,
  user: UserSession | null,
  navigate: (r: RouteKey) => void
): Promise<void> {
  if (!user || !["admin", "bendahara", "petugas_toko"].includes(user.peran)) {
    container.appendChild(el("div", { class: "alert alert-error", "data-testid": "forbidden-view" }, "Akses terbatas untuk pengurus / bendahara / petugas."));
    return;
  }

  const sidebar = el("aside", { class: "sidebar" },
    el("div", { class: "sidebar-label" }, "UTAMA"),
    el("button", { class: "btn btn-primary", type: "button" }, "Ringkasan"),
    el("div", { class: "sidebar-label" }, "OPERASIONAL AKTIF"),
    el("button", { class: "btn", type: "button", id: "sb-ver-ang" }, "03a Verifikasi Anggota"),
    el("button", { class: "btn", type: "button", id: "sb-ver-pay" }, "04b Verifikasi Pembayaran"),
    el("button", { class: "btn", type: "button", id: "sb-pesanan" }, "06 Pesanan & Antaran"),
    el("div", { class: "sidebar-label" }, "KELEMBAGAAN / AKUNTANSI"),
    el("div", { class: "alert alert-note" }, "SIMKOPDES, RAT, Pengurus/Pengawas, Kas & Mutasi: ", blockedBadge("BLOCKED: butuh keputusan pemilik"))
  );
  sidebar.querySelector("#sb-ver-ang")?.addEventListener("click", () => navigate("verifikasi_anggota"));
  sidebar.querySelector("#sb-ver-pay")?.addEventListener("click", () => navigate("verifikasi_pembayaran"));
  sidebar.querySelector("#sb-pesanan")?.addEventListener("click", () => navigate("pesanan"));

  const main = el("div");
  const kpis = el("div", { class: "grid-cards" }, el("div", { class: "card" }, "Memuat ringkasan laporan..."));
  const info = el("div", { class: "card" },
    el("h3", null, "Aktivitas & Audit Trail"),
    el("p", { class: "alert alert-note" }, "Audit log dicatat di server pada setiap persetujuan/penolakan. Endpoint baca audit langsung belum diekspos pada OpenAPI ramping.")
  );
  main.append(el("h2", null, "02 — Dashboard Pengurus"), kpis, info);
  container.appendChild(el("div", { class: "shell-admin" }, sidebar, main));

  try {
    const [angLap, tungLap, pendRes, payRes] = await Promise.all([
      api.request<Array<{ status: string; jumlah: number }>>("GET", "/api/v1/laporan?jenis=anggota"),
      api.request<unknown[]>("GET", "/api/v1/laporan?jenis=tunggakan"),
      api.request<unknown[]>("GET", "/api/v1/pendaftaran?status=diajukan"),
      api.request<unknown[]>("GET", "/api/v1/pembayaran?status=menunggu_verifikasi")
    ]);
    clear(kpis);
    const aktif = (angLap.data || []).find((a) => a.status === "aktif")?.jumlah || 0;
    kpis.append(
      el("div", { class: "card card-muted" }, el("div", null, "Anggota Aktif"), el("strong", null, String(aktif))),
      el("div", { class: "card card-muted" }, el("div", null, "Pendaftaran Menunggu"), el("strong", null, String((pendRes.data || []).length))),
      el("div", { class: "card card-muted" }, el("div", null, "Pembayaran Menunggu Verifikasi"), el("strong", null, String((payRes.data || []).length))),
      el("div", { class: "card card-muted" }, el("div", null, "Rekap Tunggakan"), el("strong", null, String((tungLap.data || []).length))),
      el("div", { class: "card card-muted" }, el("div", null, "Sinkronisasi SIMKOPDES"), blockedBadge("BLOCKED: butuh keputusan pemilik"))
    );
  } catch (err) {
    clear(kpis);
    kpis.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
  }
}

export async function renderVerifikasiAnggota(
  container: HTMLElement,
  api: APIClient,
  user: UserSession | null,
  registerCleanup: (fn: () => void) => void
): Promise<void> {
  if (!user || !["admin", "bendahara", "petugas_toko"].includes(user.peran)) {
    container.appendChild(el("div", { class: "alert alert-error", "data-testid": "forbidden-view" }, "Hanya admin/staf verifikator yang berhak meninjau pendaftaran."));
    return;
  }

  const blobURLs: string[] = [];
  registerCleanup(() => {
    for (const u of blobURLs) URL.revokeObjectURL(u);
  });

  const feedback = el("div");
  const filterStatus = el("select", { "data-testid": "filter-reg-status" },
    el("option", { value: "" }, "Semua Status"),
    el("option", { value: "diajukan" }, "Diajukan"),
    el("option", { value: "perlu_perbaikan" }, "Perlu perbaikan"),
    el("option", { value: "disetujui" }, "Disetujui"),
    el("option", { value: "ditolak" }, "Ditolak")
  );
  const searchBox = el("input", { type: "text", placeholder: "Cari nama pemohon...", "data-testid": "search-reg" });
  const tblWrap = el("div", { class: "card" }, "Memuat antrean pendaftaran...");
  const detailWrap = el("div", { class: "card", style: "display:none;" });

  interface PendaftaranRow {
    id: string;
    nama_lengkap: string;
    nik_masked: string;
    hp_masked: string;
    pekerjaan: string;
    status: string;
    catatan_verifikasi?: string;
    dokumen_tersedia?: string[];
  }

  let allRows: PendaftaranRow[] = [];

  const openDetail = async (row: PendaftaranRow): Promise<void> => {
    detailWrap.style.display = "block";
    clear(detailWrap);
    const docsBox = el("div", { class: "grid-cards" }, el("div", { class: "alert alert-note" }, "Mendekripsi preview dokumen KYC..."));
    const alasanInput = el("textarea", { rows: "2", placeholder: "Catatan / alasan (wajib untuk Perbaikan / Tolak)", "data-testid": "reg-catatan" });
    const btnApprove = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-reg-setuju" }, "Setujui");
    const btnRev = el("button", { class: "btn", type: "button", "data-testid": "btn-reg-perbaikan" }, "Minta Perbaikan");
    const btnReject = el("button", { class: "btn btn-danger", type: "button", "data-testid": "btn-reg-tolak" }, "Tolak");
    const resBox = el("div");

    const decide = async (keputusan: "disetujui" | "perlu_perbaikan" | "ditolak"): Promise<void> => {
      resBox.textContent = "";
      try {
        const res = await api.request<{ activation_code?: string; status?: string }>("POST", `/api/v1/pendaftaran/${encodeURIComponent(row.id)}/verifikasi`, {
          body: { keputusan, catatan: alasanInput.value.trim() }
        });
        const act = res.data?.activation_code ? ` | Kode Aktivasi: ${res.data.activation_code}` : "";
        resBox.appendChild(el("div", { class: "alert alert-ok", "data-testid": "reg-decision-ok" }, `Keputusan ${keputusan} tersimpan${act}`));
        if (res.data?.activation_code) {
          resBox.appendChild(el("div", { class: "badge", "data-testid": "reg-activation-code" }, res.data.activation_code));
        }
        await loadQueue();
      } catch (e) {
        resBox.appendChild(el("div", { class: "alert alert-error" }, (e as Error).message));
      }
    };

    btnApprove.addEventListener("click", () => { void decide("disetujui"); });
    btnRev.addEventListener("click", () => { void decide("perlu_perbaikan"); });
    btnReject.addEventListener("click", () => { void decide("ditolak"); });

    detailWrap.append(
      el("h3", null, `03b — Detail Pengajuan ${row.nama_lengkap} `, statusBadge(row.status)),
      el("div", { class: "row", style: "margin-bottom:8px;" },
        el("span", null, `NIK Masked: ${row.nik_masked}`),
        el("span", null, `HP Masked: ${row.hp_masked}`),
        el("span", null, `Pekerjaan: ${row.pekerjaan}`)
      ),
      docsBox,
      el("div", { class: "field", style: "margin-top:8px;" }, el("label", null, "Surat pernyataan (wireframe)"), blockedBadge("BLOCKED: butuh keputusan pemilik")),
      resBox,
      el("div", { class: "field", style: "margin-top:8px;" }, el("label", null, "Catatan / Alasan"), alasanInput),
      el("div", { class: "row" }, btnApprove, btnRev, btnReject)
    );

    clear(docsBox);
    const available = row.dokumen_tersedia || [];
    for (const t of ["ktp", "selfie_ktp", "kk"]) {
      if (!available.includes(t)) {
        docsBox.appendChild(el("div", { class: "card card-muted" }, el("strong", null, t), el("div", null, "Belum diunggah")));
        continue;
      }
      try {
        const blob = await api.fetchBlob(`/api/v1/pendaftaran/${encodeURIComponent(row.id)}/dokumen/${t}`);
        blobURLs.push(blob.url);
        docsBox.appendChild(el("div", { class: "card card-muted" }, el("strong", null, t), el("img", { src: blob.url, alt: t, class: "preview-img" })));
      } catch (e) {
        docsBox.appendChild(el("div", { class: "card card-muted" }, el("strong", null, t), el("div", { class: "alert alert-error" }, (e as Error).message)));
      }
    }
  };

  const renderTable = (): void => {
    clear(tblWrap);
    const q = searchBox.value.trim().toLowerCase();
    const list = allRows.filter((r) => !q || r.nama_lengkap.toLowerCase().includes(q));
    if (list.length === 0) {
      tblWrap.appendChild(el("div", { class: "alert alert-note" }, "Belum ada pendaftaran baru sesuai filter."));
      return;
    }
    const tbl = el("table", null,
      el("thead", null, el("tr", null, el("th", null, "Nama Pemohon"), el("th", null, "NIK"), el("th", null, "Dokumen"), el("th", null, "Status"), el("th", null, "Aksi"))),
      el("tbody", null,
        ...list.map((r) => {
          const btn = el("button", { class: "btn btn-primary", type: "button", "data-testid": `tinjau-${r.id}` }, "Tinjau");
          btn.addEventListener("click", () => { void openDetail(r); });
          return el("tr", null,
            el("td", null, r.nama_lengkap),
            el("td", null, r.nik_masked),
            el("td", null, `${(r.dokumen_tersedia || []).length}/3`),
            el("td", null, statusBadge(r.status)),
            el("td", null, btn)
          );
        })
      )
    );
    tblWrap.appendChild(el("div", { class: "table-wrap" }, tbl));
  };

  const loadQueue = async (): Promise<void> => {
    try {
      const qs = filterStatus.value ? `?status=${encodeURIComponent(filterStatus.value)}` : "";
      const res = await api.request<PendaftaranRow[]>("GET", `/api/v1/pendaftaran${qs}`);
      allRows = res.data || [];
      renderTable();
    } catch (e) {
      clear(tblWrap);
      tblWrap.appendChild(el("div", { class: "alert alert-error" }, (e as Error).message));
    }
  };

  filterStatus.addEventListener("change", () => { void loadQueue(); });
  searchBox.addEventListener("input", renderTable);

  container.append(
    el("h2", null, "03a/03b — Verifikasi Anggota & KYC"),
    feedback,
    el("div", { class: "card row" },
      el("div", { style: "flex:1;min-width:180px;" }, searchBox),
      el("div", { style: "width:180px;" }, filterStatus)
    ),
    tblWrap,
    detailWrap
  );
  await loadQueue();
}

export async function renderVerifikasiPembayaran(
  container: HTMLElement,
  api: APIClient,
  user: UserSession | null,
  registerCleanup: (fn: () => void) => void
): Promise<void> {
  if (!user || !["admin", "bendahara"].includes(user.peran)) {
    container.appendChild(el("div", { class: "alert alert-error", "data-testid": "forbidden-view" }, "Hanya admin atau bendahara yang berhak memverifikasi pembayaran."));
    return;
  }

  const blobURLs: string[] = [];
  registerCleanup(() => {
    for (const u of blobURLs) URL.revokeObjectURL(u);
  });

  const feedback = el("div");
  const tblWrap = el("div", { class: "card" }, "Memuat antrean pembayaran...");
  const detailWrap = el("div", { class: "card", style: "display:none;" });

  interface PayRow {
    id: string;
    nomor_pembayaran: string;
    jenis_transaksi: string;
    metode: string;
    nominal_pokok: number;
    kode_unik: number;
    nominal_total: number;
    status: string;
    bukti_url?: string;
  }

  const openPayment = async (p: PayRow): Promise<void> => {
    detailWrap.style.display = "block";
    clear(detailWrap);
    const imgBox = el("div", { class: "placeholder-box", style: "min-height:120px;" }, "Memuat bukti terenkripsi...");
    const inNominal = el("input", { type: "number", value: String(p.nominal_total), "data-testid": "ver-pay-nominal" });
    const inAlasan = el("textarea", { rows: "2", placeholder: "Catatan verifikasi / alasan penolakan", "data-testid": "ver-pay-alasan" });
    const chkBreak = el("input", { type: "checkbox", style: "width:auto;" });
    const inMFA = el("input", { type: "text", placeholder: "MFA TOTP (khusus break-glass)" });
    const resMsg = el("div");
    const btnSetuju = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-pay-setuju" }, "Verifikasi & Alokasikan");
    const btnTolak = el("button", { class: "btn btn-danger", type: "button", "data-testid": "btn-pay-tolak" }, "Tolak Bukti");

    const submitDecision = async (keputusan: "setuju" | "tolak"): Promise<void> => {
      resMsg.textContent = "";
      try {
        await api.request("POST", `/api/v1/pembayaran/${encodeURIComponent(p.id)}/verifikasi`, {
          body: {
            keputusan,
            nominal_dibayar: Number(inNominal.value || 0),
            alasan: inAlasan.value.trim(),
            breakglass: chkBreak.checked,
            mfa_code: inMFA.value.trim()
          }
        });
        resMsg.appendChild(el("div", { class: "alert alert-ok", "data-testid": "ver-pay-ok" }, `Pembayaran ${p.nomor_pembayaran} diputuskan: ${keputusan}.`));
        await loadList();
      } catch (e) {
        resMsg.appendChild(el("div", { class: "alert alert-error" }, (e as Error).message));
      }
    };

    btnSetuju.addEventListener("click", () => { void submitDecision("setuju"); });
    btnTolak.addEventListener("click", () => { void submitDecision("tolak"); });

    detailWrap.append(
      el("h3", null, `04b — Verifikasi ${p.nomor_pembayaran} `, statusBadge(p.status)),
      el("div", { class: "alert alert-note" }, "Kas & Mutasi, Rekonsiliasi Mutasi Bank CSV: ", blockedBadge("BLOCKED: butuh keputusan pemilik")),
      imgBox,
      el("div", { class: "row", style: "margin:8px 0;" },
        el("span", null, `Metode: ${p.metode}`),
        el("span", null, `Pokok: ${formatRp(p.nominal_pokok)}`),
        el("span", null, `Kode Unik: ${p.kode_unik}`),
        el("strong", null, `Total Harus Dibayar: ${formatRp(p.nominal_total)}`)
      ),
      resMsg,
      el("div", { class: "field" }, el("label", null, "Nominal Dibayar Aktual"), inNominal),
      el("div", { class: "field" }, el("label", null, "Alasan / Catatan"), inAlasan),
      el("div", { class: "field" }, el("label", { class: "row" }, chkBreak, "Break-glass Darurat Admin (KEPUTUSAN PEMILIK)"), inMFA),
      el("div", { class: "row" }, btnSetuju, btnTolak)
    );

    if (p.bukti_url) {
      try {
        const blob = await api.fetchBlob(`/api/v1/pembayaran?bukti_id=${encodeURIComponent(p.id)}`);
        blobURLs.push(blob.url);
        clear(imgBox);
        imgBox.appendChild(el("img", { src: blob.url, alt: "Bukti Pembayaran", class: "preview-img" }));
      } catch (e) {
        imgBox.textContent = (e as Error).message;
      }
    } else {
      imgBox.textContent = "Bukti gambar belum diunggah.";
    }
  };

  const loadList = async (): Promise<void> => {
    try {
      const res = await api.request<PayRow[]>("GET", "/api/v1/pembayaran");
      const list = res.data || [];
      clear(tblWrap);
      if (list.length === 0) {
        tblWrap.appendChild(el("div", { class: "alert alert-note" }, "Belum ada transaksi pembayaran."));
        return;
      }
      const tbl = el("table", null,
        el("thead", null, el("tr", null, el("th", null, "Nomor"), el("th", null, "Jenis"), el("th", null, "Nominal Total"), el("th", null, "Status"), el("th", null, "Aksi"))),
        el("tbody", null,
          ...list.map((p) => {
            const btn = el("button", { class: "btn btn-primary", type: "button", "data-testid": `ver-pay-${p.nomor_pembayaran}` }, "Periksa");
            btn.addEventListener("click", () => { void openPayment(p); });
            return el("tr", null,
              el("td", null, p.nomor_pembayaran),
              el("td", null, p.jenis_transaksi),
              el("td", null, formatRp(p.nominal_total)),
              el("td", null, statusBadge(p.status)),
              el("td", null, btn)
            );
          })
        )
      );
      tblWrap.appendChild(el("div", { class: "table-wrap" }, tbl));
    } catch (e) {
      clear(tblWrap);
      tblWrap.appendChild(el("div", { class: "alert alert-error" }, (e as Error).message));
    }
  };

  container.append(el("h2", null, "04b — Verifikasi Pembayaran (Bendahara/Admin)"), feedback, tblWrap, detailWrap);
  await loadList();
}
