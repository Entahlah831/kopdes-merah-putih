import "./styles.css";
import { APIClient } from "./api";
import { clear, el } from "./dom";
import { CartEntry, RouteKey, UserSession } from "./types";
import { renderBeranda } from "./views/beranda";
import { renderDaftar, renderMasuk } from "./views/auth_daftar";
import { renderKatalog, renderPesanan } from "./views/katalog_pesanan";
import { renderTagihan } from "./views/tagihan";
import {
  renderDashboardPengurus,
  renderVerifikasiAnggota,
  renderVerifikasiPembayaran,
} from "./views/admin";
import { renderOperasional, renderPengaturanUser } from "./views/operasional";

const api = new APIClient();
let currentUser: UserSession | null = null;
let currentRoute: RouteKey = "beranda";
const cart: CartEntry[] = [];
let activeCleanups: Array<() => void> = [];

function registerCleanup(fn: () => void): void {
  activeCleanups.push(fn);
}

function runCleanups(): void {
  for (const fn of activeCleanups) {
    try {
      fn();
    } catch {
      /* ignore */
    }
  }
  activeCleanups = [];
}

function routeFromHash(): RouteKey {
  const h = window.location.hash.replace(/^#\/?/, "");
  const valid: RouteKey[] = [
    "beranda",
    "katalog",
    "daftar",
    "masuk",
    "tagihan",
    "pesanan",
    "dashboard",
    "verifikasi_anggota",
    "verifikasi_pembayaran",
    "operasional",
    "pengaturan_user",
  ];
  return (valid.includes(h as RouteKey) ? h : "beranda") as RouteKey;
}

function navigate(route: RouteKey): void {
  const targetHash = `#/${route}`;
  currentRoute = route;
  if (window.location.hash === targetHash) {
    void renderApp();
    return;
  }
  window.location.hash = targetHash;
}

async function renderApp(): Promise<void> {
  runCleanups();
  const root = document.getElementById("app");
  if (!root) return;
  clear(root);

  const pub = api.getPublic();
  const cartQty = cart.reduce((s, c) => s + c.kuantitas, 0);

  const navItems: Array<{ key: RouteKey; label: string; show?: boolean }> = [
    { key: "beranda", label: "Beranda", show: true },
    { key: "katalog", label: `Katalog (${cartQty})`, show: true },
    { key: "pesanan", label: "Pesanan Saya", show: !!currentUser },
    { key: "tagihan", label: "Tagihan", show: !!currentUser },
    {
      key: "dashboard",
      label: "Pengurus",
      show:
        !!currentUser &&
        ["admin", "bendahara", "petugas_toko"].includes(currentUser.peran),
    },
    {
      key: "verifikasi_anggota",
      label: "Verif Anggota",
      show:
        !!currentUser &&
        ["admin", "bendahara", "petugas_toko"].includes(currentUser.peran),
    },
    {
      key: "verifikasi_pembayaran",
      label: "Verif Bayar",
      show: !!currentUser && ["admin", "bendahara"].includes(currentUser.peran),
    },
    {
      key: "operasional",
      label: "Operasional",
      show:
        !!currentUser &&
        ["admin", "bendahara", "petugas_toko"].includes(currentUser.peran),
    },
    {
      key: "pengaturan_user",
      label: "Pengaturan",
      show: !!currentUser && currentUser.peran === "admin",
    },
  ];

  const navButtons = navItems
    .filter((i) => i.show)
    .map((i) => {
      const b = el(
        "button",
        {
          class: `nav-btn ${currentRoute === i.key ? "active" : ""}`,
          type: "button",
          "data-testid": `nav-${i.key}`,
        },
        i.label,
      );
      b.addEventListener("click", () => navigate(i.key));
      return b;
    });

  const authBar = el("div", { class: "row" });
  if (currentUser) {
    const btnAkun = el(
      "button",
      { class: "btn", type: "button", "data-testid": "btn-akun-mfa" },
      currentUser.mfa_enabled ? "Pengaturan Akun" : "Pengaturan Akun (Atur MFA)",
    );
    btnAkun.addEventListener("click", () => navigate("pengaturan_user"));
    const btnLogout = el(
      "button",
      { class: "btn", type: "button", "data-testid": "btn-logout" },
      "Keluar",
    );
    btnLogout.addEventListener("click", async () => {
      try {
        await api.request("POST", "/api/v1/auth/logout");
      } finally {
        currentUser = null;
        navigate("beranda");
      }
    });
    authBar.append(
      el(
        "span",
        { class: "badge", "data-testid": "user-badge" },
        `${currentUser.nama} (${currentUser.peran})`,
      ),
      btnAkun,
      btnLogout,
    );
  } else {
    const bLogin = el(
      "button",
      { class: "btn", type: "button", "data-testid": "nav-masuk" },
      "Masuk",
    );
    const bReg = el(
      "button",
      { class: "btn btn-primary", type: "button", "data-testid": "nav-daftar" },
      "Daftar Anggota",
    );
    bLogin.addEventListener("click", () => navigate("masuk"));
    bReg.addEventListener("click", () => navigate("daftar"));
    authBar.append(bLogin, bReg);
  }

const header = el("header", { class: "topbar" },
  el("div", { class: "brand" },
    el("img", {
      class: "brand-logo",
      src: "/logo-kopdes.png",
      alt: "Logo Kopdes Merah Putih"
    }),
    el("span", null, pub?.koperasi?.nama_koperasi || "Kopdes Merah Putih")
  ),
  el("nav", { class: "nav-links" }, ...navButtons),
  authBar
);

  const main = el("main", { class: "shell" });
  root.append(header, main);

  switch (currentRoute) {
    case "beranda":
      await renderBeranda(main, api, navigate);
      break;
    case "katalog":
      await renderKatalog(
        main,
        api,
        currentUser,
        cart,
        () => {
          const kBtn = header.querySelector('[data-testid="nav-katalog"]');
          if (kBtn)
            kBtn.textContent = `Katalog (${cart.reduce((s, c) => s + c.kuantitas, 0)})`;
        },
        navigate,
      );
      break;
    case "daftar":
      renderDaftar(main, api, navigate);
      break;
    case "masuk":
      renderMasuk(main, api, (u) => {
        currentUser = u;
        if (["admin", "bendahara", "petugas_toko"].includes(u.peran))
          navigate("dashboard");
        else navigate("tagihan");
      });
      break;
    case "tagihan":
      await renderTagihan(main, api, currentUser, registerCleanup);
      break;
    case "pesanan":
      await renderPesanan(main, api, currentUser, registerCleanup);
      break;
    case "dashboard":
      await renderDashboardPengurus(main, api, currentUser, navigate);
      break;
    case "verifikasi_anggota":
      await renderVerifikasiAnggota(main, api, currentUser, registerCleanup);
      break;
    case "verifikasi_pembayaran":
      await renderVerifikasiPembayaran(main, api, currentUser, registerCleanup);
      break;
    case "operasional":
      await renderOperasional(main, api, currentUser);
      break;
    case "pengaturan_user":
      await renderPengaturanUser(main, api, currentUser);
      break;
  }
}

async function bootstrap(): Promise<void> {
  const root = document.getElementById("app");
  try {
    await api.init();
    try {
      const me = await api.request<UserSession>("GET", "/api/v1/auth/me");
      currentUser = me.data && me.data.scope === "full" ? me.data : null;
    } catch {
      currentUser = null;
    }
    currentRoute = routeFromHash();
    window.addEventListener("hashchange", () => {
      currentRoute = routeFromHash();
      void renderApp();
    });
    await renderApp();
  } catch (err) {
    if (root) {
      clear(root);
      root.appendChild(
        el(
          "div",
          { class: "shell" },
          el(
            "div",
            { class: "alert alert-error" },
            `Gagal inisialisasi UI: ${(err as Error).message}`,
          ),
        ),
      );
    }
  }
}

void bootstrap();
