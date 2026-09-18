import { test, expect } from "../../fixtures/test";
import { agentChat, instances } from "../../helpers/app";

/** The menu that is actually on screen: antd leaves a closed dropdown mounted. */
const openMenu = (page: import("@playwright/test").Page) =>
  page.locator(".ant-dropdown:not(.ant-dropdown-hidden)");

const dividers = (page: import("@playwright/test").Page) =>
  page.locator('[data-testid^="chat-checkpoint-mark-"]');

/** The seeded boundary's generated name: its conversation, joined to its turn. */
const SEEDED_NAME = `${instances.ready}-seed-task-1`;

/** Opens one boundary's record, which is what pressing its line does. */
async function openSnapshot(
  page: import("@playwright/test").Page,
  divider: import("@playwright/test").Locator,
) {
  await divider.locator('[data-testid^="chat-checkpoint-open-"]').click();
  await expect(page.getByTestId("snapshot-details-body")).toBeVisible();
}

test("chat: the snapshot is taken from the composer, and marks where a fork would cut", async ({
  page,
}) => {
  await page.goto(agentChat(instances.ready));
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine.first()).toBeVisible({ timeout: 30_000 });

  await test.step("1. the seeded boundary is a line under the turn it was taken at", async () => {
    await expect(dividers(page)).toHaveCount(1);
    await expect(page.getByTestId("chat-checkpoint-label")).toBeVisible();
    // Both halves of that turn are above the line: what a fork carries is the
    // exchange, not the question on its own.
    await expect(
      page.locator('[data-testid="chat-message"][data-checkpointed="true"]'),
    ).toHaveCount(4);
  });

  await test.step("2. no message carries a control of its own", async () => {
    await mine.first().hover();
    await expect(page.locator('[data-testid^="chat-message-checkpoint-"]')).toHaveCount(0);
    await expect(page.locator('[data-testid^="chat-message-menu-"]')).toHaveCount(0);
  });

  await test.step("3. the composer refuses a second snapshot at the same boundary", async () => {
    await expect(page.getByTestId("chat-checkpoint")).toBeDisabled();
  });

  await test.step("4. and offers one as soon as there is a newer turn", async () => {
    await page.getByTestId("chat-input").fill("Another question, so the boundary moves.");
    await page.getByTestId("chat-send").click();
    await expect(mine).toHaveCount(2, { timeout: 30_000 });

    await expect(page.getByTestId("chat-checkpoint")).toBeEnabled();
    await page.getByTestId("chat-checkpoint").click();
    await expect(dividers(page)).toHaveCount(2);
    await expect(page.getByTestId("chat-checkpoint")).toBeDisabled();
  });
});

/*
 * What the mark carries, and that pressing it opens the record.
 *
 * Worth one spec of its own because these are two different affordances on one
 * element: the row is a button, and the three controls under it are not part of it. A
 * row that silently stopped opening anything would leave the record unreachable while
 * every other assertion in this file still passed.
 */
test("chat: the mark names itself, carries its controls, and opens its record", async ({
  page,
}) => {
  await page.goto(agentChat(instances.ready));
  await expect(dividers(page)).toHaveCount(1, { timeout: 30_000 });

  const line = dividers(page).first();
  const id = ((await line.getAttribute("data-testid")) ?? "").replace(
    "chat-checkpoint-mark-",
    "",
  );

  await test.step("the mark names itself, with the generated name nobody has changed", async () => {
    await expect(line.getByTestId("chat-checkpoint-label")).toHaveText(
      `Snapshot \u201C${SEEDED_NAME}\u201D`,
    );
    await expect(line.getByTestId(`chat-checkpoint-subtitle-${id}`)).toHaveText(
      `\u201C${SEEDED_NAME}\u201D`,
    );
  });

  await test.step("and carries the three things a reader does to it", async () => {
    await expect(line.getByTestId(`chat-checkpoint-fork-${id}`)).toBeEnabled();
    await expect(line.getByTestId(`chat-checkpoint-rename-${id}`)).toBeEnabled();
    await expect(line.getByTestId(`chat-checkpoint-delete-${id}`)).toBeEnabled();
  });

  await test.step("pressing the row opens that boundary's own record", async () => {
    await openSnapshot(page, line);
    await expect(page.getByTestId("snapshot-details-fields")).toContainText(id);
    await expect(page.getByTestId("snapshot-details-state")).toHaveAttribute(
      "data-state",
      "ready",
    );
    // Ready, so the fork is offered rather than explained away.
    await expect(page.getByTestId("snapshot-details-fork")).toBeEnabled();
  });

  await test.step("and the mark's own delete asks first, then takes it", async () => {
    await page.keyboard.press("Escape");
    await expect(page.getByTestId("snapshot-details-body")).toHaveCount(0);

    await line.getByTestId(`chat-checkpoint-delete-${id}`).click();
    await expect(page.getByText("Delete this snapshot?")).toBeVisible();
    await page.getByTestId(`chat-checkpoint-delete-cancel-${id}`).click();
    await expect(dividers(page)).toHaveCount(1);

    await line.getByTestId(`chat-checkpoint-delete-${id}`).click();
    await page.getByTestId(`chat-checkpoint-delete-confirm-${id}`).click();
    await expect(dividers(page)).toHaveCount(0);
  });
});

/*
 * What a fork carries, what it must not, and what it is called.
 *
 * Three claims about one action, so one action proves all three. Forking the *seeded*
 * boundary once a second one exists leaves the later turn behind — which is the whole
 * point of forking a boundary rather than a conversation — the copy arrives with no
 * lines on it at all, and it is titled after the snapshot rather than after its
 * source. That last one is the controller's job now: the page passes no name, so a
 * fork called anything else means the name never reached `ForkAgentInstance`.
 *
 * The second claim is subtler than it looks. A boundary saved on this page is
 * remembered against the message it was taken at, because the reader's newest message
 * has no turn id yet. A fork is handed copies of its source's messages under the same
 * ids — so without dropping that memory when the conversation changes, a fork opened
 * from here drew a line it does not have.
 */
test("chat: a fork holds only what was above its line, takes the snapshot's name, and inherits none of its marks", async ({
  page,
}) => {
  const SNAPSHOT_NAME = "Before the second question";

  await page.goto(agentChat(instances.ready));
  const rows = page.getByTestId("chat-sessions").locator('a[data-testid^="chat-session-"]');
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });
  const before = await rows.count();
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine).toHaveCount(1, { timeout: 30_000 });

  // A second turn and a second boundary, so the seeded one is no longer the latest and
  // forking it has something to leave behind — and so this page is holding a locally
  // saved mark for the fork to fail to inherit.
  await page.getByTestId("chat-input").fill("A second turn, after the saved boundary.");
  await page.getByTestId("chat-send").click();
  await expect(mine).toHaveCount(2, { timeout: 30_000 });
  await page.getByTestId("chat-checkpoint").click();
  await expect(dividers(page)).toHaveCount(2);

  await test.step("naming it from the mark, which is what the fork will be called", async () => {
    await dividers(page).first().locator('[data-testid^="chat-checkpoint-rename-"]').click();
    await page.getByTestId("snapshot-rename-input").locator("input").fill(SNAPSHOT_NAME);
    // Exact, because an accessible name matches on substring: "Save" alone would
    // also find any control whose label merely starts with it.
    await page.getByRole("button", { name: "Save", exact: true }).click();
    // The mark itself, re-read: the name is on the snapshot before anything forks it.
    await expect(
      dividers(page).first().locator('[data-testid^="chat-checkpoint-subtitle-"]'),
    ).toHaveText(`\u201C${SNAPSHOT_NAME}\u201D`);
  });

  await dividers(page).first().locator('[data-testid^="chat-checkpoint-fork-"]').click();

  await expect(page).toHaveURL(/\/agents\/[0-9a-f-]{36}\/chat$/);
  await expect(page).not.toHaveURL(new RegExp(`/agents/${instances.ready}/chat$`), {
    timeout: 30_000,
  });
  await expect(rows).toHaveCount(before + 1);
  await expect(page.getByTestId("chat-sessions")).toContainText(SNAPSHOT_NAME);
  // Only what was above the line: the second turn is below it.
  await expect(mine).toHaveCount(1, { timeout: 30_000 });
  // And none of the source page's marks came with it.
  await expect(dividers(page)).toHaveCount(0);
});

/*
 * Deleting a boundary, and the reload that proves it.
 *
 * The line on screen is drawn from two sources — the controller's list and what this
 * page has saved since it loaded — so a delete that dropped only the first would leave
 * the line up until a reload, and one that dropped only the second would bring it back
 * on the next read. The reload here is what tells those two apart.
 */
test("chat: a snapshot is deleted from its record, and stays deleted", async ({ page }) => {
  await page.goto(agentChat(instances.ready));
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine.first()).toBeVisible({ timeout: 30_000 });

  // A second boundary, so the delete has to remove one line rather than all of them.
  await page.getByTestId("chat-input").fill("A second turn, to save a second boundary at.");
  await page.getByTestId("chat-send").click();
  await expect(mine).toHaveCount(2, { timeout: 30_000 });
  await page.getByTestId("chat-checkpoint").click();
  await expect(dividers(page)).toHaveCount(2);

  const doomed = await dividers(page).first().getAttribute("data-testid");
  const id = (doomed ?? "").replace("chat-checkpoint-mark-", "");

  await test.step("asks before deleting, and cancelling leaves both", async () => {
    await openSnapshot(page, page.getByTestId(`chat-checkpoint-mark-${id}`));
    await page.getByTestId("snapshot-details-delete").click();
    await expect(page.getByText("Delete this snapshot?")).toBeVisible();
    await page.getByTestId("snapshot-details-delete-cancel").click();
    await expect(page.getByText("Delete this snapshot?")).toBeHidden();
    await expect(dividers(page)).toHaveCount(2);
  });

  await test.step("confirming takes that line, closes the record and leaves the other", async () => {
    await page.getByTestId("snapshot-details-delete").click();
    await page.getByTestId("snapshot-details-delete-confirm").click();
    await expect(page.getByTestId(`chat-checkpoint-mark-${id}`)).toHaveCount(0);
    await expect(page.getByTestId("snapshot-details-body")).toHaveCount(0);
    await expect(dividers(page)).toHaveCount(1);
  });

  await test.step("and the reload agrees: the boundary is gone from the backend", async () => {
    await page.reload();
    await expect(mine).toHaveCount(2, { timeout: 30_000 });
    await expect(dividers(page)).toHaveCount(1);
    await expect(page.getByTestId(`chat-checkpoint-mark-${id}`)).toHaveCount(0);
  });
});

test("chat: a conversation is duplicated from the rail menu, and the copy opens", async ({
  page,
}) => {
  await page.goto(agentChat(instances.ready));
  const rows = page.getByTestId("chat-sessions").locator('a[data-testid^="chat-session-"]');
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });
  const before = await rows.count();

  const row = page.getByTestId("chat-sessions").locator("li", {
    has: page.getByTestId(`chat-session-${instances.ready}`),
  });
  await row.hover();
  await row.getByTestId(`chat-session-menu-${instances.ready}`).click();
  await openMenu(page).getByRole("menuitem", { name: "Duplicate chat" }).click();

  await expect(page).not.toHaveURL(new RegExp(`/agents/${instances.ready}/chat$`), {
    timeout: 30_000,
  });
  await expect(rows).toHaveCount(before + 1);
  await expect(page.getByTestId("chat-sessions")).toContainText("(copy)");
});

/*
 * Delete's red follows the theme, and comes back.
 *
 * Narrower than the bug that prompted it, deliberately. That bug was a *lag*: Delete
 * took its red from antd's generated danger palette, which is rebuilt from the config
 * on a theme change and landed a frame after the Emotion-styled buttons beside it, so
 * mid-switch it still wore the theme just left. Catching that needs an assertion
 * pinned to a particular frame, which is the flake this suite refuses to buy — polled,
 * this passes against the old code too.
 *
 * What it does hold is the weaker claim underneath: that this button is themed at all,
 * and in both directions. A Delete hard-coded to one red fails here.
 */
test("chat: the mark's controls follow a theme change", async ({ page }) => {
  await page.goto(agentChat(instances.ready));
  await expect(dividers(page)).toHaveCount(1, { timeout: 30_000 });

  const del = dividers(page).first().locator('[data-testid^="chat-checkpoint-delete-"]');
  const red = () => del.evaluate((node) => getComputedStyle(node).borderTopColor);

  const before = await red();
  await page.getByTestId("theme-toggle").click();
  await expect
    .poll(red, { message: "Delete should take the other theme's red" })
    .not.toBe(before);

  await page.getByTestId("theme-toggle").click();
  await expect
    .poll(red, { message: "and come back to the one it started on" })
    .toBe(before);
});
