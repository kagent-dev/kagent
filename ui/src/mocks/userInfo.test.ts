import { afterEach, describe, expect, it } from "vitest";
import { createMockAuthSource } from "./userInfo";

function openWith(search: string): void {
  window.history.replaceState(null, "", `/${search}`);
}

describe("the mock userinfo", () => {
  afterEach(() => {
    openWith("?auth=unsecured");
    window.history.replaceState(null, "", "/");
  });

  // Through the real oauth2-proxy source, so each answer is classified by the code a
  // deployment runs rather than by the fixture's own idea of what it means.
  it.each([
    ["unsecured", { status: "unsecured", user: null }],
    ["expired", { status: "expired", user: null }],
    [
      "authenticated",
      {
        status: "authenticated",
        user: {
          id: "alice",
          displayName: "alice",
          email: "alice@example.com",
          groups: ["platform"],
        },
      },
    ],
  ])("answers the %s scenario as that state", async (scenario, expected) => {
    openWith(`?auth=${scenario}`);
    await expect(createMockAuthSource().resolve()).resolves.toEqual(expected);
  });
});
