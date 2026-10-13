/**
 * Starts the in-browser mock backend, and hands back the auth source that goes
 * with it.
 *
 * Both halves are served in-process, with nothing intercepting the network. The
 * application API is gRPC, and its fixtures are installed as the transport every
 * operation calls through — so nothing above the data layer behaves differently.
 * `/oauth2/userinfo` is oauth2-proxy's HTTP endpoint, and its answer is read
 * through the auth source's fetch seam (`userInfo.ts`).
 *
 * Order matters: the transport is installed before anything can call an
 * operation, which is why this is awaited in `main.tsx` before the app renders.
 * Only `main.tsx` calls it, and only when `KAGENT_UI_VITE_API_MODE` is not `"live"` — a page
 * that quietly served fixtures because the backend was unreachable would look
 * healthy while showing data that was never real.
 */

import { setApiTransport } from "@/api/transport";
import type { AuthSource } from "@/auth";
import { mockTransport } from "./transport";
import { createMockAuthSource } from "./userInfo";
import {
  AUTH_SCENARIOS,
  AUTH_SCENARIO_PARAM,
  CHAT_SCENARIOS,
  CHAT_SCENARIO_PARAM,
  MOCK_SCENARIOS,
  MOCK_SCENARIO_PARAM,
  currentScenario,
} from "./scenario";

export function startMockBackend(): AuthSource {
  setApiTransport(mockTransport);

  console.info(
    `[mock backend] serving the gRPC API from fixtures — scenario "${currentScenario()}". ` +
      `Append ?${MOCK_SCENARIO_PARAM}=<${MOCK_SCENARIOS.join("|")}>, ` +
      `?${CHAT_SCENARIO_PARAM}=<${CHAT_SCENARIOS.join("|")}> or ` +
      `?${AUTH_SCENARIO_PARAM}=<${AUTH_SCENARIOS.join("|")}> to any route to change it.`,
  );

  return createMockAuthSource();
}
