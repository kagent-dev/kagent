import { test, expect } from "../../fixtures/test";
import {
  agentNewChat,
  agentPage,
  agents,
  dataRows,
  expectSettled,
  loadPage,
  rowNamed,
  routes,
  withScenario,
} from "../../helpers/app";
import { clickRefresh } from "../../helpers/resource";
import { operationCalls, rpc } from "../../helpers/mockCalls";
import { optionNamed } from "../../helpers/resource";
import { background, settledPaint } from "../../helpers/style";

/** Agents — each row is one explicit Agent, whose template and harness are each shared or inline. */
test("agents: the list is Agent resources, with each half shared or inline", async ({
  page,
}) => {
  await test.step("1. every namespace by default", async () => {
    await loadPage(page, routes.agents, { title: "Agents" });
    await expectSettled(page);
    await expect(dataRows(page)).toHaveCount(9);
    await expect(page.getByTestId("agents-summary")).toHaveText("9 of 9 agents");
    await expect(page.getByTestId("agents-table")).toContainText("analytics");
  });

  await test.step("2. each row says whether its template and harness are shared or inline", async () => {
    const sources = async (name: string) => [
      await page.getByTestId(`agent-template-kagent/${name}`).getAttribute("data-source"),
      await page.getByTestId(`agent-harness-kagent/${name}`).getAttribute("data-source"),
    ];
    expect(await sources("shared-brain")).toEqual(["reference", "reference"]);
    expect(await sources("release-notes")).toEqual(["inline", "reference"]);
    expect(await sources("triage-on-claude")).toEqual(["reference", "inline"]);
    expect(await sources("scratchpad")).toEqual(["inline", "inline"]);
    await expect(page.getByTestId("agent-template-kagent/shared-brain")).toHaveText("shared-brain");
    await expect(page.getByTestId("agent-harness-kagent/shared-brain")).toHaveText("k8s-agent");
  });

  await test.step("3. two Agents with identical refs are two rows with two addresses", async () => {
    await expect(page.getByTestId("agent-link-kagent-shared-brain")).toHaveAttribute(
      "href", agentNewChat(agents.sharedOnK8s),
    );
    await expect(page.getByTestId("agent-link-kagent-shared-brain-twin")).toHaveAttribute(
      "href", agentNewChat({ name: "shared-brain-twin", template: "", harness: "" }),
    );
  });

  await test.step("4. a template without an Agent is not listed", async () => {
    await expect(rowNamed(page, "note-taker")).toHaveCount(0);
  });

  await test.step("5. status is ready, preparing or not reported", async () => {
    await expect(
      page.getByTestId(`agent-revision-kagent/${agents.k8s.name}`),
    ).toHaveAttribute("data-revision-state", "ready");
    const preparing = page.getByTestId(`agent-revision-kagent/${agents.preparing.name}`);
    await expect(preparing).toHaveAttribute("data-revision-state", "preparing");
    await expect(preparing).toHaveText("Preparing");
  });

  await test.step("6. conversation counts are per Agent, not per template", async () => {
    await expect(page.getByTestId(`agent-conversations-kagent/${agents.k8s.name}`)).toHaveText("4 conversations");
    await expect(page.getByTestId(`agent-conversations-kagent/${agents.sharedOnK8s.name}`)).toHaveText("1 conversation");
    await expect(page.getByTestId("agent-conversations-kagent/shared-brain-twin")).toHaveText("0 conversations");
  });

  await test.step("7. a conversation whose Agent is gone is reported", async () => {
    await expect(page.getByTestId("agents-orphaned-conversations")).toBeVisible();
  });
});

/**
 * The filter bar, on the page whose old controls it replaces.
 *
 * The shared component is unit-tested and covered on the other three lists; what is
 * asserted here is the behaviour that used to be two contradictory controls — a
 * single-select namespace beside an "all namespaces" toggle, where choosing a
 * namespace and leaving the toggle on left the reader unsure which won.
 */
test("agents: selecting no namespace means every namespace, and a pill undoes one", async ({
  page,
}) => {
  await loadPage(page, routes.agents, { title: "Agents" });
  await expectSettled(page);

  await test.step("1. nothing selected is every namespace", async () => {
    await expect(dataRows(page)).toHaveCount(9);
    // No pills, because nothing is narrowing: a pill row over an unfiltered list
    // would be a control saying something is hidden when nothing is.
    await expect(page.getByTestId("agents-filters-pills")).toHaveCount(0);
  });

  await test.step("2. choosing one narrows the list and shows it as a pill", async () => {
    await page.getByTestId("agents-filters-filter-ns").click();
    // Located by `title` on the option element, not by role: rc-select renders a
    // second, zero-sized `role=listbox` for screen readers, and Playwright resolves
    // it happily and then waits for a visibility that never arrives.
    await optionNamed(page, "analytics").click();
    await page.keyboard.press("Escape");

    await expect(page.getByTestId("agents-filters-pill-ns-analytics")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(1);
    // In the address, so a narrowed view can be linked to and survives a reload.
    await expect(page).toHaveURL(/ns=analytics/);
  });

  await test.step("3. the pill removes exactly that filter", async () => {
    await page.getByTestId("agents-filters-pill-ns-analytics").click();
    await expect(dataRows(page)).toHaveCount(9);
    await expect(page).not.toHaveURL(/ns=analytics/);
  });

  await test.step("4. searching covers every row, not just the page on screen", async () => {
    await page.getByTestId("agents-filters-search").fill("fast-lane");
    // Matched on the harness, which is half of what an agent *is* — a search over
    // template names alone could never find one of two agents cut from one template.
    await expect(dataRows(page)).toHaveCount(1);
    await expect(page.getByTestId("agents-summary")).toHaveText("1 of 9 agents");

    // The search term is a filter like any other, so it has a pill and "clear
    // filters" means it.
    await expect(page.getByTestId("agents-filters-pill-search")).toBeVisible();
    await page.getByTestId("agents-filters-pill-clear").click();
    await expect(dataRows(page)).toHaveCount(9);
  });
});

test("agents: conversations with no agent are one click away", async ({ page }) => {
  // Deleting an Agent leaves its conversations running on their prepared revision.
  await loadPage(page, routes.agents, { title: "Agents" });
  await expectSettled(page);

  await test.step("1. the notice counts them", async () => {
    await expect(page.getByTestId("agents-orphaned-conversations")).toContainText("1 conversation belongs to no agent here");
  });

  await test.step("2. and links to where they can be opened and deleted", async () => {
    await page.getByTestId("agents-orphaned-link").click();
    await page.waitForURL(/\/agents\/unmapped$/);
    await expect(page.getByTestId("unmapped-table")).toBeVisible();
    await expect(page.getByTestId("unmapped-table").locator("tbody tr").first().locator("td").nth(1)).toHaveText("not reported");
  });
});

/**
 * Agents, templates and harnesses are three tabs of one surface.
 *
 * They were separate destinations with their own sidebar entries, which put the three
 * halves of one idea in three places and left the relationship between them implicit —
 * a reader looking at templates had no way to see which harness would run them, and a
 * reader looking for "New agent" was looking for something that does not exist.
 *
 * The tab lives in the URL, so it can be linked to and survives a reload. That is the
 * part worth asserting: a tab held in state looks identical until somebody shares the
 * address of what they are looking at.
 */
test("agents: the landing page is three tabs, and the tab is in the address", async ({
  page,
}) => {
  await loadPage(page, routes.agents, { title: "Agents" });

  await test.step("1. the concepts are stated before the list", async () => {
    // The overview explains what each resource owns.
    const concepts = page.getByTestId("agent-concepts");
    await expect(concepts).toBeVisible();
    await expect(concepts).toContainText("AgentTemplate");
    await expect(concepts).toContainText("Harness");
    await expect(page.getByTestId("concepts-pairing")).toHaveText("An agent is one template plus one harness.");
    await expect(page.getByTestId("concepts-sources")).toContainText("shared template or harness");
  });

  await test.step("2. each tab is reachable and shows its own list", async () => {
    await page.getByRole("tab", { name: "Templates" }).click();
    await expect(page).toHaveURL(/tab=templates/);
    // The tab itself carries no controls: creating and refreshing act on the whole
    // page, so they live in its header rather than three times over.
    await expect(page.getByTestId("templates-filters")).toBeVisible();

    await page.getByRole("tab", { name: /Harnesses/ }).click();
    await expect(page).toHaveURL(/tab=harnesses/);
    await expect(page.getByTestId("harnesses-table")).toBeVisible({ timeout: 30_000 });
  });

  await test.step("3. a tab survives a reload, because it is in the address", async () => {
    await page.reload();
    await expect(page.getByTestId("harnesses-table")).toBeVisible({ timeout: 30_000 });
  });

  await test.step("4. the header creates each of the three resources", async () => {
    await page.getByRole("tab", { name: "Agents" }).click();
    await expect(page.getByTestId("agents-new")).toBeVisible();
    await expect(page.getByTestId("agents-new-template")).toBeVisible();
    await expect(page.getByTestId("agents-new-harness")).toBeVisible();
  });
});

/**
 * A pressed row looks different from a hovered one.
 *
 * There was a rule for this and it did nothing: it set the pressed background to the
 * border token, which is the same colour antd already uses for the row hover — measured
 * at rgb(50, 44, 61) for both. So pressing a row looked exactly like pointing at it,
 * and on a slow route a click still looked like it had not registered, which is the
 * whole thing the rule was added for.
 *
 * Compared rather than asserted against a value: the point is that the two differ, and
 * pinning either to a literal would make this a test of the palette instead.
 */
test("agents: pressing a row looks different from hovering it", async ({ page }) => {
  await loadPage(page, routes.agents, { title: "Agents" });
  const row = page.locator("tbody tr.clickable-table-row").first();
  await expect(row).toBeVisible({ timeout: 30_000 });

  const cell = row.locator("td").first();

  await row.hover();
  // Settled, so this really is the hover colour. Read in the same tick it would be
  // the at-rest colour, and the assertion below would be comparing the pressed state
  // against the wrong thing entirely — which is what it was doing.
  const hovered = (await settledPaint(cell)).background;

  const box = await row.boundingBox();
  await page.mouse.move(box!.x + 30, box!.y + 10);
  await page.mouse.down();
  try {
    // Polled: the cell transitions over 200ms, so the first read after `mouse.down`
    // is still the colour being left. See `helpers/style` for the measurements.
    await expect
      .poll(() => background(cell), { message: "a press must not look like a hover" })
      .not.toBe(hovered);
  } finally {
    // Released even on a failure, so a held button cannot affect later tests.
    await page.mouse.up();
  }
});

/** Ordered by name by default, not by the order namespaces answered in. */
test("agents: the list is ordered by name", async ({ page }) => {
  await loadPage(page, routes.agents, { title: "Agents" });
  await expect(dataRows(page)).toHaveCount(9);
  const names = (await page.locator('[data-testid^="agent-link-"]').allTextContents()).map((name) => name.trim());
  expect(names).toEqual([...names].sort((left, right) => left.localeCompare(right)));
});

/**
 * Agents — the error journey.
 *
 * Two failure modes worth pinning: that the page says so rather than going blank,
 * and that it does not quietly report "there are no agents" when the truth is that
 * it could not find out.
 *
 * AgentService lists one namespace at a time. A failed namespace discovery must
 * report its error because the Agent read cannot run without those namespaces.
 */

test("agents: a failed load is reported, not disguised as an empty list", async ({
  page,
}) => {
  await test.step("1. the failure is on screen and names what went wrong", async () => {
    await loadPage(page, routes.agents, { scenario: "error", title: "Agents" });

    const alert = page.getByTestId("agents-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load agents");
    // The backend's own account of the failure reaches the reader rather than a
    // generic message, and it names the call that failed. Asserted as that property
    // rather than as a literal status: a gRPC error is an HTTP 200, so there is no
    // status to report and putting one back to satisfy a string match would be
    // fitting the product to a stale test.
    await expect(alert).toContainText("asked to fail");
    // The call that actually failed, not the one the page is *about*. Reporting
    // "could not list templates" when the namespaces read is what broke sends the
    // reader to the wrong service.
    await expect(alert).toContainText("SystemService/ListNamespaces");
  });

  await test.step("2. it is not mistaken for an empty list", async () => {
    // Every wording the empty state can take, because reporting a failed read as
    // "there are none" is the specific bug this step exists for.
    await expect(page.getByText(/No agents/)).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("3. no stale data is left on screen from before the failure", async () => {
    await expect(rowNamed(page, agents.k8s.template)).toHaveCount(0);
  });

  await test.step("4. retrying asks the backend again", async () => {
    // Counted as an operation: a retry issues no HTTP request under the substituted
    // transport, so counting requests would report "no retry happened" for a retry
    // that demonstrably did.
    //
    // The namespace call, because that is the one that failed and the one the
    // template read is waiting on. Counting `ListAgentTemplates` would assert that a
    // retry re-ran a read which never ran in the first place, and would fail for a
    // retry that worked.
    const before = await operationCalls(page, rpc.listNamespaces);

    await page
      .getByTestId("agents-error")
      .getByRole("button", { name: "Try again" })
      .click();
    await expect
      .poll(() => operationCalls(page, rpc.listNamespaces), { timeout: 10_000 })
      .toBeGreaterThan(before);
    // Still failing, so the message stays put rather than flickering away.
    await expect(page.getByTestId("agents-error")).toBeVisible();
  });

  await test.step("5. a refresh over a failed read says so, rather than claiming success", async () => {
    /*
     * The half of the refresh confirmation that is worth a test. A refresh usually
     * returns the same data, so a successful one is indistinguishable from a button that
     * did nothing — hence the message. But the first version of that message reported
     * success unconditionally, because SWR captures a failed revalidation into its error
     * state and resolves anyway. The toast then sat above a page showing a load error,
     * saying the opposite of it, and only a real click revealed that.
     *
     * Narrowed to one namespace, which is what makes the *agent* read happen at all:
     * with none chosen the page fans out over the namespace list, and in this scenario
     * that list is the read that fails first — so the page would be reporting a
     * namespace failure rather than the refresh failure under test. `ns` is the filter's
     * own parameter, the one `useListView` reads.
     */
    await page.goto(withScenario(`${routes.agents}?ns=kagent`, "error"));
    await expect(page.getByTestId("agents-error")).toBeVisible();

    await clickRefresh(page);
    await expect(page.getByText(/Could not refresh Agents/)).toBeVisible();
    await expect(page.getByText("Agents refreshed")).toHaveCount(0);
  });

  await test.step("6. the page recovers once the backend does, and says so on a refresh", async () => {
    await loadPage(page, routes.agents, { scenario: "ok", title: "Agents" });
    await expect(page.getByTestId("agents-error")).toHaveCount(0);
    await expect(rowNamed(page, agents.k8s.template)).toHaveCount(1);

    await clickRefresh(page);
    await expect(page.getByText("Agents refreshed")).toBeVisible();
  });
});

/**
 * An agent's own page, when its reads fail.
 *
 * Its two reads answer different questions — what this agent *is*, and what has been
 * said to it — and either can fail alone. Reporting one as the other is what this
 * covers: an agent whose conversations could not be read is not an agent with no
 * conversations.
 */
test("agents: an agent whose conversations cannot be read says so, and is not empty", async ({
  page,
}) => {
  await test.step("1. the failure names the read that failed", async () => {
    await loadPage(page, agentPage(agents.k8s), { scenario: "error" });

    const alert = page.getByTestId("conversations-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("AgentInstanceService/ListAgentInstances");
  });

  await test.step("2. and it is not reported as an agent nobody has talked to", async () => {
    // The distinction the whole page turns on: "no conversations yet" invites
    // starting one, and would be a lie about an agent with forty.
    await expect(page.getByText(/No conversations with this agent yet/)).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("3. an address for an agent whose template is gone says which half is missing", async () => {
    // A real state rather than a 404: a template can be deleted while the
    // conversations cut from it keep running, because an instance runs from the
    // prepared revision it was built against.
    await loadPage(page, agentPage({ template: "was-deleted", harness: "k8s-agent" }), {
      scenario: "ok",
    });
    const missing = page.getByTestId("agent-template-missing");
    await expect(missing).toBeVisible();
    await expect(missing).toContainText("keep running");
  });
});
