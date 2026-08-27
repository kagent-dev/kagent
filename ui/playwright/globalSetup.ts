import { chromium, type FullConfig, type Page } from "@playwright/test";
import { LIVE_PROJECT } from "../playwright.config";

/**
 * Waits until each server actually renders the app, not merely answers on its
 * port.
 *
 * Playwright's `webServer.url` check passes as soon as the dev server responds,
 * but Vite pre-bundles dependencies on the *first real page load* and forces a
 * full reload when it finishes. A test navigating into that window has the DOM
 * pulled out from under it mid-assertion, which showed up as the first run after
 * a cold start failing and every run after it passing — the worst kind of flake,
 * because it looks like a broken feature rather than a broken harness.
 *
 * Loading each app once here moves that reload before any test exists.
 */
export default async function globalSetup(config: FullConfig): Promise<void> {
  const browser = await chromium.launch();
  try {
    for (const project of config.projects) {
      const baseUrl = project.use.baseURL;
      if (!baseUrl) continue;

      const page = await browser.newPage();
      try {
        await page.goto(baseUrl, { waitUntil: "load" });
        // The shell rendering is the signal that the module graph is served and
        // any optimisation reload has already happened.
        await page.waitForSelector('[data-testid="app-content"]', {
          timeout: 120_000,
        });

        // The live project runs alone on its own port, so it cannot be the
        // victim of the port swap checked for below, and the vendor check does not
        // apply to it at all. What it has instead is a failure the mock projects
        // cannot have: coming up against fixtures and passing every assertion
        // without touching a backend.
        if (project.name === LIVE_PROJECT) {
          await verifyLiveWiring(page, baseUrl);
          continue;
        }

        // Which build a port is serving is decided when its server starts, and
        // two dev servers coming up together have been seen to end up the wrong
        // way round. Checked here so that failure reads as what it is, at the
        // start, instead of surfacing later as a spec that cannot find the
        // contribution it was asserting on.
        const slots = await page.locator('[data-testid^="vendor-slot-"]').count();
        const wantsVendor = project.name.includes("vendor");

        if (wantsVendor && slots === 0) {
          throw new Error(
            `${baseUrl} was expected to serve the app with the example extension ` +
              `installed (project "${project.name}"), but no extension points are ` +
              `mounted. The server on that port came up without ` +
              `VITE_VENDOR_EXTENSIONS=example.`,
          );
        }
        if (!wantsVendor && slots > 0) {
          throw new Error(
            `${baseUrl} was expected to serve the app with no extension installed ` +
              `(project "${project.name}"), but ${slots} extension points are ` +
              `mounted. The two dev servers have come up on each other's ports.`,
          );
        }
      } finally {
        await page.close();
      }
    }
  } finally {
    await browser.close();
  }
}

/**
 * Fails a live run that would otherwise pass without reaching the backend.
 *
 * A page-load suite is unusually easy to satisfy dishonestly. These pages render
 * — heading, toolbar, empty table — whether the data came from a cluster, from
 * in-browser fixtures, or from nothing at all. So every way a live run can be a
 * lie ends in green, and a green one is taken as evidence the cluster works.
 * These are harness faults, so they are caught here, before any test exists, and
 * said plainly: a red run is recoverable, a green one that measured nothing is
 * not.
 *
 * Checked by asking the page what it was told rather than by reading `.env` here:
 * what matters is the configuration the browser received, which is the only thing
 * the app acts on. An installed extension has its own settings to check, and adds
 * those checks after the ones below.
 */
async function verifyLiveWiring(page: Page, baseUrl: string): Promise<void> {
  // The same object the app itself reads settings from — written into the
  // document by the dev server (`vite.config.ts`) the way the container renders
  // it at startup, so this reads exactly what the page was configured with.
  const settings = await page.evaluate(
    () =>
      (window as unknown as { environmentVariables?: Record<string, string> })
        .environmentVariables ?? {},
  );

  if ((settings.ENABLE_MOCK_UI ?? "").toLowerCase().includes("true")) {
    throw new Error(
      `${baseUrl} is serving the in-browser mock backend (ENABLE_MOCK_UI=` +
        `${settings.ENABLE_MOCK_UI}), so a live run would pass without calling the ` +
        `controller at all. Unset it in ui/.env, or run the mock suite instead.`,
    );
  }

  // A second way a live run can be a lie: the flag can be off while a worker from
  // an earlier mock run is still installed and answering, which looks exactly like
  // a working backend.
  const workerCount = await page.evaluate(async () => {
    if (!("serviceWorker" in navigator)) return 0;
    const registrations = await navigator.serviceWorker.getRegistrations();
    return registrations.length;
  });

  if (workerCount > 0) {
    throw new Error(
      `${baseUrl} has ${workerCount} service worker(s) registered. The mock backend ` +
        `is a service worker, so a live run cannot be trusted while one is installed.`,
    );
  }
}
