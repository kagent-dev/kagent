import { test, expect } from "../../fixtures/test";
import { agentChat, instances } from "../../helpers/app";

/** The menu that is actually on screen: antd leaves a closed dropdown mounted. */
const openMenu = (page: import("@playwright/test").Page) =>
  page.locator(".ant-dropdown:not(.ant-dropdown-hidden)");

test("chat: a checkpoint is offered on the reader's latest message only", async ({
  page,
}) => {
  await page.goto(agentChat(instances.ready));
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine.first()).toBeVisible({ timeout: 30_000 });

  await test.step("1. not on the agent's messages", async () => {
    await expect(
      page
        .locator('[data-testid="chat-message"]:not([data-role="user"])')
        .locator('[data-testid^="chat-message-checkpoint-"]'),
    ).toHaveCount(0);
  });

  await test.step("2. the seeded conversation opens with its boundary marked", async () => {
    await expect(mine.first()).toHaveAttribute("data-checkpointed", "true");
    await expect(
      mine.first().locator('[data-testid^="chat-message-checkpointed-"]'),
    ).toBeVisible();
  });

  await test.step("3. a newer message can be checkpointed, an older one cannot", async () => {
    await page.getByTestId("chat-input").fill("Another question, so the first is not last.");
    await page.getByTestId("chat-send").click();
    await expect(mine).toHaveCount(2, { timeout: 30_000 });

    await expect(mine.last().locator('[data-testid^="chat-message-checkpoint-"]')).toBeEnabled();
    // A checkpoint is taken at the conversation's current boundary and nowhere else,
    // so an earlier message is shown the control disabled rather than not at all.
    await expect(
      mine.first().locator('[data-testid^="chat-message-checkpoint-"]'),
    ).toBeDisabled();
  });
});

test("chat: forking is refused until the message is checkpointed", async ({ page }) => {
  await page.goto(agentChat(instances.ready));
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine.first()).toBeVisible({ timeout: 30_000 });

  await page.getByTestId("chat-input").fill("A turn with no checkpoint against it.");
  await page.getByTestId("chat-send").click();
  await expect(mine).toHaveCount(2, { timeout: 30_000 });

  const latest = mine.last();
  await latest.hover();
  await latest.locator('[data-testid^="chat-message-menu-"]').click();
  await expect(
    openMenu(page).getByRole("menuitem", { name: "Fork chat from here" }),
  ).toHaveClass(/ant-dropdown-menu-item-disabled/);
  await page.keyboard.press("Escape");

  await test.step("checkpointing it enables the fork", async () => {
    await latest.locator('[data-testid^="chat-message-checkpoint-"]').click();
    await expect(latest).toHaveAttribute("data-checkpointed", "true");

    await latest.locator('[data-testid^="chat-message-menu-"]').click();
    await expect(
      openMenu(page).getByRole("menuitem", { name: "Fork chat from here" }),
    ).not.toHaveClass(/ant-dropdown-menu-item-disabled/);
  });
});

test("chat: a fork of an earlier checkpoint opens holding only what came before it", async ({
  page,
}) => {
  await page.goto(agentChat(instances.ready));
  const rows = page.getByTestId("chat-sessions").locator('a[data-testid^="chat-session-"]');
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });
  const before = await rows.count();
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine).toHaveCount(1, { timeout: 30_000 });

  // A second turn, so the seeded checkpoint is no longer the conversation's latest
  // boundary and forking it has something to leave behind.
  await page.getByTestId("chat-input").fill("A second turn, after the saved boundary.");
  await page.getByTestId("chat-send").click();
  await expect(mine).toHaveCount(2, { timeout: 30_000 });

  const checkpointed = mine.first();
  await checkpointed.hover();
  await checkpointed.locator('[data-testid^="chat-message-menu-"]').click();
  await page.waitForTimeout(400);
  await openMenu(page).getByRole("menuitem", { name: "Fork chat from here" }).click();

  await expect(page).toHaveURL(/\/agents\/[0-9a-f-]{36}\/chat$/);
  await expect(page).not.toHaveURL(new RegExp(`/agents/${instances.ready}/chat$`), {
    timeout: 30_000,
  });
  await expect(rows).toHaveCount(before + 1);
  await expect(page.getByTestId("chat-sessions")).toContainText("(fork)");
  // The whole point of forking a boundary rather than the conversation: the second
  // turn is not in the copy.
  await expect(page.locator('[data-testid="chat-message"][data-role="user"]')).toHaveCount(
    1,
    { timeout: 30_000 },
  );
});

test("chat: a conversation is duplicated from the rail, and the copy opens", async ({
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
  await row.getByTestId(`chat-session-duplicate-${instances.ready}`).click();

  await expect(page).not.toHaveURL(new RegExp(`/agents/${instances.ready}/chat$`), {
    timeout: 30_000,
  });
  await expect(rows).toHaveCount(before + 1);
  await expect(page.getByTestId("chat-sessions")).toContainText("(copy)");
});
