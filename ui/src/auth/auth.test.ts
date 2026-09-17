import { afterEach, describe, expect, it } from "vitest";
import {
  classifyUserInfoResponse,
  createOAuth2ProxyAuthSource,
  toAuthUser,
} from "./oauth2ProxyAuthSource";
import {
  RUNTIME_CONFIG_DEFAULTS,
  runtimeConfig,
} from "@/api/runtimeConfig";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function htmlResponse(status = 200): Response {
  return new Response("<!doctype html><html><body>app shell</body></html>", {
    status,
    headers: { "content-type": "text/html" },
  });
}

describe("classifyUserInfoResponse", () => {
  it("reads a userinfo document as an authenticated session", async () => {
    const result = await classifyUserInfoResponse(
      jsonResponse({
        user: "alice",
        email: "alice@example.test",
        groups: ["platform"],
      }),
    );
    expect(result.status).toBe("authenticated");
    expect(result.user).toEqual({
      id: "alice",
      displayName: "alice",
      email: "alice@example.test",
      groups: ["platform"],
    });
  });

  it("treats 401 and 403 as an expired session", async () => {
    expect((await classifyUserInfoResponse(jsonResponse({}, 401))).status).toBe(
      "expired",
    );
    expect((await classifyUserInfoResponse(jsonResponse({}, 403))).status).toBe(
      "expired",
    );
  });

  // The one that matters. A static SPA answers unknown paths with index.html,
  // so "no proxy in front" arrives as 200 text/html, not a 404. Reading that as
  // a session — or as an error worth redirecting on — is the redirect loop.
  it("treats a 200 HTML app shell as unsecured, not authenticated", async () => {
    const result = await classifyUserInfoResponse(htmlResponse(200));
    expect(result.status).toBe("unsecured");
    expect(result.user).toBeNull();
  });

  it("treats a 404 as unsecured", async () => {
    expect((await classifyUserInfoResponse(htmlResponse(404))).status).toBe(
      "unsecured",
    );
  });

  it("treats a 500 as unsecured rather than expired", async () => {
    // A broken proxy must not send the user round the OIDC flow.
    expect((await classifyUserInfoResponse(jsonResponse({}, 500))).status).toBe(
      "unsecured",
    );
  });

  it("treats unparsable JSON as unsecured", async () => {
    const broken = new Response("{not json", {
      status: 200,
      headers: { "content-type": "application/json" },
    });
    expect((await classifyUserInfoResponse(broken)).status).toBe("unsecured");
  });

  it("treats a JSON document identifying nobody as unsecured", async () => {
    expect((await classifyUserInfoResponse(jsonResponse({}))).status).toBe(
      "unsecured",
    );
    expect(
      (await classifyUserInfoResponse(jsonResponse(["not", "an", "object"])))
        .status,
    ).toBe("unsecured");
  });
});

describe("toAuthUser", () => {
  it("prefers a human label over the raw subject", () => {
    expect(
      toAuthUser({ sub: "abc-123", preferredUsername: "Alice Smith" }),
    ).toMatchObject({ id: "abc-123", displayName: "Alice Smith" });
  });

  it("accepts the OIDC snake_case spelling too", () => {
    expect(
      toAuthUser({ sub: "abc-123", preferred_username: "alice" }),
    ).toMatchObject({ displayName: "alice" });
  });

  it("falls back to the identifier when there is no label", () => {
    expect(toAuthUser({ user: "svc-account" })).toMatchObject({
      id: "svc-account",
      displayName: "svc-account",
    });
  });

  it("drops non-string group entries", () => {
    expect(toAuthUser({ user: "a", groups: ["ops", 7, null] })?.groups).toEqual([
      "ops",
    ]);
  });

  it("returns null when nothing identifies the user", () => {
    expect(toAuthUser({ groups: ["ops"] })).toBeNull();
    expect(toAuthUser(null)).toBeNull();
    expect(toAuthUser("alice")).toBeNull();
  });
});

describe("createOAuth2ProxyAuthSource", () => {
  it("reports unsecured when the fetch itself fails", async () => {
    const source = createOAuth2ProxyAuthSource({
      fetchImpl: () => Promise.reject(new Error("ECONNREFUSED")),
    });
    // A transport failure is evidence that nothing is in front, not an error
    // to surface — so `resolve` must not reject.
    await expect(source.resolve()).resolves.toEqual({
      status: "unsecured",
      user: null,
    });
  });

  it("requests the userinfo path with credentials", async () => {
    let seenUrl = "";
    let seenInit: RequestInit | undefined;
    const source = createOAuth2ProxyAuthSource({
      fetchImpl: (input, init) => {
        seenUrl = String(input);
        seenInit = init;
        return Promise.resolve(jsonResponse({ user: "alice" }));
      },
    });

    const result = await source.resolve();
    expect(seenUrl).toBe("/oauth2/userinfo");
    expect(seenInit?.credentials).toBe("same-origin");
    expect(result.status).toBe("authenticated");
  });

  it("honours a custom userinfo path", async () => {
    let seenUrl = "";
    const source = createOAuth2ProxyAuthSource({
      userInfoPath: "/auth/me",
      fetchImpl: (input) => {
        seenUrl = String(input);
        return Promise.resolve(jsonResponse({ user: "alice" }));
      },
    });
    await source.resolve();
    expect(seenUrl).toBe("/auth/me");
  });
});

describe("runtimeConfig", () => {
  afterEach(() => {
    delete window.environmentVariables;
  });

  // No script tag is rendered by `yarn dev` before Vite's plugin runs, none is
  // rendered under vitest, and a deployment whose startup script failed serves
  // none either. Every one of those has to be silent and normal.
  it("uses defaults when nothing was injected", () => {
    expect(runtimeConfig()).toEqual(RUNTIME_CONFIG_DEFAULTS);
  });

  it("uses what the deployment injected", () => {
    window.environmentVariables = {
      SSO_REDIRECT_PATH: "/custom/start",
      STREAM_TIMEOUT_MS: "90000",
    };
    expect(runtimeConfig()).toEqual({
      ssoRedirectPath: "/custom/start",
      streamTimeoutMs: 90_000,
    });
  });

  it("keeps the good keys when one is malformed", () => {
    window.environmentVariables = {
      SSO_REDIRECT_PATH: "/ok/start",
      STREAM_TIMEOUT_MS: "nope",
    };
    expect(runtimeConfig()).toEqual({
      ssoRedirectPath: "/ok/start",
      streamTimeoutMs: RUNTIME_CONFIG_DEFAULTS.streamTimeoutMs,
    });
  });

  // A zero would abort every chat stream the instant it opened, which reads as
  // the backend hanging up rather than as a bad setting.
  it("rejects a non-positive timeout", () => {
    window.environmentVariables = { STREAM_TIMEOUT_MS: "0" };
    expect(runtimeConfig().streamTimeoutMs).toBe(
      RUNTIME_CONFIG_DEFAULTS.streamTimeoutMs,
    );
  });

  // An unset chart value renders as an empty string rather than an absent key,
  // so empty has to mean "not configured" and not "configured as nothing".
  it("treats an empty value as unset", () => {
    window.environmentVariables = { SSO_REDIRECT_PATH: "" };
    expect(runtimeConfig().ssoRedirectPath).toBe(
      RUNTIME_CONFIG_DEFAULTS.ssoRedirectPath,
    );
  });
});
