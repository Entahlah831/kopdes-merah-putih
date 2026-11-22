import { defineConfig } from "@playwright/test";

const baseURL = process.env.UI_BASE_URL;
if (!baseURL) {
  throw new Error("UI_BASE_URL environment variable wajib disetel (mis. dimuat dari .env.test.alt UI_PORT)");
}

export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  retries: 0,
  workers: 1,
  use: {
    baseURL,
    headless: true,
    viewport: { width: 1280, height: 800 },
    screenshot: "only-on-failure"
  }
});
