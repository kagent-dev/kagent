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
  // Parallelism stays off until Stage 1 per-test data isolation lands: one
  // shared stub backend + one Next server means concurrent tests would race
  // against shared state (see README). Flip both `fullyParallel` and `workers`
  // together when isolation is in place.
  fullyParallel: false,
  forbidOnly: CI,
  retries: CI ? 1 : 0,
  workers: 1,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: APP_URL,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    video: "retain-on-failure",
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
      // Pin the port so a shell-exported STUB_PORT can't make the stub bind
      // somewhere other than the health-check / BACKEND_INTERNAL_URL address.
      env: { STUB_PORT: String(STUB_PORT) },
    },
    {
      // Force the webpack dev server (the default `npm run dev` uses Turbopack).
      // Turbopack's dev server corrupts the RSC client manifest when a route is
      // recompiled after a full-page navigation cycle (`evalManifest` throws
      // "Invalid or unexpected token"), which surfaces as a Runtime Error overlay
      // on the *second* cold load of "/" in a session and breaks every test that
      // re-navigates there (onboarding variants, the app-shell error state). The
      // bug is dev-only and Turbopack-specific, so we opt this run out of it while
      // leaving local `npm run dev` on Turbopack for day-to-day speed.
      command: "npm run dev -- --webpack",
      url: APP_URL,
      // Never reuse an existing dev server: the BACKEND_INTERNAL_URL below is
      // only applied to a server Playwright starts. A reused server (e.g. a
      // hand-started `npm run dev`) would silently bypass the stub. Always
      // boot our own so the redirect is guaranteed; a busy port fails loudly.
      reuseExistingServer: false,
      timeout: 120_000,
      env: {
        // Redirect the server-side backend fetch to our stub.
        BACKEND_INTERNAL_URL: `${STUB_URL}/api`,
      },
    },
  ],
});
