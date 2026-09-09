import { test, expect } from "../../fixtures/test";
import { agentChat, instances } from "../../helpers/app";

/** The menu that is actually on screen: antd leaves a closed dropdown mounted. */
const openMenu = (page: import("@playwright/test").Page) =>
  page.locator(".ant-dropdown:not(.ant-dropdown-hidden)");

const dividers = (page: import("@playwright/test").Page) =>
  page.locator('[data-testid^="chat-checkpoint-"][role="separator"]');

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
