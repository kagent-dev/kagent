// Shared test fixture. Import { test, expect } from here in every spec (not
// directly from @playwright/test) so all specs get the same setup:
//   - `mock`: the stub-backend control seam (mock.noAgents(), mock.agentsError(), …)
//   - the stub is reset before every test (page depends on mock), so scenarios
//     set by one test never leak into the next.
//   - onboarding wizard is bypassed on every navigation.

import { test as base, expect } from "@playwright/test";
import { makeMock, type MockBackend } from "../mocks/control";

export const test = base.extend<{ mock: MockBackend }>({
  // Param is named `run` (not the Playwright-idiomatic `use`) to avoid the
  // react-hooks lint rule mistaking it for React 19's use() hook.
  mock: async ({ request }, run) => {
    const mock = makeMock(request);
    // Reset scenario state before each test. The stub is managed by Playwright's
    // webServer, so in CI an unreachable stub is a real failure — fail fast.
    try {
      await mock.reset();
    } catch (err) {
      if (process.env.CI) throw err;
    }
    await run(mock);
  },
  // Depend on `mock` so the reset above runs before every test that uses a page,
  // even specs that don't touch scenarios (e.g. smoke).
  page: async ({ page, mock }, run) => {
    void mock;
    // Bypass the first-run onboarding wizard (AppInitializer redirects to it when
    // this key is unset). Runs before any page script, on every navigation.
    await page.addInitScript(() => {
      window.localStorage.setItem("kagent-onboarding", "true");
    });
    await run(page);
  },
});

export { expect };
