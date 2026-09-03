import { expect, test } from "../../fixtures/test";

/**
 * The three authentication states, each driven end to end.
 *
 * **This is the spec to run when the question is "does authentication still work".** It
 * needs no proxy, no identity provider and no cluster: the mock backend answers
 * oauth2-proxy's `/oauth2/userinfo`, which is the only thing the app asks about who is
 * signed in, and `?auth=` chooses what it answers.
 *
 * - **unsecured** — nothing is fronting the app. It must work exactly as in development
 *   and must *never* redirect: there is no `/oauth2` endpoint to redirect to, so the
 *   browser would bounce between the app and a 404 forever.
 * - **authenticated** — a proxy is in front and the session is good; the reader's identity
 *   appears in the header.
 * - **expired** — a proxy is in front and its session has lapsed. The app leaves for the
 *   proxy on its own, carrying `rd` so the reader returns to the page they were reading.
 *
 * The last is what regressed in the rewrite. The header offered a button to a page that
 * offered another button, where the UI this replaced recovered without being asked —
 * while `AuthStatus`'s own doc comment said the UI "should re-run OIDC" the whole time.
 */

/** The proxy's start endpoint. Not served by anything, so the SPA fallback answers it. */
const START = "/oauth2/start";

test.describe("authentication", () => {
  test("unsecured: the app works, and never redirects", async ({ page }) => {
    // No `?auth=`, which is the default and what mock mode should say: there is no
    // backend to have signed in to.
    const navigations: string[] = [];
    page.on("framenavigated", (frame) => {
      if (frame === page.mainFrame()) navigations.push(frame.url());
    });

    await page.goto("/agents");
    await expect(page.getByTestId("agents-table")).toBeVisible({ timeout: 30_000 });

    // Usable, and silent about sessions.
    await expect(page.getByTestId("header-reauth")).toHaveCount(0);
    await expect(page.getByTestId("header-user")).toHaveCount(0);

    await page.waitForTimeout(2_000);
    expect(
      navigations.filter((url) => url.includes(START)),
      "an unsecured deployment must never be sent to a proxy that is not there",
    ).toEqual([]);
  });

  test("authenticated: the header names the reader", async ({ page }) => {
    await page.goto("/agents?auth=authenticated");

    await expect(page.getByTestId("header-user")).toBeVisible({ timeout: 30_000 });
    await expect(page.getByTestId("header-user")).toContainText("alice");
    // Nothing asks them to sign in, because they are signed in.
    await expect(page.getByTestId("header-reauth")).toHaveCount(0);
  });

  test.describe("with a lapsed session", () => {
    // The 401 from `/oauth2/userinfo` *is* the state under test, so the console fixture
    // has to be told to expect it. Scoped to these two tests and to that one status: a
    // blanket allowance would stop the fixture doing its job for everything else here.
    test.use({
      expectedNoise: [
        /Failed to load resource: the server responded with a status of 401/,
      ],
    });

  test("expired: the app re-authenticates itself, and comes back here", async ({
    page,
  }) => {
    await page.goto("/agents?auth=expired&filter=k8s");

    // Nothing is clicked. The app notices and leaves.
    await page.waitForURL((url) => url.pathname === START, { timeout: 30_000 });

    // Carrying where the reader was, so signing in does not cost them the page.
    const rd = new URL(page.url()).searchParams.get("rd");
    expect(rd).toContain("/agents");
    expect(rd).toContain("filter=k8s");
  });

  test("an attempt already spent: it stays put and offers the way out", async ({
    page,
  }) => {
    // The guard is seeded directly rather than earned by driving the browser through the
    // proxy's path. `/oauth2/start` is not a route this app owns — the SPA fallback serves
    // it, so the app boots there and its first API calls race the mock worker's
    // registration, which surfaced as CORS noise the console fixture rightly failed on.
    // What is under test is what the app does when an attempt is *already* spent, and that
    // needs no round trip to set up.
    await page.addInitScript(() => {
      window.sessionStorage.setItem("kagent_reauth_attempt", String(Date.now()));
    });

    await page.goto("/agents?auth=expired");

    // Offered the way out rather than bounced: a proxy that keeps handing back a token it
    // will not refresh must not become a redirect loop, because a reader cannot read an
    // error on a page that keeps leaving.
    await expect(page.getByTestId("header-reauth")).toBeVisible({ timeout: 30_000 });

    await page.waitForTimeout(2_000);
    expect(
      new URL(page.url()).pathname,
      "a spent attempt must not send the reader away again",
    ).toBe("/agents");
  });
  });
});
