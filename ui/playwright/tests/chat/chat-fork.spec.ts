import { test, expect } from "../../fixtures/test";
import { agentChat, instances } from "../../helpers/app";

/** The menu that is actually on screen: antd leaves a closed dropdown mounted. */
const openMenu = (page: import("@playwright/test").Page) =>
  page.locator(".ant-dropdown:not(.ant-dropdown-hidden)");

const dividers = (page: import("@playwright/test").Page) =>
  page.locator('[data-testid^="chat-checkpoint-mark-"]');

test("chat: the checkpoint is taken from the composer, and marks where a fork would cut", async ({
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

  await test.step("3. the composer refuses a second checkpoint at the same boundary", async () => {
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

test("chat: a fork of an earlier checkpoint opens holding only what was above its line", async ({
  page,
}) => {
  await page.goto(agentChat(instances.ready));
  const rows = page.getByTestId("chat-sessions").locator('a[data-testid^="chat-session-"]');
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });
  const before = await rows.count();
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine).toHaveCount(1, { timeout: 30_000 });

  // A second turn, so the seeded boundary is no longer the conversation's latest and
  // forking it has something to leave behind.
  await page.getByTestId("chat-input").fill("A second turn, after the saved boundary.");
  await page.getByTestId("chat-send").click();
  await expect(mine).toHaveCount(2, { timeout: 30_000 });

  await dividers(page).first().locator('[data-testid^="chat-checkpoint-fork-"]').click();

  await expect(page).toHaveURL(/\/agents\/[0-9a-f-]{36}\/chat$/);
  await expect(page).not.toHaveURL(new RegExp(`/agents/${instances.ready}/chat$`), {
    timeout: 30_000,
  });
  await expect(rows).toHaveCount(before + 1);
  await expect(page.getByTestId("chat-sessions")).toContainText("(fork)");
  // The whole point of forking a boundary rather than the conversation: the second
  // turn is below the line, so it is not in the copy.
  await expect(mine).toHaveCount(1, { timeout: 30_000 });
});

/*
 * The marks a fork must not inherit.
 *
 * A boundary saved on this page is remembered against the message it was taken at,
 * because the reader's newest message has no turn id yet. A fork is handed copies of
 * its source's messages under the same ids — so without dropping that memory when the
 * conversation changes, a fork opened from here drew a line it does not have.
 */
test("chat: a fork does not inherit the marks of the page it was made from", async ({
  page,
}) => {
  await page.goto(agentChat(instances.ready));
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine.first()).toBeVisible({ timeout: 30_000 });

  await page.getByTestId("chat-input").fill("A turn to save a boundary at.");
  await page.getByTestId("chat-send").click();
  await expect(mine).toHaveCount(2, { timeout: 30_000 });
  await page.getByTestId("chat-checkpoint").click();
  await expect(dividers(page)).toHaveCount(2);

  await dividers(page).last().locator('[data-testid^="chat-checkpoint-fork-"]').click();
  await expect(page).not.toHaveURL(new RegExp(`/agents/${instances.ready}/chat$`), {
    timeout: 30_000,
  });

  await expect(mine.first()).toBeVisible({ timeout: 30_000 });
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
test("chat: a checkpoint is deleted from the chat, and stays deleted", async ({ page }) => {
  await page.goto(agentChat(instances.ready));
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine.first()).toBeVisible({ timeout: 30_000 });

  // A second boundary, so the delete has to remove one line rather than all of them.
  await page.getByTestId("chat-input").fill("A second turn, to save a second boundary at.");
  await page.getByTestId("chat-send").click();
  await expect(mine).toHaveCount(2, { timeout: 30_000 });
  await page.getByTestId("chat-checkpoint").click();
  await expect(dividers(page)).toHaveCount(2);

  // The seeded line, not the new one: the newest sits against the composer, where the
  // "Checkpoint saved" toast covers it.
  const doomed = await dividers(page).first().getAttribute("data-testid");
  const id = (doomed ?? "").replace("chat-checkpoint-mark-", "");

  await test.step("asks before deleting, and cancelling leaves both", async () => {
    await page.getByTestId(`chat-checkpoint-delete-${id}`).click();
    await expect(page.getByText("Delete this checkpoint?")).toBeVisible();
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(dividers(page)).toHaveCount(2);
  });

  await test.step("confirming takes that line and leaves the other", async () => {
    await page.getByTestId(`chat-checkpoint-delete-${id}`).click();
    await page.getByTestId(`chat-checkpoint-delete-confirm-${id}`).click();
    await expect(page.getByTestId(`chat-checkpoint-mark-${id}`)).toHaveCount(0);
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
