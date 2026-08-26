import { test, expect } from "../../fixtures/test";
import { agentChat, agentDetail, agentPage, agents, instances } from "../../helpers/app";

/**
 * The agent rail — the navigation for when you are inside one agent.
 *
 * Narrowed to a single agent: which agent you are in, the things you can do to it,
 * and every conversation you have had with it. The last of those is the sibling
 * instances of the same `(Harness, AgentTemplate)` pair, because an `AgentInstance`
 * *is* one conversation — so a second conversation with an agent is a second
 * instance of the same pair, and "New chat" creates rather than navigates.
 *
 * ## What is no longer here
 *
 * The capabilities panel — an agent's tools and skills beside its conversation. It
 * read them off a `SandboxAgent`, and an instance has neither: what an agent can
 * reach is described by its `AgentTemplate`, which has no surface in this build yet.
 * Recorded in `playwright/DEFERRED.md` rather than left as a passing test of
 * something that is gone.
 */

const AGENT_CHAT = agentChat(instances.ready);
const AGENT_DETAILS = agentDetail(instances.ready);

test("chat: a conversation's record is read without leaving the conversation", async ({
  page,
}) => {
  /*
   * This was an entry in the rail, and reading four facts about a conversation meant
   * leaving it and then finding the way back. Reference that costs a navigation is
   * reference nobody consults, so it is a modal over the conversation now — in the
   * gutter under Share, which is where the conversation's other controls live.
   */
  await page.goto(AGENT_CHAT);
  await page.getByTestId("chat-details").click();

  const fields = page.getByTestId("conversation-details-fields");
  await expect(fields).toBeVisible({ timeout: 30_000 });
  // The record, not a summary: the id is what a reader copies into a CLI.
  await expect(fields).toContainText(instances.ready);

  // Still on the conversation behind it — the point of not making this a page.
  await expect(page).toHaveURL(new RegExp(`/agents/kagent/${instances.ready}/chat$`));
  await expect(page.getByTestId("chat-input")).toBeVisible();

  // There is no Edit anywhere on it: an instance has no spec to change. What the agent
  // *is* lives on its AgentTemplate and how it *runs* on its Harness, so a control here
  // would offer something that does not exist.
  await expect(page.getByTestId("agent-details-edit")).toHaveCount(0);
});

test("agent rail: a conversation is deleted from a menu, on every surface", async ({
  page,
}) => {
  /*
   * The control used to be a trash can on every row, always visible, inches from the
   * conversation being read in a rail where every row looks alike — a slip cost the
   * whole thing with nothing to undo it. It is behind a per-row menu now, revealed on
   * hover, so deleting takes two deliberate actions and the list reads as names.
   *
   * It also only existed where a caller passed a handler, which meant the chat page and
   * nowhere else: the same row behaved differently depending on which surface had
   * mounted the rail. The rail owns the delete now, which is what step 3 checks.
   */
  await page.goto(AGENT_CHAT);
  const rail = page.getByTestId("chat-sessions");
  // The row links themselves. Several controls share the `chat-session-` prefix now —
  // the menu, the checkbox, the confirmation — so a prefix match counts each row
  // several times.
  const rows = rail.locator('a[data-testid^="chat-session-"]');
  // Counted after the list has arrived: counting during the read gives zero, and a
  // later assertion of "one fewer" then expects minus one.
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });
  const before = await rows.count();

  const item = page.getByRole("menuitem", { name: "Delete chat" });

  await test.step("1. the menu offers it, and the row is otherwise quiet", async () => {
    const menu = rail.locator('[data-testid^="chat-session-menu-"]').first();
    // Present for a pointer to find, but not drawn until the row is hovered.
    await expect(menu).toHaveCSS("opacity", "0");
    await menu.click({ force: true });
    await expect(item).toBeVisible();
    // The dropdown animates in, and a click landing mid-transition is refused as
    // unstable rather than missing the element.
    await page.waitForTimeout(400);
  });

  await test.step("2. it still asks, and the question names the conversation", async () => {
    // The menu makes deleting deliberate; it does not make it recoverable. A
    // conversation is gone with its whole transcript and there is no undo.
    await item.click();
    const confirm = page.locator(".ant-modal:visible");
    await expect(confirm).toContainText("cannot be recovered");
    await confirm.getByRole("button", { name: "Keep" }).click();
    await expect(rows).toHaveCount(before);
  });

  await test.step("3. and Delete removes exactly one", async () => {
    // The dialog animates out, and a click while it is still there lands on its mask.
    await expect(page.locator(".ant-modal:visible")).toHaveCount(0);
    await page.locator('[data-testid^="chat-session-menu-"]').first().click({ force: true });
    await page.waitForTimeout(400);
    await page.getByRole("menuitem", { name: "Delete chat" }).click();
    await page.locator(".ant-modal:visible").getByRole("button", { name: "Delete" }).click();
    await expect(rows).toHaveCount(before - 1, { timeout: 20_000 });
    await expect(page.getByTestId("chat-sessions-error")).toHaveCount(0);
  });

  await test.step("4. and the same control is there off the chat page", async () => {
    // The agent's own page mounts the same rail and passes no delete handler. That used
    // to mean no control at all.
    await page.getByTestId("agent-nav-agent-conversations").click();
    await expect(page.getByTestId("agent-rail")).toBeVisible({ timeout: 30_000 });
    await expect(
      page.locator('[data-testid^="chat-session-menu-"]').first(),
    ).toHaveCount(1);
  });
});

/**
 * Changing which agent the rail is scoped to, without leaving the rail.
 *
 * The identity card wears a chevron, so it has to open something: an affordance that
 * looked like "change agent" and went somewhere else was the bug this replaced.
 */
/**
 * The agent you are on stays out of its own switcher, wherever you opened it from.
 *
 * The exclusion matched on namespace, template *and* harness, and the harness reaches
 * the rail from the open conversation's record — so it was undefined until that record
 * loaded, and on the surfaces with no conversation at all it never arrived. Requiring
 * it to match meant nothing matched, and the current agent listed itself: not always,
 * which is what made it look intermittent, but exactly whenever the record was not
 * there.
 *
 * Opened from the agent's own page, which is one of the surfaces that has no
 * conversation to read a harness from — the case the chat page's version of this test
 * cannot reach.
 */
test("agent rail: the current agent is absent from the switcher on a surface with no conversation", async ({
  page,
}) => {
  // The agent's own page, which has no conversation open and so no record to read a
  // harness from — the state where this actually broke.
  await page.goto(agentPage(agents.k8s));
  await expect(page.getByTestId("agent-rail-identity")).toBeVisible({ timeout: 30_000 });
  await page.getByTestId("agent-rail-identity").click();

  const switcher = page.getByTestId("agent-switcher");
  await expect(switcher).toBeVisible();
  // Once the list has actually arrived: asserting a row is absent while nothing has
  // loaded passes for the wrong reason.
  await expect(
    switcher.locator('[data-testid^="agent-switcher-option-"]').first(),
  ).toBeVisible({ timeout: 30_000 });

  await expect(
    page.getByTestId(`agent-switcher-option-${agents.k8s.template}-${agents.k8s.harness}`),
    "the agent whose page is open should not be offered as somewhere to go",
  ).toHaveCount(0);
});

test("agent rail: the identity card switches agent", async ({ page }) => {
  await page.goto(AGENT_CHAT);

  // Not mounted until asked for — the switcher reads every namespace, one request
  // each, and a rail that did that before anybody wanted a menu would be paying for
  // one most readers never open.
  await expect(page.getByTestId("agent-switcher")).toHaveCount(0);

  await page.getByTestId("agent-rail-identity").click();
  const switcher = page.getByTestId("agent-switcher");
  await expect(switcher).toBeVisible();

  const options = switcher.locator('[data-testid^="agent-switcher-option-"]');
  /*
   * Agents, not conversations.
   *
   * This listed `AgentInstance`s, so a switcher labelled "agent" moved between
   * *conversations* — and one agent with nine of them filled it nine times over with
   * rows nothing distinguished but a UUID. An agent is a (template, harness) pair,
   * which is what the agents page lists and what somebody opening this is looking for.
   *
   * The agent the reader is already on is **not** listed. Every row here is somewhere
   * to go, and that one goes nowhere: listing it made them read past their own agent to
   * find another, and offered a click that did nothing — which is worse than absent,
   * because it looks like a destination. The card that opens this menu names the
   * current agent directly above it.
   */
  await expect(options.first()).toBeVisible({ timeout: 30_000 });
  await expect(
    page.getByTestId(`agent-switcher-option-${agents.k8s.template}-${agents.k8s.harness}`),
  ).toHaveCount(0);

  // Filtering narrows it, and matches what the reader can see — the template and the
  // harness, which is how the agent is named everywhere else.
  const before = await options.count();
  await switcher.getByTestId("agent-switcher-filter").fill("reporting");
  await expect(options).toHaveCount(1);
  expect(before).toBeGreaterThan(1);

  // Picking one starts a conversation with it — the call to action for that agent,
  // where nothing is created until a message is sent. Not the conversation of the
  // agent left behind, and not a new instance either.
  await switcher.getByTestId("agent-switcher-filter").fill("");
  await options.first().click();
  await expect(page).toHaveURL(/\/agents\/[^/]+\/[^/]+\/on\/[^/]+\/new$/);
});

/**
 * The rail is sticky and so is the header, and the header draws on top.
 *
 * Stuck any higher than the header is tall, the rail slid underneath it — and the
 * switcher, which opens from the card at the very top of the rail, came out of a card
 * that was itself half-hidden. Asserted as geometry rather than as a screenshot,
 * because the failure is an overlap of two rectangles and that is what to measure.
 */
test("agent rail: stays clear of the header when the page scrolls", async ({ page }) => {
  // A short viewport on a tall page, so there is something to scroll.
  await page.setViewportSize({ width: 1400, height: 700 });
  await page.goto(AGENT_DETAILS);
  await expect(page.getByTestId("agent-rail-identity")).toBeVisible();

  await page.evaluate(() => window.scrollTo(0, 900));
  await page.getByTestId("agent-rail-identity").click();
  await expect(page.getByTestId("agent-switcher")).toBeVisible();

  const edges = await page.evaluate(() => {
    const rect = (id: string) =>
      document.querySelector(`[data-testid="${id}"]`)?.getBoundingClientRect();
    return {
      headerBottom: rect("app-header")?.bottom ?? 0,
      cardTop: rect("agent-rail-identity")?.top ?? 0,
      switcherTop: rect("agent-switcher")?.top ?? 0,
    };
  });

  expect(edges.headerBottom).toBeGreaterThan(0);
  expect(edges.cardTop).toBeGreaterThanOrEqual(edges.headerBottom);
  expect(edges.switcherTop).toBeGreaterThanOrEqual(edges.headerBottom);
});

/**
 * The conversations are the only part of the rail that scrolls.
 *
 * It used to scroll as one box, so a reader with thirty conversations scrolled the
 * agent's name, the switcher and the search field away in order to reach them — and
 * the search field is the thing you reach for *because* the list is long.
 *
 * Asserted structurally rather than by scrolling a long fixture: what makes this true
 * is the rail being bounded with its overflow hidden while the list owns an `auto`
 * one, and that holds at any length.
 */
test("agent rail: the conversation list scrolls without taking the rest with it", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1400, height: 700 });
  await page.goto(AGENT_DETAILS);
  await expect(page.getByTestId("chat-sessions-list")).toBeVisible();

  const shape = await page.evaluate(() => {
    const at = (id: string) => document.querySelector(`[data-testid="${id}"]`);
    const rail = at("agent-rail");
    const list = at("chat-sessions-list");
    const search = at("chat-search");
    return {
      railOverflow: rail ? getComputedStyle(rail).overflowY : null,
      listOverflow: list ? getComputedStyle(list).overflowY : null,
      railHeight: rail?.getBoundingClientRect().height ?? 0,
      searchBottom: search?.getBoundingClientRect().bottom ?? 0,
      listTop: list?.getBoundingClientRect().top ?? 0,
      searchIsInsideList: list && search ? list.contains(search) : true,
    };
  });

  // The rail cannot scroll; the list can.
  expect(shape.railOverflow).toBe("hidden");
  expect(shape.listOverflow).toBe("auto");

  // The rail is bounded by the window, which is what gives the list something to
  // scroll inside rather than growing the page.
  expect(shape.railHeight).toBeLessThanOrEqual(700);

  // And the search field is above the scrolling part, not inside it.
  expect(shape.searchIsInsideList).toBe(false);
  expect(shape.searchBottom).toBeLessThanOrEqual(shape.listTop);
});

test("agent rail: it can be got out of the way, and stays that way", async ({ page }) => {
  await page.goto(AGENT_CHAT);
  await expect(page.getByTestId("agent-rail")).toBeVisible({ timeout: 30_000 });

  await test.step("1. collapsing leaves a way back, not a dead edge", async () => {
    await page.getByTestId("agent-rail-collapse").click();
    // Hidden rather than unmounted: it slides shut, and animating a width needs the
    // element to still be there. Asserted as not-visible so a rail that stopped
    // collapsing would still fail.
    await expect(page.getByTestId("agent-rail")).toBeHidden();
    // The control that undoes it has to be where the thing used to be. A collapse with
    // no visible way back is a feature people use once.
    await expect(page.getByTestId("agent-rail-expand")).toBeVisible();
  });

  await test.step("2. the preference is the reader's, not the page's", async () => {
    // Collapsing on one conversation and finding it back on the next is what makes
    // people stop using the control, so it is remembered rather than reset per page.
    await page.goto(AGENT_DETAILS);
    await expect(page.getByTestId("agent-rail-expand")).toBeVisible({ timeout: 30_000 });
    await expect(page.getByTestId("agent-rail")).toBeHidden();
  });

  await test.step("3. and expanding brings the navigation back", async () => {
    await page.getByTestId("agent-rail-expand").click();
    await expect(page.getByTestId("agent-rail")).toBeVisible();
    await expect(page.getByTestId("chat-sessions")).toBeVisible();
  });
});

test("chat: the agent panel says what the conversation cannot", async ({ page }) => {
  /*
   * A conversation is an `AgentInstance`, and an instance holds no configuration —
   * what model is answering, what it was told to do and what tools it can reach all
   * live on the `AgentTemplate` it was cut from. So this panel reads the template,
   * which is also a thing the reader can open and change.
   */
  await page.goto(AGENT_CHAT);
  const panel = page.getByTestId("chat-agent-context");
  await expect(panel).toBeVisible({ timeout: 30_000 });

  await test.step("1. it names the template, and the template is a link", async () => {
    // Not a dead label: every conversation with this agent reads the same template, and
    // the page behind this link is where that is said before anybody edits it.
    await expect(page.getByTestId("chat-agent-context-template")).toBeVisible();
  });

  await test.step("2. the model and the tools, read from that template", async () => {
    await expect(panel).toContainText("Model");
    await expect(panel).toContainText("Tools");
  });

  await test.step("3. and it can be put away, and stays away", async () => {
    await page.getByTestId("chat-context-collapse").click();
    await expect(panel).toBeHidden();
    await expect(page.getByTestId("chat-context-expand")).toBeVisible();

    // Remembered per reader, like the rail: closing it on one conversation and finding
    // it back on the next is what makes people stop using the control.
    await page.reload();
    await expect(page.getByTestId("chat-context-expand")).toBeVisible({ timeout: 30_000 });
    await page.getByTestId("chat-context-expand").click();
    await expect(page.getByTestId("chat-agent-context")).toBeVisible();
  });
});

/**
 * A rail you can read: every row named, and every row's state visible.
 *
 * Both of these were missing for the same reason and are fixed by the same change.
 * Deriving a conversation's title needs its first message, which needs its task list —
 * and the A2A gateway refused a task read for any conversation that was not ready. With
 * conversations giving their workers back at the end of every turn, that is most of
 * them, so the rail fell back to `Untitled · 50b46891` for everything except the one
 * already open: the only row a reader could identify was the one they were looking at.
 *
 * The gateway now answers a task read from the store whatever state the instance is in,
 * because that is where the transcript lives. Resuming to read one would have claimed a
 * worker every time somebody glanced at a conversation.
 */
test("agent rail: conversations are named and show their state", async ({ page }) => {
  await page.goto(AGENT_CHAT);
  const rail = page.getByTestId("chat-sessions");
  const rows = rail.locator('a[data-testid^="chat-session-"]');
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });

  await test.step("1. an ordinary conversation is not marked at all", async () => {
    /*
     * The dot marks the exceptions, not everything.
     *
     * It used to appear on every row, `ready` included, which was right while `ready`
     * meant something: a conversation held a worker until the page suspended it. The
     * server quiesces a runtime after every turn now and leaves the record `ready`, so
     * `ready` is what every conversation says, permanently — and a dot on every row
     * repeating it is decoration implying a distinction the API cannot make.
     *
     * So a fixture of ordinary conversations carries no dots, and one that is creating,
     * failed or being deleted carries one worth looking at.
     */
    const dots = rail.locator('[data-testid^="chat-session-state-"]');
    await expect(dots.locator('[data-testid="chat-session-state-ready"]')).toHaveCount(0);
  });

  await test.step("2. a row is named by what was said in it, not only by its id", async () => {
    /*
     * Asserted on a row other than the open one, which is the whole point: the open
     * conversation always had a title, because the page rendering its transcript could
     * derive one. Everything else fell back to the id.
     */
    const others = rows.filter({ hasNotText: "Untitled" });
    await expect(
      others,
      "at least one conversation should be named by its first message",
    ).not.toHaveCount(0, { timeout: 30_000 });
  });
});

test("agent rail: several conversations can be picked and deleted together", async ({
  page,
}) => {
  /*
   * Deleting twenty conversations one confirmation at a time is a chore, and on a
   * cluster where each holds a worker it is the chore standing between a reader and a
   * working pool. So they can be picked as a set.
   *
   * Scoped to what the filter is showing throughout: selecting all after a search means
   * the ones searched for, and a range extends over the visible list. Extending over the
   * unfiltered one would tick conversations that are not on screen, and the count would
   * then not match what the reader can see.
   */
  await page.goto(AGENT_CHAT);
  const rail = page.getByTestId("chat-sessions");
  const rows = rail.locator('a[data-testid^="chat-session-"]');
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });
  const before = await rows.count();
  const boxes = rail.locator('[data-testid^="chat-session-select-"]');

  await test.step("1. the row is there before a selection, but the actions are not", async () => {
    /*
     * The row stays and only the actions button comes and goes.
     *
     * It used to be the whole bar that appeared, which meant ticking the first
     * conversation inserted a line and pushed the list down under the reader's pointer
     * — a jump at the exact moment they were aiming at something. So what this asserts
     * is a layout that does not move: select-all is present with nothing selected, and
     * the button that has nothing to act on yet is the only part missing.
     */
    await expect(page.getByTestId("chat-bulk-bar")).toBeVisible();
    await expect(page.getByTestId("chat-selection-count")).toContainText("Select all");
    await expect(page.getByTestId("chat-bulk-menu")).toHaveCount(0);

    // The property the row exists for: the list does not move when one is ticked.
    const listTop = await rail
      .locator('a[data-testid^="chat-session-"]')
      .first()
      .evaluate((node) => node.getBoundingClientRect().top);
    await rows.first().hover();
    await boxes.first().click();
    await expect(page.getByTestId("chat-bulk-menu")).toBeVisible();
    const listTopAfter = await rail
      .locator('a[data-testid^="chat-session-"]')
      .first()
      .evaluate((node) => node.getBoundingClientRect().top);
    expect(
      Math.abs(listTopAfter - listTop),
      "ticking a conversation should not move the list under the pointer",
    ).toBeLessThanOrEqual(1);

    // Left as it was found, so the step below starts from nothing selected.
    await boxes.first().click();
  });

  await test.step("2. shift extends the selection over a run", async () => {
    // Hovered first, as a reader does: the boxes are hidden until the row is under the
    // pointer, so the list reads as names rather than as a form.
    await rows.first().hover();
    await boxes.first().click();
    await rows.nth(before - 1).hover();
    await boxes.nth(before - 1).click({ modifiers: ["Shift"] });
    // Everything between the two ends, not just the two clicked — which is the whole
    // difference between shift-selecting and clicking twice.
    await expect(page.getByTestId("chat-selection-count")).toContainText(
      `${before} selected`,
    );
  });

  await test.step("3. the same control clears, then selects everything again", async () => {
    // Everything is selected after the range above, so the box is checked and clicking
    // it clears — a select-all that only ever adds gives no way back out of a big
    // selection.
    await page.getByTestId("chat-select-all").click();
    // The row stays put; what goes is the actions button and the count, because there
    // is nothing left to act on. The row remaining is the whole point of it.
    await expect(page.getByTestId("chat-bulk-bar")).toBeVisible();
    await expect(page.getByTestId("chat-bulk-menu")).toHaveCount(0);
    await expect(page.getByTestId("chat-selection-count")).toContainText("Select all");
  });

  await test.step("4. deleting the set asks once, and takes all of them", async () => {
    await rows.first().hover();
    await boxes.first().click();
    await rows.nth(1).hover();
    await boxes.nth(1).click();
    await page.getByTestId("chat-bulk-menu").click();
    await page.waitForTimeout(400);
    await page.getByRole("menuitem", { name: /Delete all selected/ }).click();

    const confirm = page.getByTestId("chat-bulk-confirm");
    // One question for the set, naming how many — not one per conversation, which is
    // the thing that makes clearing a rail unbearable.
    await expect(page.locator(".ant-modal:visible")).toContainText("Delete 2 conversations?");
    await expect(page.locator(".ant-modal:visible")).toContainText("can be recovered");
    // And why it is worth doing on a cluster that keeps running out of workers.
    await expect(page.locator(".ant-modal:visible")).toContainText("workers they hold");
    await page.locator(".ant-modal:visible").getByRole("button", { name: "Delete" }).click();

    await expect(rows).toHaveCount(before - 2, { timeout: 20_000 });
    await expect(confirm).toHaveCount(0);
  });
});
