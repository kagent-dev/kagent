import { defineConfig, devices } from "@playwright/test";

/**
 * Overridable so concurrent runs can each own a port — but fixed, and
 * deliberately not derived from the process: Playwright loads this file in the
 * main process and again in every worker, so anything varying per process gives
 * each worker a different base URL from the one the servers were started on.
 */
const PORT = Number(process.env.UI_LOOP_PORT ?? 8001);
const BASE_URL = `http://localhost:${PORT}`;

/**
 * A second app, booted with the example vendor extension installed.
 *
 * Which extension a build ships with is decided at build time
 * (`src/vendorExtensions/activeConfig.ts` reads an env var), so "installed" and
 * "not installed" cannot be two states of one server — they are two servers.
 * That split is also what lets the default suite assert the app is bare, which
 * is the shape a build with no extension takes.
 */
// Deliberately not PORT + 1: Vite falls forward to the next free port when the
// one it is told to use is busy, so adjacent ports let a slow-to-die server from
// a previous run push one app onto the other's port.
const VENDOR_PORT = Number(process.env.UI_LOOP_VENDOR_PORT ?? PORT + 50);
const VENDOR_BASE_URL = `http://localhost:${VENDOR_PORT}`;

/** Specs that need the extension installed opt in by filename. */
const VENDOR_SPECS = /\.vendor\.spec\.ts$/;

/**
 * The suite is the acceptance bar, so what it runs against cannot depend on the
 * shell it was started from: both servers are pinned to the in-browser mock
 * backend. An inherited VITE_API_MODE=live would otherwise point a whole run at
 * a real cluster.
 */
const MOCK_BACKEND = { VITE_API_MODE: "mock" };

/**
 * What each of the three servers is pinned to.
 *
 * Named here, rather than written inline below, so that what a server serves is
 * stated in one place — and so a branch that installs an extension changes a
 * value instead of restructuring the `projects`/`webServer` blocks.
 *
 * `VITE_VENDOR_EXTENSIONS` is pinned on the bare server for the same reason
 * `VITE_API_MODE` is: an inherited value must not be able to decide what a run
 * measures. Left unpinned, the bare project measures whatever the shell happened
 * to export.
 */
const BARE_APP = { ...MOCK_BACKEND, VITE_VENDOR_EXTENSIONS: "none" };
const EXAMPLE_APP = { ...MOCK_BACKEND, VITE_VENDOR_EXTENSIONS: "example" };

/**
 * The third mode: one app, wired to a real backend.
 *
 * Selected by an environment variable rather than added as a third project
 * alongside the mock two, because the two modes have incompatible requirements
 * and each would break the other:
 *
 * - A live run needs a cluster and a port-forward. Adding it to the default
 *   project list would make `yarn test:pw` — which is meant to need nothing but a
 *   machine that can run the dev server — fail on any laptop without a cluster in
 *   front of it.
 * - A live run has no use for the two mock servers, and starting them would cost
 *   every live run the time to boot two more Vite instances.
 *
 * So `LIVE` swaps the whole `projects`/`webServer` pair rather than appending to
 * it. `yarn test:pw` and `yarn test:pw:live` are two disjoint runs.
 */
const LIVE = process.env.UI_LOOP_LIVE === "true";

/**
 * Its own port, far from the mock servers' 8001/8051, for the same reason those
 * two are 50 apart: Vite falls forward to the next free port when the one it is
 * told to use is busy, so a live run must not be able to land on a port a mock
 * server is about to want, or vice versa.
 */
const LIVE_PORT = Number(process.env.UI_LOOP_LIVE_PORT ?? 8301);
const LIVE_BASE_URL = `http://localhost:${LIVE_PORT}`;

/** Read by `playwright/globalSetup.ts` to decide what to verify about a server. */
export const LIVE_PROJECT = "chromium-live";

/**
 * A live run reaches the backend through Vite's proxy, exactly as a deployed
 * build reaches it through nginx — so the app uses the same relative URLs either
 * way and this mode tests the addressing a real deployment uses.
 *
 * `VITE_API_MODE` is pinned as well as the runtime flag: the build-time pin is
 * the one thing an inherited `.env` cannot override, and a live suite that
 * silently answered from fixtures would be worse than a red one.
 */
const LIVE_APP = { VITE_API_MODE: "live", ENABLE_MOCK_UI: "false" };

/**
 * How the live server is started.
 *
 * Named for the same reason the three env pins above are: a branch whose backend
 * needs more than a dev server — a credential minted per run, a port-forward
 * probed before Vite starts — replaces this line rather than the block below.
 */
const LIVE_COMMAND = `yarn dev --port ${LIVE_PORT}`;

export default defineConfig({
  testDir: "./playwright/tests",
  // Both servers have to be rendering, not merely listening, before any test
  // navigates — see the file for what goes wrong otherwise.
  globalSetup: "./playwright/globalSetup.ts",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  /*
   * Parallel in CI too, at a share of the runner rather than a count.
   *
   * It was one worker, and that was buying real stability: at high concurrency this
   * suite produced roughly one failure per two full runs, always a different test.
   * Two of those were the suite's own fault and are fixed — see `helpers/style` for
   * the press-state reads that compared unpainted frames. The rest were starvation
   * rather than logic: a mock-backed click that normally settles in under a second
   * timing out at fifteen, because two dev servers and two browser engines on one
   * machine leave the servers transforming modules for whoever asks first.
   *
   * A share, not a number, because that failure mode is about how much machine there
   * is: `ubuntu-latest` is four cores, where a flat `4` would thrash with two engines
   * running, and a runner that grows should get the benefit without another edit.
   *
   * `retries` stays at two deliberately. Raising concurrency without keeping the net
   * would trade a slow suite for a suite that fails on other people's pull requests,
   * and the starvation above is not fixed — only made less likely by asking for less
   * of the machine than the local runs that provoked it.
   */
  workers: process.env.CI ? "50%" : undefined,
  reporter: process.env.CI ? "github" : "list",
  /*
   * A real backend behind a port-forward answers in tens of seconds where the
   * in-browser mock answers in milliseconds, so the defaults that suit the mock
   * suite are too tight to distinguish "slow cluster" from "broken page".
   *
   * The mock suite gets sixty rather than the default thirty, which is about the
   * machine and not about the app. Several of these tests are journeys — six steps,
   * a page load in most of them — and thirty seconds is a comfortable budget on an
   * idle laptop and a tight one on a runner sharing itself with another engine. The
   * failure it produced said `Test timeout of 30000ms exceeded` partway through a
   * step that had done nothing wrong, which is a report about contention dressed as
   * a defect.
   *
   * `expect.timeout` is deliberately left at its default. That is the one that keeps
   * a genuinely missing element fast to find: an assertion still gives up in five
   * seconds, so a real failure does not sit here spending the larger budget.
   */
  ...(LIVE
    ? { timeout: 120_000, expect: { timeout: 30_000 } }
    : { timeout: 60_000 }),
  use: {
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },
  projects: LIVE
    ? [
        {
          name: LIVE_PROJECT,
          testDir: "./playwright/live",
          use: {
            ...devices["Desktop Chrome"],
            baseURL: LIVE_BASE_URL,
            // Worth keeping for a live failure: unlike the mock suite there is
            // no fixed fixture to re-read, so the trace is the only record of what
            // the cluster actually answered.
            trace: "retain-on-failure",
          },
        },
      ]
    : [
        {
          name: "chromium",
          testIgnore: VENDOR_SPECS,
          use: { ...devices["Desktop Chrome"], baseURL: BASE_URL },
        },
        {
          // The same suite in a second engine, against the same server.
          //
          // Not redundancy: the two disagree about things this app depends on —
          // flex and grid sizing, scroll metrics, focus and selection, and how
          // streamed responses are delivered. A chat that pins to the bottom and
          // a rail that stays put are exactly the kind of thing one engine gets
          // right by accident.
          //
          // The vendor split below is a build-time difference, not a browser one,
          // so it stays on one engine rather than doubling for no new signal.
          name: "firefox",
          testIgnore: VENDOR_SPECS,
          use: { ...devices["Desktop Firefox"], baseURL: BASE_URL },
        },
        {
          name: "chromium-vendor",
          testMatch: VENDOR_SPECS,
          use: { ...devices["Desktop Chrome"], baseURL: VENDOR_BASE_URL },
        },
      ],
  // Never adopt a server this config did not start. Adopting one skips the `env`
  // below, so a dev server left over from an earlier run — or one a developer has
  // open — silently serves a build with the wrong extension config, and the
  // vendor specs then fail looking for contributions that were never installed.
  // That was an intermittent failure whose frequency depended only on whether
  // something happened to linger. Refusing to adopt makes an occupied port a
  // loud startup error instead; set UI_LOOP_PORT / UI_LOOP_VENDOR_PORT to run
  // alongside a dev server you want to keep.
  webServer: LIVE
    ? [
        {
          command: LIVE_COMMAND,
          url: LIVE_BASE_URL,
          reuseExistingServer: false,
          timeout: 120_000,
          // Whatever starts the live server is the most useful output a failed
          // live run has — something that cannot reach the backend says so there,
          // and Playwright discards a web server's stdout unless asked to pass it
          // through.
          stdout: "pipe",
          stderr: "pipe",
          env: LIVE_APP,
        },
      ]
    : [
        {
          command: `yarn dev --port ${PORT}`,
          url: BASE_URL,
          reuseExistingServer: false,
          timeout: 120_000,
          env: BARE_APP,
        },
        {
          command: `yarn dev --port ${VENDOR_PORT}`,
          url: VENDOR_BASE_URL,
          reuseExistingServer: false,
          timeout: 120_000,
          env: EXAMPLE_APP,
        },
      ],
});
