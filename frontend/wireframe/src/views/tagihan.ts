import { APIClient, startTerminalPoll } from "../api";
import { blockedBadge, clear, el, formatRp, statusBadge } from "../dom";
import { validateAndCompressImage } from "../image";
import { UserSession } from "../types";

interface TagihanItem {
  id: string;
  jenis_kode: string;
  jenis_nama: string;
  periode_tahun: number;
  periode_bulan: number;
  nominal: number;
  nominal_keringanan: number;
  nominal_netto: number;
  tanggal_jatuh_tempo: string;
  status: string;
}

export async function renderTagihan(
  container: HTMLElement,
  api: APIClient,
  user: UserSession | null,
  registerCleanup: (fn: () => void) => void
): Promise<void> {
  if (!user) {
    container.appendChild(el("div", { class: "alert alert-error" }, "Silakan login terlebih dahulu untuk membuka portal tagihan."));
    return;
  }

  let stopPoll: (() => void) | null = null;
  registerCleanup(() => {
    if (stopPoll) stopPoll();
  });

  const feedback = el("div");
  const kpiWrap = el("div", { class: "grid-cards" });
  const listWrap = el("div", { class: "card" }, "Memuat tagihan iuran...");
  const payPanel = el("div", { class: "card card-muted" });
  const selectedIDs = new Set<string>();
  let rows: TagihanItem[] = [];

  const renderPayPanel = (): void => {
    clear(payPanel);
    const chosen = rows.filter((r) => selectedIDs.has(r.id));
    const totalNominal = chosen.reduce((s, r) => s + r.nominal_netto, 0);
    const qrisData = api.getPublic()?.koperasi?.active_qris;
    const btnCreate = el(
      "button",
      { class: "btn btn-primary", type: "button", disabled: chosen.length === 0, "data-testid": "btn-buat-bayar-iuran" },
      "Buat Instruksi QRIS"
    );
    const uploadWrap = el("div");

    btnCreate.addEventListener("click", async () => {
      feedback.textContent = "";
      btnCreate.disabled = true;
      try {
        const res = await api.request<{ pembayaran: { id: string; nomor_pembayaran: string; nominal_pokok: number; kode_unik: number; nominal_total: number; status: string } }>("POST", "/api/v1/pembayaran", {
          headers: { "Idempotency-Key": api.newIdempotencyKey("payiur") },
          body: { jenis_transaksi: "iuran", metode: "qris", tagihan_ids: Array.from(selectedIDs) }
        });
        const pay = res.data.pembayaran;
        clear(uploadWrap);
        const fInput = el("input", { type: "file", accept: "image/jpeg,image/png", "data-testid": "iuran-bukti-file" });
        const bSend = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-kirim-bukti-iuran" }, "Kirim Bukti Pembayaran (JPG/PNG)");
        const pollInfo = el("div", { class: "alert alert-note" }, "Catatan: Bukti diunggah != lunas. Menunggu verifikasi bendahara.");

        bSend.addEventListener("click", async () => {
          feedback.textContent = "";
          const f = fInput.files?.[0];
          if (!f) {
            feedback.appendChild(el("div", { class: "alert alert-error" }, "Pilih berkas JPG/PNG bukti bayar."));
            return;
          }
          bSend.disabled = true;
          try {
            const img = await validateAndCompressImage(f);
            const fd = new FormData();
            fd.append("pembayaran_id", pay.id);
            fd.append("file", img.blob, img.filename);
            await api.request("POST", "/api/v1/pembayaran", { formData: fd });
            pollInfo.textContent = "Bukti terenkripsi berhasil diunggah. Memantau status hingga terminal...";
            stopPoll = startTerminalPoll(
              async (sig) => (await api.request<{ pembayaran: { status: string } }>("GET", `/api/v1/pembayaran?id=${encodeURIComponent(pay.id)}`, { signal: sig })).data.pembayaran,
              (p) => p.status,
              (p, stopped) => {
                pollInfo.textContent = `Status ${pay.nomor_pembayaran}: ${p.status}${stopped ? " (selesai memantau)" : ""}`;
              }
            );
          } catch (err) {
            feedback.appendChild(el("div", { class: "alert alert-error", "data-testid": "iuran-upload-error" }, (err as Error).message));
          } finally {
            bSend.disabled = false;
          }
        });

        uploadWrap.append(
          el("div", { class: "alert alert-ok" }, `No. ${pay.nomor_pembayaran} — Transfer tepat ${formatRp(pay.nominal_total)} (pokok ${formatRp(pay.nominal_pokok)} + unik ${pay.kode_unik}).`),
          pollInfo,
          el("div", { class: "field" }, el("label", null, "Unggah Bukti JPG/PNG (PDF ditolak)"), fInput),
          bSend
        );
      } catch (err) {
        feedback.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
      } finally {
        btnCreate.disabled = false;
      }
    });

    payPanel.append(
      el("h3", null, "Bayar Tagihan Terpilih"),
      el("div", { class: "row", style: "margin-bottom:8px;" },
        el("span", { class: "badge" }, `Dipilih: ${chosen.length} tagihan`),
        el("strong", null, `Total: ${formatRp(totalNominal)}`)
      ),
      el("div", { class: "row", style: "margin-bottom:8px;" },
        el("button", { class: "btn btn-primary", type: "button" }, "QRIS Statis"),
        el("button", { class: "btn", type: "button", disabled: true }, "Transfer Bank ", blockedBadge("BLOCKED"))
      ),
      el("div", { class: "placeholder-box", style: "min-height:90px;margin-bottom:8px;" },
        qrisData?.nama_merchant ? `QRIS ${qrisData.nama_merchant} (${qrisData.nmid || "-"})` : "QRIS Koperasi Statis"
      ),
      btnCreate,
      uploadWrap
    );
  };

  container.append(el("h2", null, "04a — Tagihan & Pembayaran Iuran"), feedback, kpiWrap, el("div", { class: "grid-2" }, listWrap, payPanel));

  try {
    const scope = user.anggota_id ? "saya" : "tunggakan";
    const res = await api.request<TagihanItem[]>("GET", `/api/v1/iuran/tagihan?scope=${scope}`);
    rows = res.data || [];

    const unpaid = rows.filter((r) => r.status === "belum_bayar");
    const totalUnpaid = unpaid.reduce((s, r) => s + r.nominal_netto, 0);
    kpiWrap.append(
      el("div", { class: "card card-muted" }, el("div", null, "Total belum dibayar"), el("strong", null, formatRp(totalUnpaid))),
      el("div", { class: "card card-muted" }, el("div", null, "Tagihan aktif"), el("strong", null, `${unpaid.length} tagihan`)),
      el("div", { class: "card card-muted" }, el("div", null, "Jatuh tempo terdekat"), el("strong", null, unpaid[0]?.tanggal_jatuh_tempo || "-"))
    );

    clear(listWrap);
    if (rows.length === 0) {
      listWrap.appendChild(el("div", { class: "alert alert-note" }, "Tidak ada tagihan iuran saat ini."));
      renderPayPanel();
      return;
    }

    const tbl = el("table", null,
      el("thead", null, el("tr", null, el("th", null, "Pilih"), el("th", null, "Tagihan"), el("th", null, "Jatuh Tempo"), el("th", null, "Jumlah"), el("th", null, "Status & Langkah Berikutnya"))),
      el("tbody", null,
        ...rows.map((r) => {
          const chk = el("input", { type: "checkbox", style: "width:auto;", disabled: r.status !== "belum_bayar", "data-testid": `chk-tagihan-${r.id}` });
          chk.addEventListener("change", () => {
            if (chk.checked) selectedIDs.add(r.id);
            else selectedIDs.delete(r.id);
            renderPayPanel();
          });
          const hint = r.status === "belum_bayar" ? "Pilih lalu bayar via QRIS" : r.status === "lunas" ? "Sudah lunas" : "Periksa status pembayaran aktif";
          return el("tr", null,
            el("td", null, chk),
            el("td", null, `${r.jenis_nama} — ${r.periode_tahun}-${String(r.periode_bulan).padStart(2, "0")}`),
            el("td", null, r.tanggal_jatuh_tempo),
            el("td", null, formatRp(r.nominal_netto)),
            el("td", null, statusBadge(r.status), el("div", { style: "font-size:11px;color:var(--subtle);" }, hint))
          );
        })
      )
    );
    listWrap.appendChild(el("div", { class: "table-wrap" }, tbl));
    renderPayPanel();
  } catch (err) {
    clear(listWrap);
    listWrap.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
  }
}
