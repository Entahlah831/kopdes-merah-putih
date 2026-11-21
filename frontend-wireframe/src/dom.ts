export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs?: Record<string, string | boolean | undefined> | null,
  ...children: Array<Node | string | number | null | undefined>
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (v === undefined || v === false) continue;
      if (v === true) {
        node.setAttribute(k, "");
      } else {
        node.setAttribute(k, v);
      }
    }
  }
  for (const child of children) {
    if (child === null || child === undefined) continue;
    if (typeof child === "string" || typeof child === "number") {
      node.appendChild(document.createTextNode(String(child)));
    } else {
      node.appendChild(child);
    }
  }
  return node;
}

export function clear(node: HTMLElement): void {
  while (node.firstChild) {
    node.removeChild(node.firstChild);
  }
}

export function formatRp(val: number | undefined | null): string {
  const n = typeof val === "number" && Number.isFinite(val) ? val : 0;
  return "Rp " + Math.round(n).toLocaleString("id-ID");
}

export function blockedBadge(label = "BLOCKED: butuh keputusan pemilik"): HTMLSpanElement {
  return el("span", { class: "badge badge-blocked" }, label);
}

export function statusBadge(status: string): HTMLSpanElement {
  const s = (status || "").toLowerCase();
  if (["aktif", "disetujui", "lunas", "selesai", "tersedia", "dikirim", "diambil"].includes(s)) {
    return el("span", { class: "badge badge-ok" }, status);
  }
  if (["ditolak", "batal", "kadaluarsa", "menunggak", "nonaktif"].includes(s)) {
    return el("span", { class: "badge badge-danger" }, status);
  }
  return el("span", { class: "badge badge-warn" }, status || "-");
}

const TERMINAL_STATES = new Set([
  "lunas",
  "ditolak",
  "batal",
  "kadaluarsa",
  "selesai",
  "perlu_tindak_lanjut"
]);

export function isTerminalStatus(status: string): boolean {
  return TERMINAL_STATES.has((status || "").toLowerCase());
}
