import { test, expect } from "../../fixtures/test";
import { agentChat, agents, instances, loadPage, routes } from "../../helpers/app";
import { expectShell } from "../../helpers/nav";
import { CORE_NAV_ORDER, allSlots, navOrder } from "../../helpers/extensions";

/**
 * Extension points — with nothing installed.
 *
 * This is the shape a default build takes, and it is the case most likely to rot
 * unnoticed, because every other spec and every screenshot is taken with the
 * example switched on. A framework that only works when something is plugged
 * into it is a framework that breaks the day a deployment ships bare.
 *
 * Four tests rather than one journey: each claim reads a page of its own and none
 * builds on another, so one journey was six page loads on the thirty-second budget of a
 * single claim — and a load of the dev server is seconds on a busy runner, so the last
 * one ran out of time on a loaded Firefox run with nothing wrong on the page.
 */

test("extension points: a bare build mounts nothing, and its sidebar is the application's", async ({
  page,
}) => {
  await test.step("1. no point mounts anything anywhere", async () => {
    for (const [path, title] of [
      [routes.dashboard, "Dashboard"],
      [routes.agents, "Agents"],
      [routes.models, "Models"],
    ] as const) {
      // Wait for the page to have actually rendered before asserting an absence.
      // "Nothing is here" is trivially true of a page that has not mounted yet,
      // so without this anchor the whole step would pass on a blank screen.
      await loadPage(page, path, { title });
      await expect(
        allSlots(page),
        `${path} rendered an extension slot with no extension installed`,
      ).toHaveCount(0);
    }
  });

  await test.step("2. the sidebar shows exactly what the application ships", async () => {
    expect(await navOrder(page)).toEqual(CORE_NAV_ORDER);
  });
});

test("extension points: a bare build is otherwise whole", async ({ page }) => {
  // The absence of contributions must not take any of the app with it.
  await loadPage(page, routes.agents, { title: "Agents" });
  await expectShell(page);
  // By the harness as well as the template, because an agent is the pair: the
  // template alone appears on two rows, so matching it would pass on a build that
  // had lost the harness column entirely.
  await expect(
    page
      .getByRole("row")
      .filter({ hasText: agents.k8s.template })
      .filter({ hasText: agents.k8s.harness }),
  ).toHaveCount(1);
});

test("extension points: a bare build's agent rail carries the application's two entries and no more", async ({
  page,
}) => {
  await loadPage(page, agentChat(instances.ready));
  const rail = page.getByTestId("chat-sessions-nav");
  // Both destinations are addresses of this agent, and the chat page derives them
  // from the instance it is still fetching — so the nav is empty for a moment and
  // reading it straight away reads nothing. Half the test's budget, so that a rail
  // which never fills is what the failure names, not the test's own timeout.
  await expect(rail.getByTestId("agent-nav-agent-conversations")).toBeVisible({
    timeout: 15_000,
  });
  const order = await rail
    .locator("[data-testid]")
    .evaluateAll((nodes) => nodes.map((node) => node.getAttribute("data-testid") ?? ""));
  expect(order).toEqual(["agent-nav-agent-conversations", "chat-new-session"]);
});

test("extension points: a route only an extension would contribute is a 404 in a bare build", async ({
  page,
}) => {
  // The example contributes this path; with nothing installed the router must
  // not have quietly kept a slot for it.
  await loadPage(page, "/example/insights");
  await expect(page.getByTestId("not-found")).toBeVisible();
  await expect(allSlots(page)).toHaveCount(0);
});
