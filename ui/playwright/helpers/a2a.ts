// A2A chat helpers for the failure path.
//
// The success-path chat reply is mocked server-side by the proxy (see
// playwright/mocks/server.mjs), which intercepts /a2a and returns a canned SSE
// stream — so specs don't stub the happy path in the browser at all.
//
// The failure path is different: to simulate a broken stream we abort the request
// in the browser before it reaches the Next route handler. `POST /a2a/<ns>/<name>`
// is a real browser fetch, so page.route CAN intercept it here.

import { type Page } from "@playwright/test";

/** Intercept the chat SSE call and fail it (network error), for the failure path. */
export async function mockAgentStreamError(page: Page): Promise<void> {
  const handler = (route: import("@playwright/test").Route) => route.abort("failed");
  await page.route("**/a2a/**", handler);
  await page.route("**/a2a-sandboxes/**", handler);
}
