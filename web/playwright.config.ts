import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  testMatch: "*.spec.ts",
  workers: 1,
  timeout: 60000,
  use: { viewport: { width: 1360, height: 900 }, headless: true, channel: process.env.CODEXM_BROWSER_CHANNEL },
});
