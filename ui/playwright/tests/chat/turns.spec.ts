import { test, expect } from "../../fixtures/test";
import { agentChat, instances } from "../../helpers/app";
import { expectTranscriptSettled } from "../../helpers/chat";

/**
 * A turn that does not finish the way it should.
 *
 * Two independent failure modes, kept as two tests rather than merged into one
 * journey: the turn fails mid-flight, and the reader stops one themselves. Unlike a
 * resource lifecycle — where a broken create genuinely does make the rest moot —
 * these do not build on each other, and folding them together would let a broken
 * retry hide whether cancelling works.
 *
 * What they have in common is the property worth pinning: neither may leave the page
 * stuck with no way forward. The composer comes back, and the conversation takes
 * another message afterwards.
 *
 * `?chat=…` drives the turn's outcome; `?mock=…` drives the API. They fail
 * independently, which is why a failed *list* of conversations lives in
 * `agent-rail.spec.ts` beside the rail that shows it, not here.
 */

const AGENT_CHAT = agentChat(instances.ready);

test("chat: a failed turn is reported and can be retried", async ({ page }) => {
  await test.step("1. the turn starts normally", async () => {
    await page.goto(`${AGENT_CHAT}?chat=error`);
    await page.getByTestId("chat-input").fill("This one fails");
    await page.getByTestId("chat-send").click();

    // The user's message is theirs; a failing agent does not erase it.
    await expect(
      page.locator('[data-testid="chat-message"][data-role="user"]')
        .filter({ hasText: "This one fails" }),
    ).toHaveCount(1);
  });

  await test.step("2. the failure is reported, with what went wrong", async () => {
    const error = page.getByTestId("chat-turn-error");
    await expect(error).toBeVisible();
    await expect(error).toContainText("could not finish this turn");
    await expect(error).toContainText("stopped responding");
  });

  await test.step("3. the composer is usable again rather than stuck streaming", async () => {
    await expect(page.getByTestId("chat-send")).toBeVisible();
    await expect(page.getByTestId("chat-cancel")).toHaveCount(0);
  });

  await test.step("4. retrying resends and clears the previous failure", async () => {
    /*
     * This step also covers a turn crossing a suspend, which is not obvious from
     * reading it.
     *
     * A suspended conversation is one a reader can meet at any point — they may have
     * suspended it themselves, or left it and come back. Retry begins its turn inside
     * `useChat` and never passes through the composer, so a page that resumed only
     * around `send` would leave this one button broken while every other path worked.
     */
    await expectTranscriptSettled(page);
    await page.getByRole("button", { name: "Retry" }).click();
    // Still the failing scenario, so it fails again — the point is that the
    // retry ran at all, and that the page did not double up the user's message.
    await expect(page.getByTestId("chat-turn-error")).toBeVisible();
    await expect(
      page.locator('[data-testid="chat-message"][data-role="user"]')
        .filter({ hasText: "This one fails" }),
      "a retry should resend the message, not duplicate the original",
    ).toHaveCount(2);
  });

  await test.step("5. a healthy turn afterwards works", async () => {
    await page.goto(AGENT_CHAT);
    await page.getByTestId("chat-input").fill("Now it works");
    await page.getByTestId("chat-send").click();
    await expect(page.getByTestId("chat-turn-error")).toHaveCount(0);
    await expect(page.getByTestId("chat-send")).toBeVisible({ timeout: 20_000 });
  });
});

test("chat: a turn can be stopped while it is streaming", async ({ page }) => {
  await test.step("1. a long turn starts", async () => {
    // The slow scenario exists so this is deterministic rather than a race
    // against a stream that might already have finished.
    await page.goto(`${AGENT_CHAT}?chat=slow`);
    await page.getByTestId("chat-input").fill("This one gets stopped");
    await page.getByTestId("chat-send").click();
    await expect(page.getByTestId("chat-cancel")).toBeVisible();
  });

  await test.step("2. stopping it reports a cancelled turn", async () => {
    await page.getByTestId("chat-cancel").click();
    await expect(page.getByTestId("chat-status")).toHaveAttribute(
      "data-state",
      "canceled",
    );
  });

  await test.step("3. the composer returns and nothing further streams in", async () => {
    await expect(page.getByTestId("chat-send")).toBeVisible();

    const settled = await page.getByTestId("chat-message").count();
    await page.waitForTimeout(2_000);
    await expect(
      page.getByTestId("chat-message"),
      "a stopped turn should stop producing messages",
    ).toHaveCount(settled);
  });

  await test.step("4. the conversation is still usable", async () => {
    await expect(page.getByTestId("chat-input")).toBeEditable();
  });
});
