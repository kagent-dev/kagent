/**
 * The one thing the mock backend still answers over HTTP.
 *
 * Everything else moved. The application API is gRPC, so the fixtures are served
 * as a transport (`transport.ts`) with no service worker in the path at all. What
 * is left here is `/oauth2/userinfo`, which is not the controller's endpoint — it
 * is the *proxy's*, and it is what the app reads to find out whether anybody is
 * signed in. There is no gRPC service to fake it with.
 *
 * A conversation is not here either, and never was: chat in mock mode is
 * `MockChatClient` in `@/api/chat/mockChatClient`, a fake of the chat port rather
 * than of the network, driven by the `?chat=` axis.
 */

import { HttpResponse, http } from "msw";
import { currentAuthScenario } from "./scenario";

export const handlers = [
  // oauth2-proxy's endpoint, answered here so the three states a deployment can be
  // in are reachable without a proxy, an identity provider or a cluster. Driven by
  // `?auth=`, and `unsecured` by default: mock mode has no backend to have signed
  // in to, so reporting anybody would be a fabrication.
  //
  // The shapes are the ones the app actually distinguishes, not invented ones:
  // - `unsecured`     — HTML, which is what a plain file server answers an unknown
  //                     path with. That is the evidence nothing is fronting the app.
  // - `authenticated` — the userinfo document.
  // - `expired`       — 401, which is what the proxy sends when its session lapsed.
  http.get("/oauth2/userinfo", () => {
    const scenario = currentAuthScenario();

    if (scenario === "authenticated") {
      return HttpResponse.json({
        user: "alice",
        email: "alice@example.com",
        preferredUsername: "alice",
        groups: ["platform"],
      });
    }

    if (scenario === "expired") {
      return HttpResponse.json({}, { status: 401 });
    }

    return HttpResponse.html("<!doctype html><html><body>app</body></html>");
  }),
];
