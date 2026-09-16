import { test, expect } from "../fixtures/test";
import { agentChat, instances, loadPage, expectPageTitle, routes } from "../helpers/app";
import { expectNoShell, expectShell } from "../helpers/nav";

/**
 * Routing, the two parts of it that need fixtures.
 *
 * A deep link carrying an `AgentInstance` id has to name a conversation that exists,
 * and the login route has to have something to log in to — neither of which a clean
 * cluster supplies. The rest of the journey is backend-agnostic and runs against both
 * from `shared/routing.spec.ts`, including the deep link and 404 steps, which are the
 * ones a server can get wrong.
 */

test("routing: a deep link with params, and the standalone login route", async ({
  page,
}) => {
  await test.step("1. a deep link with route params renders too", async () => {
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

  await test.step("2. login renders standalone, outside the shell", async () => {
    await loadPage(page, routes.login);
    await expect(page.getByTestId("login-page")).toBeVisible();
    await expectNoShell(page);

    await page.getByTestId("login-submit").click();
    await page.waitForURL(/\/$/);
    await expectShell(page);
    await expectPageTitle(page, "Dashboard");
  });
});
