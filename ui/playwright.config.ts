import { defineConfig, devices } from "@playwright/test";

/**
 * Playwright E2E config for the kagent UI.
 *
 * Data is fetched server-side (Next.js server actions), so browser-level
 * `page.route` cannot intercept `/api/**`. Instead we boot a standalone stub
 * backend (playwright/mocks/server.mjs) and point Next at it via
 * BACKEND_INTERNAL_URL — `getBackendUrl()` (src/lib/utils.ts) checks that env
 * var first. Both servers are started by the `webServer` block below.
 *
 * See playwright/README.md for the full test strategy.
 */

const CI = !!process.env.CI;

const STUB_PORT = 8899;
const STUB_URL = `http://127.0.0.1:${STUB_PORT}`;
const APP_URL = "http://localhost:8001";

export default defineConfig({
  testDir: "./playwright/tests",
  outputDir: "./playwright/test-results",
  fullyParallel: true,
  forbidOnly: CI,
  retries: CI ? 1 : 0,
  // Single worker for now: one shared stub backend + one Next server. Per-test
  // data isolation is a Stage 1 concern (see README).
  workers: CI ? 1 : undefined,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: APP_URL,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    video: "off",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      command: "node playwright/mocks/server.mjs",
      url: `${STUB_URL}/__mock/health`,
      reuseExistingServer: !CI,
      timeout: 30_000,
      stdout: "pipe",
      stderr: "pipe",
    },
    {
      command: "npm run dev",
      url: APP_URL,
      reuseExistingServer: !CI,
      timeout: 120_000,
      env: {
        // Redirect the server-side backend fetch to our stub.
        BACKEND_INTERNAL_URL: `${STUB_URL}/api`,
      },
    },
  ],
});
