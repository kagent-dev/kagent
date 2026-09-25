/**
 * Starts the in-browser mock backend.
 *
 * Two halves, because the app speaks two protocols. The application API is gRPC,
 * and its fixtures are installed as the transport every operation calls through —
 * so nothing above the data layer behaves differently and no request is
 * intercepted at all. `/oauth2/userinfo` is still HTTP, so a service worker
 * answers that one.
 *
 * Order matters: the transport is installed before anything can call an
 * operation, which is why this is awaited in `main.tsx` before the app renders.
 * Only `main.tsx` calls it, and only when `VITE_API_MODE` is not `"live"` — a page
 * that quietly served fixtures because the backend was unreachable would look
 * healthy while showing data that was never real.
 */

import { MOCK_API_BASE_URL } from "@/api/config";
import { setApiTransport } from "@/api/transport";
import { mockTransport } from "./transport";
import {
  AUTH_SCENARIOS,
  AUTH_SCENARIO_PARAM,
  CHAT_SCENARIOS,
  CHAT_SCENARIO_PARAM,
  MOCK_SCENARIOS,
  MOCK_SCENARIO_PARAM,
  currentScenario,
} from "./scenario";

export async function startMockBackend(): Promise<void> {
  setApiTransport(mockTransport);

  const { worker } = await import("./browser");

  await worker.start({
    // Only the proxy endpoint is ours to answer now, so Vite's module and HMR
    // requests pass through untouched. A request to the API base *is* worth
    // complaining about: the API is not served over HTTP any more, so anything
    // still addressing it went around the operations layer — which is a bug, and a
    // silent one if it is bypassed without a word.
    onUnhandledRequest: (request, print) => {
      if (request.url.startsWith(MOCK_API_BASE_URL)) print.warning();
    },
    serviceWorker: { url: "/mockServiceWorker.js" },
    quiet: true,
  });

  console.info(
    `[mock backend] serving the gRPC API from fixtures — scenario "${currentScenario()}". ` +
      `Append ?${MOCK_SCENARIO_PARAM}=<${MOCK_SCENARIOS.join("|")}>, ` +
      `?${CHAT_SCENARIO_PARAM}=<${CHAT_SCENARIOS.join("|")}> or ` +
      `?${AUTH_SCENARIO_PARAM}=<${AUTH_SCENARIOS.join("|")}> to any route to change it.`,
  );
}
