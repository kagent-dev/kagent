import { test, expect } from "../../fixtures/test";
import {
  agentChat,
  agentPage,
  agents,
  dataRows,
  expectSettled,
  instances,
  loadPage,
  rowNamed,
  routes,
} from "../../helpers/app";
import { expectShell } from "../../helpers/nav";
import {
  CORE_NAV_ORDER,
  expectInline,
  expectPortalled,
  navOrder,
  slot,
} from "../../helpers/extensions";

/**
 * Extension points — the framework's contract, with an extension installed.
 *
 * Runs against the extension server (see `playwright.config.ts`). Every assertion
 * here is about the *mechanism*: that a component configured at a point mounts
 * at that point, in the DOM position the point promises, carrying the context
 * the point declares. None of it asserts what the bundled example renders, so
 * reshaping the example does not churn this file.
 */

test("extension points: configured components mount where the point promises", async ({
  page,
}) => {
  await test.step("1. shell points mount on every in-app page", async () => {
    await loadPage(page, routes.agents, { title: "Agents" });
    await expectShell(page);

    await expect(slot(page, "app_shell_appLayout_contentArea_leadingBanner")).toHaveCount(1);
    await expect(slot(page, "app_shell_appLayout_appSidebar_footer")).toHaveCount(1);
  });

  await test.step("2. an inline point renders where the slot sits", async () => {
    await expectInline(
      page,
      "app_shell_appLayout_contentArea_leadingBanner",
      page.getByTestId("app-content"),
    );
    await expectInline(
      page,
      "app_shell_appLayout_appSidebar_footer",
      page.getByTestId("app-sidebar"),
    );
  });

  await test.step("3. the portal point escapes the scrolling content area", async () => {
    // The one point whose render mode is observable only from where it landed.
    await expectPortalled(page, "app_shell_appLayout_contentArea_globalOverlay");
  });

  await test.step("4. page-level points mount on the page that declares them", async () => {
    await expect(slot(page, "app_agents_agentsList_pageHeader_actions")).toHaveCount(1);
  });

  await test.step("5. a per-row point mounts once per row", async () => {
    const badge = "app_agents_agentsList_agentListItem_badge";
    // On one agent's page, because that is where `AgentInstance` rows are listed
    // now: an instance is a conversation, so the agents list holds pairs and the
    // conversations sit inside one. The point's id and its context are unchanged —
    // it was always a point about an instance — which is the property this step is
    // really protecting: a contribution written against it keeps working.
    await loadPage(page, agentPage(agents.k8s));
    // One per row, whatever the list holds — counted from the table rather than
    // written down, so a fixture gaining a conversation is not a false failure while
    // a slot that mounted twice or not at all still fails. Waited for, because
    // counting too early compares a slot count against nought rows and passes for a
    // slot that never mounted.
    await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });
    await expectSettled(page);
    const rows = await dataRows(page).count();
    expect(rows).toBeGreaterThan(1);
    await expect(slot(page, badge)).toHaveCount(rows);

    // And each row carries its own, located by the short id the table shows beside
    // the conversation's name.
    for (const id of [instances.ready, instances.suspended]) {
      await expect(
        rowNamed(page, id.slice(0, 8)).locator(`[data-testid="extension-slot-${badge}"]`),
        `row "${id}" should carry its own slot`,
      ).toHaveCount(1);
    }
  });

  await test.step("6. each row's slot receives that row's context, not a shared one", async () => {
    // The trap: every badge renders identical copy, so a component that ignored
    // its context entirely would satisfy any text assertion. What proves context
    // is per-row is that what each one rendered is *distinguishable* — so this
    // asserts distinctness and deliberately says nothing about the values.
    const rendered = await slot(page, "app_agents_agentsList_agentListItem_badge")
      .evaluateAll((nodes) =>
        nodes.map((node) => node.firstElementChild?.getAttribute("data-testid") ?? ""),
      );

    // Counted against the list rather than against a written-down number, so a
    // fixture gaining an agent is not a false failure — while a contribution that
    // rendered nothing, or the same thing for every row, still fails.
    expect(
      rendered.length,
      "every row should have rendered something",
    ).toBeGreaterThan(1);
    expect(
      rendered.filter((value) => value !== ""),
      "each contribution should identify itself from its context",
    ).toHaveLength(rendered.length);
    expect(
      new Set(rendered).size,
      `contributions were indistinguishable, so context is not per-row: ${rendered.join(", ")}`,
    ).toBe(rendered.length);
  });

  await test.step("7. a contributed nav entry composes into the core list by order", async () => {
    // Asserted positionally rather than by name: the invariant is that a
    // contribution can land *between* core entries instead of being appended,
    // which is what "composes" means here.
    const order = await navOrder(page);
    const extras = order.filter((id) => !CORE_NAV_ORDER.includes(id));

    expect(extras, "expected exactly one contributed nav entry").toHaveLength(1);
    expect(order.indexOf(extras[0])).toBeGreaterThan(order.indexOf("nav-agents"));
    expect(order.indexOf(extras[0])).toBeLessThan(order.indexOf("nav-models"));

    // The core entries keep their own relative order around the insertion.
    expect(order.filter((id) => CORE_NAV_ORDER.includes(id))).toEqual(CORE_NAV_ORDER);
  });

  await test.step("8. a per-message point mounts once per message, with its own context", async () => {
    // The agent with a seeded conversation behind it. There is no session segment:
    // an AgentInstance *is* the conversation.
    await loadPage(page, agentChat(instances.ready));
    const point = "app_agents_agentChat_agentChatMessage_additionalActionsButton";

    const messages = page.getByTestId("chat-message");
    await expect(messages).toHaveCount(4);
    await expect(slot(page, point)).toHaveCount(4);

    // Same trap as the per-row badge: every contribution renders the same label,
    // so only distinctness proves each one received its own message.
    const rendered = await slot(page, point).evaluateAll((nodes) =>
      nodes.map((node) => node.firstElementChild?.getAttribute("data-testid") ?? ""),
    );
    expect(rendered.filter(Boolean)).toHaveLength(4);
    expect(
      new Set(rendered).size,
      `contributions were indistinguishable, so context is not per-message: ${rendered.join(", ")}`,
    ).toBe(4);
  });

  await test.step("9. a point on another page mounts there and only there", async () => {
    // The dashboard point is the one place a contribution appears that is not
    // the agents area, so it also checks that page-scoped points stay scoped.
    const dashboardPoint = "app_dashboard_dashboardOverview_summaryGrid_leadingCard";
    await expect(slot(page, dashboardPoint)).toHaveCount(0);

    await loadPage(page, routes.dashboard, { title: "Dashboard" });
    await expect(slot(page, dashboardPoint)).toHaveCount(1);
    await expectInline(page, dashboardPoint, page.getByTestId("app-content"));
  });

  await test.step("10. a contributed route renders inside the shell", async () => {
    await loadPage(page, routes.agents, { title: "Agents" });
    const order = await navOrder(page);
    const contributed = order.find((id) => !CORE_NAV_ORDER.includes(id))!;

    await page.getByTestId(contributed).click();
    await expect(page.getByTestId("page-title")).toBeVisible();
    // A contributed page is a page of this app, not a separate site.
    await expectShell(page);
  });
});
