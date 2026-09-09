import { test, expect } from "../../fixtures/test";
import { agentChat, instances } from "../../helpers/app";

/** The menu that is actually on screen: antd leaves a closed dropdown mounted. */
const openMenu = (page: import("@playwright/test").Page) =>
  page.locator(".ant-dropdown:not(.ant-dropdown-hidden)");

test("chat: fork is offered on the reader's messages, enabled on the latest only", async ({
  page,
}) => {
  await page.goto(agentChat(instances.ready));
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine.first()).toBeVisible({ timeout: 30_000 });

  await test.step("1. not on the agent's messages", async () => {
    await expect(
      page
        .locator('[data-testid="chat-message"]:not([data-role="user"])')
        .locator('[data-testid^="chat-message-menu-"]'),
    ).toHaveCount(0);
  });

  await test.step("2. enabled on the reader's latest message", async () => {
    await expect(mine).toHaveCount(1);
    await mine.first().hover();
    await mine.first().locator('[data-testid^="chat-message-menu-"]').click();
    const item = openMenu(page).getByRole("menuitem", { name: "Fork chat" });
    await expect(item).toBeVisible();
    await expect(item).not.toHaveClass(/ant-dropdown-menu-item-disabled/);
    await page.keyboard.press("Escape");
  });

  await test.step("3. disabled on an earlier one, once a newer message exists", async () => {
    // A second message of the reader's own, so the first is no longer the latest.
    await page.getByTestId("chat-input").fill("Another question, so the first is not last.");
    await page.getByTestId("chat-send").click();
    await expect(mine).toHaveCount(2, { timeout: 30_000 });

    await mine.first().hover();
    await mine.first().locator('[data-testid^="chat-message-menu-"]').click();
    const earlier = openMenu(page).getByRole("menuitem", { name: "Fork chat" });
    await expect(earlier).toBeVisible();
    // A checkpoint is taken at the latest turn boundary, so an earlier anchor cannot
    // be honoured. It is shown rather than hidden, and refuses.
    await expect(earlier).toHaveClass(/ant-dropdown-menu-item-disabled/);
  });
});

test("chat: a conversation is forked from its latest message, and the fork opens", async ({
  page,
}) => {
  await page.goto(agentChat(instances.ready));
  const rows = page.getByTestId("chat-sessions").locator('a[data-testid^="chat-session-"]');
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });
  const before = await rows.count();

  const mine = page.locator('[data-testid="chat-message"][data-role="user"]').last();
  await mine.hover();
  await mine.locator('[data-testid^="chat-message-menu-"]').click();
  await page.waitForTimeout(400);
  await openMenu(page).getByRole("menuitem", { name: "Fork chat" }).click();

  await expect(page).not.toHaveURL(new RegExp(`/agents/${instances.ready}/chat$`), {
    timeout: 30_000,
  });
  await expect(page).toHaveURL(/\/agents\/[0-9a-f-]{36}\/chat$/);
  await expect(rows).toHaveCount(before + 1);
  await expect(page.getByTestId("chat-sessions")).toContainText("(fork)");
});
