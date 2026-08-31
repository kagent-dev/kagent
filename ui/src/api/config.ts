/**
 * The one place that knows whether the app is talking to a real backend.
 *
 * Everything above this file — the client, the hooks, the pages — issues the
 * same requests against the same paths in both modes. In mock mode the base URL
 * points at the address the in-browser mock backend intercepts; in live mode it
 * points at whatever the deployment configured. Which of the two is running is
 * decided here and nowhere else — see `resolveApiMode` for the order of
 * precedence.
 */

import { env, envFlag } from "@/env";

export type ApiMode = "mock" | "live";

/**
 * The origin the mock backend answers on.
 *
 * It is a real, absolute URL rather than a relative path so that mock traffic is
 * unmistakable in the network panel, and so a stray request in live mode cannot
 * accidentally be served by a leftover service worker.
 */
export const MOCK_API_BASE_URL = "http://localhost:8083/api";

/**
 * Resolved once at module load: the mode cannot change without a reload.
 *
 * Two sources, most specific first.
 *
 * `VITE_API_MODE` is a build-time pin and always wins — the e2e suite sets it so
 * a run cannot be aimed at a real cluster by whatever the shell exported.
 *
 * Failing that, `ENABLE_MOCK_UI` decides, and **fixtures are never the default**.
 * Unset means the real API, in a dev server exactly as in a built image. The dev
 * server used to default to mocks, and the cost of that convenience was paid the
 * wrong way round: a backend that was down, misconfigured or unreachable rendered
 * as a healthy app full of plausible data, and the way you found out was noticing
 * that a name you did not recognise kept appearing. Asking for fixtures is cheap
 * — one setting, or `?mock=ok` — and being given them unasked is not.
 */
export const apiMode: ApiMode = resolveApiMode();

function resolveApiMode(): ApiMode {
  if (import.meta.env.VITE_API_MODE === "live") return "live";
  if (import.meta.env.VITE_API_MODE === "mock") return "mock";

  const wantsMock = envFlag("ENABLE_MOCK_UI");

  // The mock backend is a service worker that the release image deliberately
  // does not ship, so a built bundle cannot honour this however it is set.
  // Entering mock mode anyway would point every request at an address nothing
  // is listening on — a wholly broken app, from a setting that looked supported.
  if (wantsMock && !import.meta.env.DEV) {
    console.warn(
      "ENABLE_MOCK_UI is set, but this build ships no mock backend. Using the real API.",
    );
    return "live";
  }

  return wantsMock ? "mock" : "live";
}

export const isMockMode = apiMode === "mock";

/**
 * Base URL every request is built on, with any trailing slash removed.
 *
 * Read from the runtime environment rather than from `import.meta.env`, so the
 * address of the backend is a deployment decision rather than something frozen
 * into the image at build time. This is a module-level constant, which is why the
 * environment has to arrive synchronously — see `@/env`.
 */
export const apiBaseUrl: string = stripTrailingSlash(
  apiMode === "live" ? env("API_BASE_URL") : MOCK_API_BASE_URL,
);

/** How long a request may run before the client aborts it. */
export const REQUEST_TIMEOUT_MS = 30_000;

function stripTrailingSlash(url: string): string {
  return url.endsWith("/") ? url.slice(0, -1) : url;
}
