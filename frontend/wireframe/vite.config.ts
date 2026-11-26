import { defineConfig } from "vite";

const rawProxyTarget = (process.env.VITE_API_PROXY_TARGET || "").trim();
const proxyTarget = /^https?:\/\//i.test(rawProxyTarget) ? rawProxyTarget : "";

export default defineConfig({
  base: "/",
  ...(proxyTarget
    ? {
        server: {
          proxy: {
            "/api": {
              target: proxyTarget,
              changeOrigin: true
            },
            "/healthz": {
              target: proxyTarget,
              changeOrigin: true
            },
            "/readyz": {
              target: proxyTarget,
              changeOrigin: true
            }
          }
        }
      }
    : {}),
  build: {
    outDir: "dist",
    emptyOutDir: true
  }
});
