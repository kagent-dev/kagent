import { test, expect } from "../fixtures/test";
import { agentChat, instances, loadPage, expectPageTitle, routes } from "../helpers/app";
import { clickNav, expectNoShell, expectShell } from "../helpers/nav";

/**
 * Routing — new coverage for the thing this rewrite changed most.
 *
 * The old app routed on the server through Next's file-system router; this one
 * is a single-page app with a client-side router, which puts four behaviours at
 * risk that used to come for free: in-app navigation, deep linking straight to a
 * route, an unknown path resolving to a 404 rather than a blank screen, and a
 * standalone route rendering outside the shell.
 */

test("routing: in-app navigation, deep links, 404, and standalone routes", async ({
  page,
}) => {
  await test.step("1. a sidebar click changes both the URL and the content", async () => {
    await loadPage(page, routes.dashboard, { title: "Dashboard" });

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
    // A full page load, not a client-side transition: this is the link someone
    // pastes into chat, and the one a server that does not fall back to
    // index.html would break.
    await loadPage(page, routes.substrate, { title: "Substrate" });
    await expectShell(page);
    await expect(page).toHaveURL(/\/substrate/);
  });

  await test.step("5. a deep link with route params renders too", async () => {
    // Two params: the namespace and the AgentInstance id, which is how every agent
    // surface is addressed now.
    await loadPage(page, agentChat(instances.ready));
    // The agent surfaces carry no page heading — the rail names the agent instead —
    // so what proves the params reached the route is the rail being scoped to them.
    // The card shows the template, which is what a reader recognises the agent by,
    // over the short id that distinguishes this conversation from its siblings.
    await expect(page.getByTestId("agent-rail-identity")).toContainText(
      instances.ready.slice(0, 8),
    );
    await expect(page.getByTestId("chat-panel")).toBeVisible();
  });

  await test.step("6. an unknown path renders 404 inside the shell", async () => {
    await loadPage(page, "/no-such-page");
    // The address it tried, which is what a reader compares against the link they
    // followed — "that page does not exist" told them nothing they could act on.
    await expect(page.getByTestId("not-found-path")).toHaveText("/no-such-page");
    // And somewhere to go that is not just "back to the dashboard", which is the right
    // destination only if that is where they were headed.
    await expect(page.getByTestId("not-found-link-agents")).toBeVisible();
    // Still inside the app: a wrong URL should not strand the user with no
    // way back.
    await expectShell(page);

    await page.getByTestId("not-found-dashboard").click();
    await page.waitForURL(/\/$/);
    await expectPageTitle(page, "Dashboard");
  });

  await test.step("7. login renders standalone, outside the shell", async () => {
    await loadPage(page, routes.login);
    await expect(page.getByTestId("login-page")).toBeVisible();
    await expectNoShell(page);

    await page.getByTestId("login-submit").click();
    await page.waitForURL(/\/$/);
    await expectShell(page);
    await expectPageTitle(page, "Dashboard");
  });
});
