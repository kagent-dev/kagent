/**
 * The one thing the mock backend still answers as HTTP: oauth2-proxy's
 * `/oauth2/userinfo`, which is what the app reads to find out whether anybody is
 * signed in.
 *
 * Answered in-process, through the auth source's own fetch seam, like the API is
 * through its transport — no service worker. A worker in the page's path saw every
 * module request the dev server answers, held each one until the page replied that
 * it was not to be mocked, and a page navigating away never replies: under load
 * Firefox then drew the next page and never fired its `load`.
 *
 * The three states a deployment can be in, driven by `?auth=` and `unsecured` by
 * default — mock mode has no backend to have signed in to, so reporting anybody
 * would be a fabrication. The shapes are the ones the app actually distinguishes:
 * - `unsecured`     — HTML, which is what a plain file server answers an unknown
 *                     path with. That is the evidence nothing is fronting the app.
 * - `authenticated` — the userinfo document.
 * - `expired`       — 401, which is what the proxy sends when its session lapsed.
 */

import { createOAuth2ProxyAuthSource } from "@/auth";
import type { AuthSource } from "@/auth";
import { currentAuthScenario } from "./scenario";

export function mockUserInfoResponse(): Response {
  const scenario = currentAuthScenario();

  if (scenario === "authenticated") {
    return Response.json({
      user: "alice",
      email: "alice@example.com",
      preferredUsername: "alice",
      groups: ["platform"],
    });
  }

  if (scenario === "expired") {
    return Response.json({}, { status: 401 });
  }

  return new Response("<!doctype html><html><body>app</body></html>", {
    headers: { "content-type": "text/html" },
  });
}

/** The oauth2-proxy source, reading its answer from the scenario instead of the network. */
export function createMockAuthSource(): AuthSource {
  return createOAuth2ProxyAuthSource({
    fetchImpl: async () => mockUserInfoResponse(),
  });
}
