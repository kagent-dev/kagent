// Single source of truth for where the REAL kagent backend lives during E2E.
//
// The proxy (mocks/server.mjs, configured in playwright.config.ts) targets
// KAGENT_BACKEND_URL, and setup.ts port-forwards the controller onto that URL's
// port. Both read this module so the two can never drift apart — setting the env
// var alone stays consistent across the proxy target and the port-forward.

// In-cluster controller port; also the local port the port-forward defaults to.
export const CONTROLLER_PORT = 8083;

// Origin of the real backend the proxy forwards to. The proxy mocks only the
// chat A2A stream; every other /api call hits this backend.
export const KAGENT_BACKEND_URL =
  process.env.KAGENT_BACKEND_URL ?? `http://127.0.0.1:${CONTROLLER_PORT}`;

// Local port the port-forward must open, derived from KAGENT_BACKEND_URL so it
// always matches the proxy target. Falls back to CONTROLLER_PORT for a URL with
// no explicit port.
export const LOCAL_PORT = Number(new URL(KAGENT_BACKEND_URL).port || CONTROLLER_PORT);
