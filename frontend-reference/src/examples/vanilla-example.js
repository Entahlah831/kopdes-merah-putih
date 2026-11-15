import {
  createClient,
  fetchPublicConfig,
  createPaymentPoller,
  formatRupiah,
  getErrorPresentation,
  ApiError,
} from "../index.js";

/**
 * Contoh Vanilla JS tanpa framework maupun build step, siap dipakai di browser modern.
 */
export async function initKopdesVanillaApp({
  baseUrl,
  statusEl,
  bannerEl,
}) {
  const config = await fetchPublicConfig(baseUrl, { cache: "no-store" });
  if (config.features.read_only_mode && bannerEl) {
    bannerEl.textContent = "Sistem sedang mode baca-saja sementara.";
  }

  const client = createClient({
    baseUrl,
    publicConfig: config,
    onForceLogout: () => {
      if (statusEl) statusEl.textContent = "Sesi berakhir, silakan masuk kembali.";
    },
  });

  async function loginAndLoadProfile(email, password) {
    try {
      const out = await client.auth.login(email, password);
      if (out.status !== "authenticated") {
        return out;
      }
      const profile = await client.request("/api/v1/auth/me");
      if (statusEl) {
        statusEl.textContent = `Masuk sebagai ${profile.nama_lengkap} (${profile.role})`;
      }
      return { status: "authenticated", profile };
    } catch (err) {
      if (err instanceof ApiError && bannerEl) {
        bannerEl.textContent = getErrorPresentation(err).message;
      }
      throw err;
    }
  }

  function startPaymentPolling(pembayaranId, onStatusChange) {
    const poller = createPaymentPoller({
      intervalMs: config.polling_interval_ms,
      fetchStatus: (signal) => client.request(`/api/v1/pembayaran/${pembayaranId}`, { signal }),
      onUpdate: (data) => {
        const label = `${data.status} - ${formatRupiah(data.nominal_total)}`;
        onStatusChange(label, data);
      },
    });
    poller.start();
    return poller;
  }

  return { config, client, loginAndLoadProfile, startPaymentPolling };
}
