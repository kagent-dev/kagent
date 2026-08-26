import { test, expect } from "../../fixtures/test";
import {
  agentNewChat,
  agents,
  dataRows,
  expectSettled,
  loadPage,
  rowNamed,
  routes,
} from "../../helpers/app";

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
 * - **A row is a pair.** One template admitted by two harnesses is two agents, and
 *   the fixtures carry exactly that case. A page keyed on the template would render
 *   one row, look entirely correct, and merge two agents' conversations.
 * - **A template nothing admits is no agent.** It reaches no prepared revision and
 *   every `CreateAgentInstance` naming it is refused, so listing it as a runnable
 *   agent would be listing something that cannot run.
 * - **Selecting no namespace means all of them**, which is one state rather than two
 *   controls that could disagree — the toggle-beside-a-single-select this replaced.
 * - **The revision state is three answers, not two.** "Preparing" is not a failure
 *   and "not reported" is not one either.
 *
 * ## Why the assertions read the state and not the wording
 *
 * The revision tag carries its state in `data-revision-state` beside its label. The
 * wording is a product decision and may change; the state is derived from the
 * controller's status and may not. Asserting the value means a rename is a
 * deliberate edit here rather than a broken suite, while a row showing the wrong
 * state still fails.
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
    await expect(page.getByTestId("agents-table")).toContainText("analytics");
    await expect(page.getByTestId("instances-all-namespaces")).toHaveCount(0);
  });

  await test.step("2. a template two harnesses admit is two agents, told apart by the harness", async () => {
    // The load-bearing assertion of this whole page. `shared-brain` carries one
    // label each of two harnesses selects on, so the controller materialises two
    // pairs with two revisions — and they share a name, which is exactly why the
    // harness is a column rather than a detail.
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
      page.getByTestId("agent-link-kagent-shared-brain-fast-lane"),
    ).toHaveAttribute("href", agentNewChat(agents.sharedOnFastLane));
  });

  await test.step("4. a template no harness admits is not listed as an agent", async () => {
    // `note-taker` has no labels, so nothing admits it, so it reaches no prepared
    // revision and cannot be run. It is a template, and the templates page is where
    // that is said — listing it here would offer a "New chat" that cannot succeed.
    await expect(rowNamed(page, "note-taker")).toHaveCount(0);
  });

  await test.step("5. the revision state is three answers, and 'preparing' is not a failure", async () => {
    await expect(
      page.getByTestId(`agent-revision-kagent/${agents.k8s.template}/${agents.k8s.harness}`),
    ).toHaveAttribute("data-revision-state", "ready");

    // Admitted, with a desired revision and none successful yet, because its harness
    // has not reported ready. A page that rendered this the same as a failure would
    // send a reader looking for a broken template.
    const preparing = page.getByTestId(
      `agent-revision-kagent/${agents.preparing.template}/${agents.preparing.harness}`,
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
        `agent-conversations-kagent/${agents.k8s.template}/${agents.k8s.harness}`,
      ),
    ).toHaveText("4 conversations");
    // One each for the two agents `shared-brain` is — which is the count being per
    // *pair* rather than per template. Split the other way it would read 2 and 0.
    await expect(
      page.getByTestId(
        `agent-conversations-kagent/${agents.sharedOnK8s.template}/${agents.sharedOnK8s.harness}`,
      ),
    ).toHaveText("1 conversation");
    await expect(
      page.getByTestId(
        `agent-conversations-kagent/${agents.sharedOnFastLane.template}/${agents.sharedOnFastLane.harness}`,
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
    await page.locator('.ant-select-item-option[title="analytics"]').click();
    await page.keyboard.press("Escape");

    await expect(page.getByTestId("agents-filters-pill-ns-analytics")).toBeVisible();
    // One agent in that namespace, plus the stand-in row — which survives every filter
    // deliberately: it belongs to no namespace in the sense the filter means, and
    // hiding it because a namespace was chosen would take away the only way to reach
    // the conversations it stands for.
    await expect(dataRows(page)).toHaveCount(2);
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
    await page.getByTestId("agent-link-kagent-unmapped-agentinstances-—").click();
    await page.waitForURL(/\/agents\/unmapped$/, { timeout: 30_000 });
    await expect(page.getByTestId("unmapped-table")).toBeVisible();
    // Each row says which pair it *was* built from, which is the only clue to why it
    // is here — a reader recognising a template they deleted has their answer.
    await expect(page.getByTestId("unmapped-table")).toContainText("on");
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
    // The model is not guessable from the nouns: "Agents" reads like a list of things
    // somebody made, and there is no Agent CRD at all.
    const concepts = page.getByTestId("agent-concepts");
    await expect(concepts).toBeVisible();
    await expect(concepts).toContainText("AgentTemplate");
    await expect(concepts).toContainText("Harness");
    await expect(concepts, "the derived one has to say that it is").toContainText("derived");
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

  const background = () =>
    row.locator("td").first().evaluate((cell) => getComputedStyle(cell).backgroundColor);

  await row.hover();
  const hovered = await background();

  const box = await row.boundingBox();
  await page.mouse.move(box!.x + 30, box!.y + 10);
  await page.mouse.down();
  const pressed = await background();
  await page.mouse.up();

  expect(pressed, "a press must not look like a hover").not.toBe(hovered);
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
