import { defineConfig, devices } from "@playwright/test";
import { KAGENT_BACKEND_URL } from "./playwright/backend";

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
// KAGENT_BACKEND_URL — origin of the REAL kagent backend the proxy forwards to —
// is defined in playwright/backend.ts alongside the port-forward config in
// playwright/setup.ts, so the proxy target and the port-forward stay in sync.

// `slowMo` adds an idle delay between every Playwright action (click, fill,
// goto). The recorded videos play at real time, so without slowMo the test
// runs fast enough that a human can't follow what's happening. 250ms feels
// natural in the recording without bloating wall-clock test time too much.
// Coerce + validate the env override so a malformed value (non-numeric → NaN,
// or negative) falls back to the default instead of reaching Playwright.
const DEFAULT_SLOW_MO_MS = 250;
const parsedSlowMo = Number(process.env.E2E_SLOW_MO_MS);
const SLOW_MO_MS =
  Number.isFinite(parsedSlowMo) && parsedSlowMo >= 0
    ? parsedSlowMo
    : DEFAULT_SLOW_MO_MS;

export default defineConfig({
  testDir: "./playwright/tests",
  outputDir: "./playwright/test-results",
  // Port-forward the real controller before the run, tear it down after.
  globalSetup: "./playwright/setup.ts",
  globalTeardown: "./playwright/teardown.ts",
  // Parallelism stays off until Stage 1 per-test data isolation lands: one
  // shared stub backend + one Next server means concurrent tests would race
  // against shared state (see README). Flip both `fullyParallel` and `workers`
  // together when isolation is in place.
  fullyParallel: false,
  forbidOnly: CI,
  retries: CI ? 1 : 0,
  workers: 1,
  // Real-backend flows do create/list/delete round trips, so allow headroom.
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: APP_URL,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    video: "on",
    launchOptions: {
      slowMo: SLOW_MO_MS,
    },
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
      // Pin the proxy port (health-check / BACKEND_INTERNAL_URL address) and tell
      // it where the real backend is (the port-forward from playwright/setup.ts).
      env: { STUB_PORT: String(STUB_PORT), KAGENT_BACKEND_URL },
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
        // Route the UI's server-side backend fetches (and the /a2a route handler)
        // through the proxy, which forwards to the real backend and mocks chat.
        BACKEND_INTERNAL_URL: `${STUB_URL}/api`,
      },
    },
  ],
});
