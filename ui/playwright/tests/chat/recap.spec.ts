import type { Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { agentChat, instances, loadPage } from "../../helpers/app";
import { sendMessage } from "../../helpers/chat";

/**
 * The recap card: a stored summary shown after the last message once the reader has
 * been away from the chat for three minutes.
 *
 * The clock is Playwright's, so the three minutes pass instantly and nothing waits.
 */

const AWAY_MS = 3 * 60 * 1000;

/** An open conversation on a clock the test controls. */
async function openChat(page: Page, id: string): Promise<void> {
  await page.clock.install();
  await loadPage(page, agentChat(id));
  await expect(page.getByTestId("chat-message").first()).toBeVisible();
}

/** Time passes with nobody in the pane, then the pointer comes back. */
async function goAwayAndReturn(page: Page): Promise<void> {
  await page.clock.fastForward(AWAY_MS);
  await page.getByTestId("chat-transcript").hover();
}

test("chat recap: a key press in the composer counts as returning", async ({ page }) => {
  await openChat(page, instances.recapCurrent);
  await page.clock.fastForward(AWAY_MS);
  await page.getByRole("textbox").last().focus();
  await page.keyboard.press("a");
  await expect(page.getByTestId("chat-recap")).toBeVisible();
});

test("chat recap: shows once the reader returns, and not otherwise", async ({ page }) => {
  const recap = page.getByTestId("chat-recap");

  await test.step("1. it is absent while the reader has not been away", async () => {
    await openChat(page, instances.recapCurrent);
    await expect(page.getByTestId("chat-message")).toHaveCount(6);
    await page.getByTestId("chat-transcript").hover();
    await expect(recap).toHaveCount(0);
  });

  await test.step("2. it appears after the last message when they return from three minutes away", async () => {
    await goAwayAndReturn(page);
    await expect(recap).toBeVisible();
    await expect(recap).toContainText("liveness probe");
    const followsLastMessage = await recap.evaluate((card) => {
      const messages = document.querySelectorAll('[data-testid="chat-message"]');
      const last = messages[messages.length - 1];
      return Boolean(last.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING);
    });
    expect(followsLastMessage, "the recap should sit after the last message").toBe(true);
  });

  await test.step("3. its header says how much of the conversation it covers", async () => {
    await expect(recap).toContainText("Recap of the last 30 minutes");
    await expect(recap).toHaveAttribute("aria-expanded", "true");
  });

  await test.step("4. clicking it collapses it to its header, and clicking again expands it", async () => {
    await recap.click();
    await expect(recap).toHaveAttribute("aria-expanded", "false");
    await expect(recap).not.toContainText("liveness probe");
    await expect(recap).toContainText("Recap of the last 30 minutes");
    await recap.click();
    await expect(recap).toHaveAttribute("aria-expanded", "true");
    await expect(recap).toContainText("liveness probe");
  });

  await test.step("5. it opens again after another absence, and sending a message dismisses it", async () => {
    await goAwayAndReturn(page);
    await expect(recap).toBeVisible();
    await sendMessage(page, "One more question");
    await expect(page.getByTestId("chat-message").filter({ hasText: "One more question" })).toBeVisible();
    await expect(recap).toHaveCount(0);
  });

  await test.step("6. it is absent when it no longer covers every turn", async () => {
    await openChat(page, instances.recapStale);
    await expect(page.getByTestId("chat-message")).toHaveCount(8);
    await goAwayAndReturn(page);
    await expect(recap).toHaveCount(0);
  });
});
