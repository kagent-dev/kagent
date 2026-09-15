import type { Locator, Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import {
  agentChat,
  agentDetail,
  agentPage,
  agents,
  instances,
  SIBLING_OF_READY,
  loadPage,
  withScenario,
} from "../../helpers/app";
import { dialog, pressOnce, pressUntil } from "../../helpers/resource";

/**
 * The agent rail — the navigation for when you are inside one agent.
 *
 * Narrowed to a single agent: which agent you are in, the things you can do to it, and
 * every conversation you have had with it. The last of those is the sibling instances
 * of the same `(Harness, AgentTemplate)` pair, because an `AgentInstance` *is* one
 * conversation — so a second conversation with an agent is a second instance of the
 * same pair, and "New chat" creates rather than navigates.
 *
 * What is beside the conversation rather than in the rail — its record, and the agent
 * panel — is `panels.spec.ts`. What the rail *does* to a conversation is owned by
 * `agents/agent-page.spec.ts`; this file keeps only what that table cannot say.
 */

/**
 * Picks one item from a rail row's action menu, and proves it took.
 *
 * Two clicks, either of which the dropdown's animation can swallow: the one that opens
 * the menu, and the one that picks from it. Both fail silently — the menu never opens,
 * or it opens and the item does nothing — and the report is whatever the *next* step was
 * waiting for, which is nowhere near the cause.
 *
 * So the caller says what the item is supposed to open, and this retries until that is on
 * screen. The same press-until-it-takes shape `pressUntil` gives a dialog button; it is
 * separate only because two controls are involved rather than one.
 */
async function chooseFromRowMenu(
  page: Page,
  menu: Locator,
  item: string,
  opened: Locator,
): Promise<void> {
  const entry = page.getByRole("menuitem", { name: item });
  await expect(async () => {
    if (!(await opened.isVisible())) {
      if (!(await entry.isVisible())) await menu.click({ force: true });
      await entry.click();
    }
    await expect(opened).toBeVisible();
  }).toPass({ timeout: 30_000 });
}

/** Unnamed, with a first message — so the rail has something to derive a title from. */
const AUTO_TITLED = "2b6e0c45-8a71-4f39-9d02-3c85f1a7e6d0";

const AGENT_CHAT = agentChat(instances.ready);
const AGENT_DETAILS = agentDetail(instances.ready);

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
test("chat agent rail: the current agent is absent from the switcher on a surface with no conversation", async ({
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

test("chat agent rail: the identity card switches agent", async ({ page }) => {
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
test("chat agent rail: stays clear of the header when the page scrolls", async ({ page }) => {
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
test("chat agent rail: the conversation list scrolls without taking the rest with it", async ({
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

test("chat agent rail: it can be got out of the way, and stays that way", async ({ page }) => {
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
test("chat agent rail: conversations are named and show their state", async ({ page }) => {
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

  await test.step("3. and the derived name is a title, not the message", async () => {
    /*
     * The specific fixture rather than the property above, because "some row is not
     * Untitled" is satisfied by a row that simply has a name somebody typed. This one
     * has no name and something said in it, which is the case the derivation exists
     * for.
     *
     * This lived in `agents/agent-page.spec.ts` while driving the rail — the same
     * claim, made twice and weakly in the place that owns the surface.
     */
    await page.goto(agentChat(AUTO_TITLED));
    await expect(page.getByTestId("chat-panel")).toBeVisible({ timeout: 30_000 });

    const row = page.getByTestId(`chat-session-${AUTO_TITLED}`);
    await expect(row).toContainText("Summarise last night's deploy");
    // Cut at a word boundary with an ellipsis, which is what says it is a summary
    // rather than the text itself.
    await expect(row).toContainText("…");
    // And emphatically not the id, which is what an unnamed conversation falls back to
    // when there is nothing said in it to derive from.
    await expect(row).not.toContainText("Untitled");
  });
});

test("chat agent rail: several conversations can be picked and deleted together", async ({
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
    // The confirmation's own test id rather than the modal class: it was reaching for
    // `.ant-modal` while this id sat unused beside it.
    const confirm = page.getByTestId("chat-bulk-confirm");
    /*
     * Pressed until the confirmation is up, rather than clicked once behind a fixed
     * 400ms sleep. The sleep was a guess at how long the dropdown takes to animate, and
     * under a loaded Firefox run it is sometimes wrong — which showed up as a menu item
     * that did nothing.
     *
     * The proof is the visible modal rather than `confirm`: that test id sits on antd's
     * `ant-modal-root`, which is in the DOM whether the dialog is up or not and reports
     * hidden either way. It is still the right locator to assert the *text* on, which is
     * why both are here.
     */
    await chooseFromRowMenu(
      page,
      page.getByTestId("chat-bulk-menu"),
      "Delete all selected",
      dialog(page),
    );
    // One question for the set, naming how many — not one per conversation, which is
    // the thing that makes clearing a rail unbearable.
    await expect(confirm).toContainText("Delete 2 conversations?");
    await expect(confirm).toContainText("can be recovered");
    // And why it is worth doing on a cluster that keeps running out of workers.
    await expect(confirm).toContainText("workers they hold");
    await pressUntil(confirm.getByRole("button", { name: "Delete" }), () =>
      expect(dialog(page)).toHaveCount(0),
    );

    await expect(rows).toHaveCount(before - 2, { timeout: 20_000 });
    await expect(confirm).toHaveCount(0);
  });
});

test("chat agent rail: the newest conversation is at the top", async ({ page }) => {
  /*
   * The rail rendered in whatever order `ListAgentInstances` answered in, which is an
   * order in no particular order — so a conversation started a minute ago could sit
   * anywhere in the list.
   *
   * By when it was *started*, not when it was last spoken in. The latter is the more
   * useful ordering and is not available: `AgentInstance.updatedAt` is written when the
   * instance is created and when it goes `CREATING` -> `READY`, and never when a message
   * is sent, so ordering by it would be creation order wearing a better name. The
   * comparator carries the note, and `conversationOrder.test.ts` carries the cases.
   *
   * The fixtures are arranged against this deliberately: unsorted, this rail renders
   * the *older* suspended sibling first, so a rail that sorts has to move it and a
   * rail that does not cannot accidentally pass. Asserted on ids rather than titles,
   * because a title is derived and a row's identity is not.
   */
  await page.goto(agentChat(instances.ready));
  const rail = page.getByTestId("chat-sessions");
  const rows = rail.locator('a[data-testid^="chat-session-"]');
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });

  await expect(rows.first()).toHaveAttribute(
    "data-testid",
    `chat-session-${instances.ready}`,
  );
  await expect(rows.nth(1)).toHaveAttribute(
    "data-testid",
    `chat-session-${instances.suspended}`,
  );
});

test("chat agent rail: only the page you are on is marked as current", async ({ page }) => {
  /*
   * A conversation row used to be lit by id alone, so it claimed to be the current
   * page on every surface that mounts the rail for an instance — the agent's own
   * details page included, which is a different page from the chat the row links to.
   * Two rows then carried `aria-current="page"` at once, which is both wrong on its
   * face and wrong for a screen reader.
   *
   * Asserted from the details page rather than the chat, because the chat is the one
   * place the old behaviour happened to be right.
   */
  await page.goto(AGENT_DETAILS);

  const rail = page.getByTestId("chat-sessions");
  await expect(rail).toBeVisible({ timeout: 30_000 });

  const row = rail.locator(`a[data-testid="chat-session-${instances.ready}"]`);

  await test.step("1. the conversation is listed, but is not the current page", async () => {
    await expect(row).toBeVisible();
    await expect(row).toHaveAttribute("data-active", "false");
    await expect(row).not.toHaveAttribute("aria-current", "page");
  });

  await test.step("2. exactly one entry in the rail claims to be current", async () => {
    // The count is the assertion. Any single row being right is not enough when the
    // defect was two of them being right at the same time.
    await expect(page.locator('[data-testid="chat-sessions-nav"] [aria-current="page"]')).toHaveCount(0);
    await expect(page.locator('[data-testid="chat-sessions"] [aria-current="page"]')).toHaveCount(0);
  });

  await test.step("3. opening the conversation is what makes it current", async () => {
    await row.click();
    await expect(page).toHaveURL(new RegExp(`${instances.ready}/chat$`));
    await expect(row).toHaveAttribute("data-active", "true");
    await expect(row).toHaveAttribute("aria-current", "page");
  });
});

/**
 * Renaming and deleting a conversation from the rail — the surface, not the operation.
 *
 * Both operations are owned by `agents/agent-page.spec.ts`, which asserts them
 * against the agent's conversations table: what a name may be, what a rename does to the
 * list, what deleting costs. None of that is repeated here.
 *
 * What *is* here is the one thing the table cannot say — that you can do either without
 * leaving the conversation you are reading. Renaming used to live only in that table, so
 * changing the one field on a record its reader owns meant leaving, finding it in a list,
 * renaming it and coming back; and the delete only existed where a caller passed a
 * handler, which meant the chat page and nowhere else, so the same row behaved
 * differently depending on which surface had mounted the rail.
 *
 * Deliberately a named sibling rather than `.first()`. That was an unstated dependency on
 * render order: once the rail sorted newest-first it became the *open* conversation, and
 * deleting the one you are looking at navigates away — so the rail went with it and the
 * test failed counting rows on a page it had left.
 */
test("chat agent rail: a conversation is renamed and deleted, without leaving it", async ({
  page,
}) => {
  await page.goto(AGENT_CHAT);
  const rail = page.getByTestId("chat-sessions");
  // The row links themselves. Several controls share the `chat-session-` prefix — the
  // menu, the checkbox, the confirmation — so a prefix match counts each row several
  // times.
  const rows = rail.locator('a[data-testid^="chat-session-"]');
  // Counted after the list has arrived: counting during the read gives zero, and a later
  // assertion of "one fewer" then expects minus one.
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });
  const before = await rows.count();

  const menu = rail.locator(`[data-testid="chat-session-menu-${SIBLING_OF_READY}"]`);
  const sibling = rail.locator(`a[data-testid="chat-session-${SIBLING_OF_READY}"]`);

  await test.step("1. the row's menu renames it, and the rail says so without a reload", async () => {
    // Drawn on every row, not revealed on hover: these actions are most of the reason to
    // open the rail on a conversation you are not in.
    await expect(menu).toBeVisible();
    const rename = page.getByTestId("conversation-rename-input");
    await chooseFromRowMenu(page, menu, "Rename chat", rename);

    const field = rename.locator("input");
    await field.fill("Named from the rail");
    /*
     * Pressed once the modal has stopped arriving, rather than clicked.
     *
     * A click computed while antd is still zooming a modal in lands where the button no
     * longer is, and the box then stays open with the new name typed into it — which
     * surfaces thirty seconds later as a rename that did not work, nowhere near the
     * click that never happened. `pressOnce` rather than `pressUntil` because a second
     * click would go out with this modal gone and the conversation list under it.
     */
    await pressOnce(page.getByRole("button", { name: "Save" }));

    // The rail row's own text rather than the toast: a success message says the app
    // believes it worked, and a rename that failed would still show one.
    await expect(sibling).toContainText("Named from the rail", { timeout: 30_000 });
  });

  await test.step("2. the details modal renames the open conversation, and the rail agrees", async () => {
    /*
     * The second entry point, and the direction that was broken.
     *
     * The chat page reads the open conversation on its own and the rail beside it reads
     * the list, so a rename that refreshed the read it was started from left the other
     * showing the old name. Renaming from the modal refreshed the modal and not the
     * rail; renaming from the rail refreshed the rail and not the modal. Both were right
     * about their own read and both looked broken.
     */
    /*
     * Both clicks pressed until they take, like the row menu above.
     *
     * This step opens a modal and then a dialog inside it, and either click can land
     * while the thing it is aimed at is still animating and be dropped. That is not a
     * theory: this test walks four animated controls in a row and every one of them has
     * failed that way at least once under a loaded Firefox run.
     */
    const fields = page.getByTestId("conversation-details-fields");
    await pressUntil(page.getByTestId("chat-details"), () => expect(fields).toBeVisible());

    const rename = page.getByTestId("conversation-rename-input");
    await pressUntil(page.getByTestId("conversation-details-rename"), () =>
      expect(rename).toBeVisible(),
    );
    const field = rename.locator("input");
    // Pre-filled here, because this conversation *has* a name: the box opens on the
    // stored one so an edit is an edit rather than a retype. The rail's box opens empty
    // for the unnamed sibling in step 1, which is the other half of the same rule.
    await expect(field).toHaveValue("Tuesday cluster review");
    await field.fill("Named from the details");
    // Once, for the reason step 1 gives: this modal is still arriving too.
    await pressOnce(page.getByRole("button", { name: "Save" }));

    // Asserted through the modal rather than after closing it, because the rail is
    // behind it the whole time and this is the moment the old value would still be on
    // screen.
    await expect(
      rail.locator(`a[data-testid="chat-session-${instances.ready}"]`),
    ).toContainText("Named from the details", { timeout: 30_000 });
    // And step 1's rename survived it, so neither read clobbers the other.
    await expect(sibling).toContainText("Named from the rail");

    // Pressed until it takes, like every other dialog button here: an Escape sent while
    // the save behind it is still settling is swallowed, and the modal then blocks the
    // row menu the next step aims at.
    await pressUntil(page.getByRole("button", { name: "Close" }), () =>
      expect(fields).toBeHidden(),
    );
  });

  await test.step("3. deleting asks first, naming what cannot be recovered", async () => {
    const confirm = dialog(page);
    await chooseFromRowMenu(page, menu, "Delete chat", confirm);

    // The menu makes deleting deliberate; it does not make it recoverable. A
    // conversation is gone with its whole transcript and there is no undo.
    await expect(confirm).toContainText("cannot be recovered");
    await pressUntil(confirm.getByRole("button", { name: "Keep" }), () =>
      expect(dialog(page)).toHaveCount(0),
    );
    await expect(rows).toHaveCount(before);
  });

  await test.step("4. and confirming removes exactly one, leaving the page put", async () => {
    await chooseFromRowMenu(page, menu, "Delete chat", dialog(page));
    await pressUntil(
      dialog(page).getByRole("button", { name: "Delete" }),
      () => expect(rows).toHaveCount(before - 1),
    );
    await expect(page.getByTestId("chat-sessions-error")).toHaveCount(0);
    // The conversation being read is not the one that went, so the page stays.
    await expect(page).toHaveURL(AGENT_CHAT);
  });

  await test.step("5. and the same control is there off the chat page", async () => {
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
 * The rail's own read, failing on its own.
 *
 * Moved here from the chat error spec, where it sat among failing *turns*. It is not
 * one: the conversation and the list of the agent's other conversations fail
 * independently — `?chat=…` drives the turn, `?mock=…` drives the API — and this is
 * the list. It belongs beside the rail that shows it.
 */
test("chat agent rail: a failed read says so, and is not an empty list", async ({
  page,
}) => {
  await test.step("1. the list says it failed", async () => {
    // The API scenario, not the chat one: the conversation itself and the list of the
    // agent's other conversations fail independently, and this is the list.
    await loadPage(page, AGENT_CHAT, { scenario: "error" });

    const error = page.getByTestId("chat-sessions-error");
    await expect(error).toBeVisible();
    await expect(error).toContainText("Could not load conversations");
  });

  await test.step("2. it is not mistaken for having no conversations", async () => {
    await expect(page.getByTestId("chat-sessions-empty")).toHaveCount(0);
  });

  await test.step("3. it recovers", async () => {
    await page.goto(withScenario(AGENT_CHAT, "ok"));
    await expect(page.getByTestId("chat-sessions-error")).toHaveCount(0);
    // This conversation is in its own rail, marked as the one that is open.
    await expect(
      page.getByTestId(`chat-session-${instances.ready}`),
    ).toBeVisible();
  });
});
