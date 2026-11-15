import { ref, computed, onUnmounted } from "vue";
import {
  createClient,
  fetchPublicConfig,
  formatRupiah,
  getErrorPresentation,
  ApiError,
  type PublicConfig,
  type AuthSessionUser,
} from "../index.js";

export function useKopdesVueSession(baseUrl: string) {
  const config = ref<PublicConfig | null>(null);
  const user = ref<AuthSessionUser | null>(null);
  const errorText = ref("");
  const loading = ref(false);
  const controller = new AbortController();

  const isReadOnly = computed(() => Boolean(config.value?.features.read_only_mode));
  let client: ReturnType<typeof createClient> | null = null;

  async function init() {
    loading.value = true;
    errorText.value = "";
    try {
      const cfg = await fetchPublicConfig(baseUrl, { signal: controller.signal });
      config.value = cfg;
      client = createClient({
        baseUrl,
        publicConfig: cfg,
        onForceLogout: () => {
          user.value = null;
        },
        onConfigChange: (updated) => {
          config.value = updated;
        },
      });
      return client;
    } catch (err) {
      errorText.value =
        err instanceof ApiError ? getErrorPresentation(err).message : "Gagal terhubung ke server.";
      return null;
    } finally {
      loading.value = false;
    }
  }

  async function loginMember(email: string, password: string) {
    if (!client) await init();
    if (!client) return null;
    loading.value = true;
    errorText.value = "";
    try {
      const outcome = await client.auth.login(email, password, controller.signal);
      if (outcome.status === "authenticated") {
        user.value = outcome.session;
      }
      return outcome;
    } catch (err) {
      if (err instanceof ApiError) {
        errorText.value = getErrorPresentation(err).message;
      }
      return null;
    } finally {
      loading.value = false;
    }
  }

  async function loadMyBills() {
    if (!client) return [];
    const bills = await client.request<Array<{ id: string; periode: string; nominal_tagihan: number; status: string }>>(
      "/api/v1/iuran/tagihan/saya",
      { signal: controller.signal }
    );
    return bills.map((b) => ({
      ...b,
      nominalLabel: formatRupiah(b.nominal_tagihan),
    }));
  }

  onUnmounted(() => {
    controller.abort();
    client?.auth.destroy();
  });

  return {
    config,
    user,
    errorText,
    loading,
    isReadOnly,
    init,
    loginMember,
    loadMyBills,
    formatRupiah,
  };
}
