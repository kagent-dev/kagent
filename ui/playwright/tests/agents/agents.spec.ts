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

/**
 * Agents — what can be run, and what each one is.
 *
 * An agent is an `AgentTemplate` paired with a `Harness`. An `AgentInstance` is one
 * *conversation* with an agent, not an agent — the A2A gateway files every task
 * under the instance as the task's `contextId`, so an instance holds a single thread
 * of turns. This page used to list those, under a heading that said "Agents".
 *
 * ## What this covers, and why each thing is here
 *
 * The properties a page cannot show you it got wrong:
 *
 * Each row is an explicit Agent. Two Agents can reuse a template while retaining
 * independent revisions and conversations. Templates alone do not appear here.
 */

test("agents: the list is agents, and an agent is a template paired with a harness", async ({
  page,
}) => {
  await test.step("1. every namespace by default, with no toggle to say so", async () => {
    await loadPage(page, routes.agents, { title: "Agents" });
    await expectSettled(page);

    // Five agents across two namespaces, which a page scoped to one could not show.
    // The old "all namespaces" switch is gone: nothing selected *is* everything, so
    // there is one control rather than two that could contradict each other.
    //
    // Six rows, because the fixtures also hold conversations whose pair no longer
    // exists and those are gathered under a stand-in row — see the dedicated test
    // below for why that row is there and when it is not.
    await expect(dataRows(page)).toHaveCount(6);
    await expect(page.getByTestId("agents-summary")).toHaveText("5 of 5 agents");
    await expect(page.getByTestId("agent-conversations-__unmapped__")).toHaveText("1 conversation");
    await expect(page.getByTestId("agents-table")).toContainText("analytics");
    await expect(page.getByTestId("instances-all-namespaces")).toHaveCount(0);
  });

  await test.step("2. two explicit Agents reuse a template with different Harnesses", async () => {
    const shared = rowNamed(page, "shared-brain");
    await expect(shared).toHaveCount(2);

    const harnesses = await shared
      .locator("[data-testid^='agent-harness-']")
      .allInnerTexts();
    expect(harnesses.sort()).toEqual(["fast-lane", "k8s-agent"]);
  });

  await test.step("3. each row links to its own agent, not to a shared one", async () => {
    // Two links to two addresses. A page keyed on the template would produce the same
    // href twice and the two rows would be the same page.
    //
    // The destination is a conversation that does not exist yet, which is what clicking
    // an agent's name is for. It creates nothing until a message is sent; the agent's
    // own page — `agentPage` — is reached from the Conversations control instead.
    await expect(
      page.getByTestId("agent-link-kagent-shared-brain-k8s-agent"),
    ).toHaveAttribute("href", agentNewChat(agents.sharedOnK8s));
    await expect(
      page.getByTestId("agent-link-kagent-shared-brain-fast-fast-lane"),
    ).toHaveAttribute("href", agentNewChat(agents.sharedOnFastLane));
  });

  await test.step("4. a template without an Agent is not listed as an agent", async () => {
    await expect(rowNamed(page, "note-taker")).toHaveCount(0);
  });

  await test.step("5. the revision state is three answers, and 'preparing' is not a failure", async () => {
    await expect(
      page.getByTestId(`agent-revision-kagent/${agents.k8s.name}`),
    ).toHaveAttribute("data-revision-state", "ready");

    // Defined, with a desired revision and none successful yet, because its harness
    // has not reported ready. A page that rendered this the same as a failure would
    // send a reader looking for a broken template.
    const preparing = page.getByTestId(
      `agent-revision-kagent/${agents.preparing.name}`,
    );
    await expect(preparing).toHaveAttribute("data-revision-state", "preparing");
    await expect(preparing).toHaveText("Preparing");
  });

  await test.step("6. each agent carries a count of the conversations people have had with it", async () => {
    // Counted with `all_creators`, so it is what the agent is doing rather than what
    // this reader has done with it. Four for the k8s agent: two of the caller's own
    // and two of somebody else's.
    await expect(
      page.getByTestId(
        `agent-conversations-kagent/${agents.k8s.name}`,
      ),
    ).toHaveText("4 conversations");
    // One each for the two agents `shared-brain` is — which is the count being per
    // *pair* rather than per template. Split the other way it would read 2 and 0.
    await expect(
      page.getByTestId(
        `agent-conversations-kagent/${agents.sharedOnK8s.name}`,
      ),
    ).toHaveText("1 conversation");
    await expect(
      page.getByTestId(
        `agent-conversations-kagent/${agents.sharedOnFastLane.name}`,
      ),
    ).toHaveText("1 conversation");
  });

  await test.step("7. a conversation belonging to no pair is said out loud, not dropped", async () => {
    // The fixture instance with no harness and no template belongs to no agent, so
    // it appears under none of them — which would otherwise be a conversation that
    // silently vanished from the product. Deleting a template produces the same
    // state on a cluster, and the conversation keeps running.
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
    // Five agents plus the stand-in row for conversations that belong to none of them.
    await expect(dataRows(page)).toHaveCount(6);
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
    // The unknown-Agent fixture remains visible, but conversations belonging to
    // known Agents in other namespaces must not be counted as orphaned.
    await expect(dataRows(page)).toHaveCount(2);
    await expect(page.getByTestId("agent-conversations-__unmapped__")).toHaveText("1 conversation");
    // In the address, so a narrowed view can be linked to and survives a reload.
    await expect(page).toHaveURL(/ns=analytics/);
  });

  await test.step("3. the pill removes exactly that filter", async () => {
    await page.getByTestId("agents-filters-pill-ns-analytics").click();
    await expect(dataRows(page)).toHaveCount(6);
    await expect(page).not.toHaveURL(/ns=analytics/);
  });

  await test.step("4. searching covers every row, not just the page on screen", async () => {
    await page.getByTestId("agents-filters-search").fill("fast-lane");
    // Matched on the harness, which is half of what an agent *is* — a search over
    // template names alone could never find one of two agents cut from one template.
    await expect(dataRows(page)).toHaveCount(1);
    await expect(page.getByTestId("agents-summary")).toHaveText("1 of 5 agents");

    // The search term is a filter like any other, so it has a pill and "clear
    // filters" means it.
    await expect(page.getByTestId("agents-filters-pill-search")).toBeVisible();
    await page.getByTestId("agents-filters-pill-clear").click();
    // Back to five agents and the stand-in row.
    await expect(dataRows(page)).toHaveCount(6);
  });
});

test("agents: conversations with no agent are gathered rather than only counted", async ({
  page,
}) => {
  /*
   * Deleting a template does not stop the conversations cut from it: an instance runs
   * from the prepared revision it was built against, and the collector keeps that
   * revision *for it*. So a conversation outlives its agent — and until now the list
   * counted those in a sentence and offered nowhere to go, which left them running,
   * holding a worker each, and reachable from nothing.
   */
  await loadPage(page, routes.agents, { title: "Agents" });
  await expectSettled(page);

  await test.step("1. the notice says where they went, not just that they exist", async () => {
    const notice = page.getByTestId("agents-orphaned-conversations");
    await expect(notice).toBeVisible();
    await expect(notice).toContainText("unmapped-agentinstances");
  });

  await test.step("2. and there is a row for them, last, because it is not an agent", async () => {
    /*
     * It used to be pinned first, as the exception. Last is better: it is a stand-in for
     * conversations whose pair no longer exists, not something anybody came here to
     * find, and putting it above every real agent made the list open on the one row
     * most readers were not looking for.
     */
    await expect(dataRows(page).last()).toContainText("unmapped-agentinstances");
  });

  await test.step("3. it opens the conversations rather than offering a new one", async () => {
    // There is no agent to start a conversation with — that is the condition the row
    // describes — so the name goes to the list of what it stands for.
    await page.getByRole("link", { name: "unmapped-agentinstances", exact: true }).click();
    await page.waitForURL(/\/agents\/unmapped$/, { timeout: 30_000 });
    await expect(page.getByTestId("unmapped-table")).toBeVisible();
    // This fixture has no Agent reference; the page must report that absence.
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
    await expect(concepts).toContainText("each referenced or inline");
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

  await test.step("4. there is no way to create an agent, because there is no such thing", async () => {
    await page.getByRole("tab", { name: "Agents" }).click();
    await expect(page.getByTestId("agents-new")).toHaveCount(0);
    // What it offers instead is the thing that actually makes one.
    await expect(page.getByTestId("agents-new-template")).toBeVisible();
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

/**
 * The list has an order of its own, and the stand-in row is not part of it.
 *
 * It arrived in whatever order the namespaces were read in — stable within a read and
 * meaningless to a reader, so an agent moved when an unrelated namespace answered more
 * slowly. And `Unmapped conversations` is not an agent: it stands in for conversations
 * whose pair no longer exists, so sorting it among real agents by name would drop it
 * into the middle of the list on a "U".
 */
test("agents: the list is ordered by name, with the stand-in row last", async ({ page }) => {
  await loadPage(page, routes.agents, { title: "Agents" });
  await expect(page.locator("tbody tr").first()).toBeVisible({ timeout: 30_000 });

  const names = await page
    .locator('tbody tr [data-testid="agent-name"], tbody tr td:first-child')
    .allTextContents();
  const cleaned = names.map((name) => name.trim()).filter(Boolean);
  const stranded = cleaned.findIndex((name) => name.includes("Unmapped"));

  if (stranded !== -1) {
    expect(stranded, "the stand-in row belongs at the bottom").toBe(cleaned.length - 1);
  }

  const real = stranded === -1 ? cleaned : cleaned.slice(0, stranded);
  const sorted = [...real].sort((left, right) => left.localeCompare(right));
  expect(real, "agents should be listed by name").toEqual(sorted);
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
    await expect(alert).toContainText("SessionService/ListSessions");
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
