// Shared test fixture. Import { test, expect } from here in every spec (not
// directly from @playwright/test) so all specs get the same setup.
//
// For now it resets the stub backend before each test. Reset is a no-op until
// Stage 1 adds per-test scenarios, but establishing the pattern now means specs
// don't change when scenarios land.

import { test as base, expect } from "@playwright/test";

const STUB_URL = "http://127.0.0.1:8899";

export const test = base.extend({
  page: async ({ page, request }, use) => {
    await request.post(`${STUB_URL}/__mock/reset`).catch(() => {
      // Stub not reachable (e.g. reused external server) — non-fatal for Stage 0.
    });
    // Bypass the first-run onboarding wizard (AppInitializer redirects to it when
    // this key is unset). Runs before any page script, on every navigation.
    await page.addInitScript(() => {
      window.localStorage.setItem("kagent-onboarding", "true");
    });
    await use(page);
  },
});

export { expect };
