# 17. Contoh Integrasi Lengkap: React, Vue 3 & Vanilla JS

Ketiga contoh lengkap di bawah menggunakan SDK resmi [frontend-reference/](../../frontend-reference/) yang bebas dependensi runtime dan mengikuti seluruh aturan kontrak API Kopdes Merah Putih. File sumber siap jalannya tersedia di:
- React: [react-example.tsx](../../frontend-reference/src/examples/react-example.tsx)
- Vue 3: [vue-example.ts](../../frontend-reference/src/examples/vue-example.ts)
- Vanilla JS: [vanilla-example.js](../../frontend-reference/src/examples/vanilla-example.js)

---

## 1. Contoh React (Hook `useKopdesAuth` + Komponen `QrisPaymentCard`)

```tsx
import { useState, useEffect, useCallback } from "react";
import {
  createClient,
  fetchPublicConfig,
  formatRupiah,
  createPaymentPoller,
  getErrorPresentation,
  ApiError,
  type PublicConfig,
} from "../../../frontend-reference/src/index";

export function useKopdesClient(baseUrl: string) {
  const [client, setClient] = useState<ReturnType<typeof createClient> | null>(null);
  const [config, setConfig] = useState<PublicConfig | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    fetchPublicConfig(baseUrl, { signal: ac.signal }).then((cfg) => {
      setConfig(cfg);
      setClient(createClient({ baseUrl, publicConfig: cfg }));
    });
    return () => ac.abort();
  }, [baseUrl]);

  return { client, config };
}

export function PembayaranBanner({
  config,
  error,
}: {
  config: PublicConfig | null;
  error: unknown;
}) {
  if (config?.features.read_only_mode) {
    return <div role="alert">Sistem sedang dalam mode baca-saja (pemeliharaan).</div>;
  }
  if (error instanceof ApiError) {
    const info = getErrorPresentation(error);
    return (
      <div role="alert">
        <strong>{info.title}</strong>: {error.message || info.action}
      </div>
    );
  }
  return null;
}
```

> Kode lengkap `useAuth`, `QrisPaymentPanel`, serta `PendaftaranKycPanel` dapat disalin langsung dari [react-example.tsx](../../frontend-reference/src/examples/react-example.tsx).

## 2. Contoh Vue 3 (Composable `useKopdesClient` & Pembayaran QRIS)

```typescript
import { ref, onUnmounted } from "vue";
import { createClient, fetchPublicConfig, formatRupiah, type PublicConfig } from "../../../frontend-reference/src/index";

export function useKopdesVue(baseUrl: string) {
  const config = ref<PublicConfig | null>(null);
  const loading = ref(false);
  const errorMsg = ref("");
  const controller = new AbortController();

  async function init() {
    loading.value = true;
    try {
      const cfg = await fetchPublicConfig(baseUrl, { signal: controller.signal });
      config.value = cfg;
      return createClient({ baseUrl, publicConfig: cfg });
    } catch (e: any) {
      errorMsg.value = e?.message || "Gagal memuat konfigurasi";
      return null;
    } finally {
      loading.value = false;
    }
  }

  onUnmounted(() => controller.abort());
  return { config, loading, errorMsg, init, formatRupiah };
}
```

> Implementasi composable penuh beserta daftar tagihan iuran & polling tersedia di [vue-example.ts](../../frontend-reference/src/examples/vue-example.ts).

## 3. Contoh Vanilla JavaScript (Tanpa Framework / Build Step)

```javascript
export async function bootVanillaKopdes(baseUrl, containerEl) {
  const cfgRes = await fetch(`${baseUrl}/api/v1/public/config`);
  const cfgJson = await cfgRes.json();
  if (!cfgRes.ok || !cfgJson.success) {
    containerEl.textContent = "Layanan belum tersedia.";
    return;
  }
  const cfg = cfgJson.data;
  if (cfg.features.read_only_mode) {
    containerEl.textContent = "Sistem dalam mode baca-saja sementara.";
    return;
  }
  containerEl.textContent = `Siap terhubung ke API v${cfg.api_version} (${cfg.timezone})`;
}
```

> Implementasi lengkap tanpa framework (login, daftar KYC, checkout & QRIS + poller `visibilitychange`) ada di [vanilla-example.js](../../frontend-reference/src/examples/vanilla-example.js) dan dijalankan nyata pada [frontend-tester/reference/](../../frontend-tester/reference/index.html)!
