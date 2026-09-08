import { test, expect } from "../../fixtures/test";
import { agentChat, instances } from "../../helpers/app";

test("chat: a conversation is forked from a message's menu, and the fork opens", async ({
  page,
}) => {
  /*
   * A fork is a new conversation that starts from where this one is, so the proof is
   * the address bar: it now names an instance that is not the one forked, and the rail
   * lists that instance beside the source.
   */
  await page.goto(agentChat(instances.ready));
  const rows = page.getByTestId("chat-sessions").locator('a[data-testid^="chat-session-"]');
  await expect(rows.first()).toBeVisible({ timeout: 30_000 });
  const before = await rows.count();

  const mine = page.locator('[data-testid="chat-message"][data-role="user"]').first();
  await expect(mine).toBeVisible({ timeout: 30_000 });
  // Not on the agent's, which is most of a transcript once tool calls are in it.
  await expect(
    page.locator('[data-testid="chat-message"]:not([data-role="user"])')
      .locator('[data-testid^="chat-message-menu-"]'),
  ).toHaveCount(0);
  await mine.hover();
  await mine.locator('[data-testid^="chat-message-menu-"]').click();
  // The dropdown animates in, and a click landing mid-transition is refused as
  // unstable rather than missing the element.
  await page.waitForTimeout(400);
  await page.getByRole("menuitem", { name: "Fork chat" }).click();

  await expect(page).not.toHaveURL(new RegExp(`/agents/${instances.ready}/chat$`), {
    timeout: 30_000,
  });
  await expect(page).toHaveURL(/\/agents\/[0-9a-f-]{36}\/chat$/);
  await expect(rows).toHaveCount(before + 1);
  await expect(page.getByTestId("chat-sessions")).toContainText("(fork)");
});
