// Shared test fixture. Import { test, expect } from here in every spec (not
// directly from @playwright/test) so all specs get the same setup: the first-run
// onboarding wizard is bypassed on every navigation. The onboarding spec opts
// back in by setting the flag to "false" in its own init script (init scripts run
// in registration order, so the spec's wins).

import { test as base, expect } from "@playwright/test";

export const test = base.extend({
  page: async ({ page }, run) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("kagent-onboarding", "true");
    });
    await run(page);
  },
});

export { expect };
