import { test, expect } from "../fixtures/test";
import { expectPageTitle, loadApp, routes } from "../helpers/app";
import { clickNav, expectShell } from "../helpers/nav";

/**
 * Routing, which is the part of this app a *server* can still get wrong.
 *
 * Client-side routing means a deep link is not a file on disk, so something has to
 * answer `/substrate` and `/no-such-page` with the app shell rather than a 404. In a
 * cluster that something is nginx's `try_files $uri $uri/ /index.html`; under `yarn dev`
 * it is Vite's own fallback, which is a different implementation of the same promise.
 * Steps 4 and 5 are here rather than in `tests/` for exactly that reason: they are the
 * one thing in this suite that can pass against a dev server and fail against the image.
 *
 * What stays in `tests/routing.spec.ts` is the two steps needing fixtures: a deep link
 * carrying an instance id, and the standalone login route.
 */

test("routing: in-app navigation, deep links and 404, on either backend", async ({
  page,
}) => {
  await test.step("1. a sidebar click changes both the URL and the content", async () => {
    await loadApp(page, routes.dashboard);
    await expectPageTitle(page, "Dashboard");

    await clickNav(page, "agents", /\/agents(\?|$)/);
    await expectPageTitle(page, "Agents");

    await clickNav(page, "prompts", /\/prompts(\?|$)/);
    await expectPageTitle(page, "Prompts");
  });

  await test.step("2. the sidebar marks the active destination", async () => {
    await expect(page.getByTestId("nav-prompts")).toHaveClass(/ant-menu-item-selected/);
    await expect(page.getByTestId("nav-agents")).not.toHaveClass(
      /ant-menu-item-selected/,
    );
  });

  await test.step("3. browser history moves between routes", async () => {
    await page.goBack();
    await page.waitForURL(/\/agents(\?|$)/);
    await expectPageTitle(page, "Agents");

    await page.goForward();
    await page.waitForURL(/\/prompts(\?|$)/);
    await expectPageTitle(page, "Prompts");
  });

  await test.step("4. a deep link renders that route on a cold load", async () => {
    // A full page load, not a client-side transition: this is the link somebody pastes
    // into chat, and the one a server that does not fall back to index.html would break.
    await loadApp(page, routes.substrate);
    await expectPageTitle(page, "Substrate");
    await expectShell(page);
    await expect(page).toHaveURL(/\/substrate/);
  });

  await test.step("5. an unknown path renders 404 inside the shell", async () => {
    await loadApp(page, "/no-such-page");
    // The address it tried, which is what a reader compares against the link they
    // followed — "that page does not exist" told them nothing they could act on.
    await expect(page.getByTestId("not-found-path")).toHaveText("/no-such-page");
    // And somewhere to go that is not just "back to the dashboard", which is the right
    // destination only if that is where they were headed.
    await expect(page.getByTestId("not-found-link-agents")).toBeVisible();
    // Still inside the app: a wrong URL should not strand the reader with no way back.
    await expectShell(page);

    await page.getByTestId("not-found-dashboard").click();
    await page.waitForURL(/\/$/);
    await expectPageTitle(page, "Dashboard");
  });
});
