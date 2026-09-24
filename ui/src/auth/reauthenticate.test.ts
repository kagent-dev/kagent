import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  REAUTH_GUARD_WINDOW_MS,
  clearReauthenticationAttempt,
  reauthenticationLooping,
  reauthenticationUrl,
  startReauthentication,
} from "./reauthenticate";

/**
 * The guard, which is the part with teeth.
 *
 * Re-authenticating automatically is the behaviour being restored; refusing to do it
 * twice is what keeps the restoration from being worse than the bug. A proxy whose own
 * cookie is valid can keep handing back an id_token it will not refresh, and a browser
 * bouncing between the app and the proxy is worse than a stuck page — the reader cannot
 * even read the error.
 */

const replace = vi.fn();

beforeEach(() => {
  replace.mockClear();
  window.sessionStorage.clear();
  window.environmentVariables = { SSO_REDIRECT_PATH: "/oauth2/start" };
  Object.defineProperty(window, "location", {
    configurable: true,
    value: {
      pathname: "/agents/kagent/k8s-agent/chat",
      search: "?tab=logs",
      hash: "#latest",
      replace,
    },
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("reauthenticationUrl", () => {
  it("comes back to the page being read, not the front door", () => {
    // Without `rd`, the proxy returns the reader wherever it defaults to — which is how
    // signing in again used to cost somebody the page they were on.
    expect(reauthenticationUrl(window.location)).toBe(
      "/oauth2/start?rd=%2Fagents%2Fkagent%2Fk8s-agent%2Fchat%3Ftab%3Dlogs%23latest",
    );
  });

  it("uses the path the deployment injected", () => {
    window.environmentVariables = { SSO_REDIRECT_PATH: "/custom/start" };

    expect(reauthenticationUrl(window.location)).toContain("/custom/start?rd=");
  });
});

describe("startReauthentication", () => {
  it("leaves for the proxy the first time", () => {
    expect(startReauthentication(1_000)).toBe(true);
    expect(replace).toHaveBeenCalledWith(
      expect.stringContaining("/oauth2/start?rd="),
    );
  });

  it("refuses a second attempt inside the window", () => {
    startReauthentication(1_000);
    replace.mockClear();

    // Came back still expired: redirecting again is the loop.
    expect(startReauthentication(1_000 + REAUTH_GUARD_WINDOW_MS - 1)).toBe(false);
    expect(replace).not.toHaveBeenCalled();
  });

  it("allows one again once the window has passed", () => {
    startReauthentication(1_000);
    replace.mockClear();

    // A reader who genuinely lapses twice in a long session should not be refused.
    expect(startReauthentication(1_000 + REAUTH_GUARD_WINDOW_MS + 1)).toBe(true);
    expect(replace).toHaveBeenCalled();
  });

  it("is armed again as soon as a session is established", () => {
    startReauthentication(1_000);
    // What `AuthProvider` does on `authenticated`.
    clearReauthenticationAttempt();

    expect(reauthenticationLooping(1_000)).toBe(false);
    expect(startReauthentication(1_000)).toBe(true);
  });

  it("replaces rather than pushes", () => {
    // A page that could not authenticate is not somewhere Back should return to: it
    // would immediately try to leave again.
    startReauthentication(1_000);

    expect(replace).toHaveBeenCalled();
  });
});
