import { test, expect } from "../../fixtures/test";
import { agentChat, instances } from "../../helpers/app";

/**
 * A turn parked on something other than a question.
 *
 * `questions.spec.ts` covers `ask_user`, which is answerable. These are its two
 * neighbours: a tool approval, which this build asks the reader to vouch for tool by
 * tool, and a request it does not recognise, which it says plainly it cannot answer.
 *
 * Both were deferred as "blocked on a product decision" while the controls were being
 * argued about. The decision landed in #2714 and the controls shipped with it — approve,
 * reject, a reason per rejection — and nothing in the browser had driven them since.
 */

const AGENT_CHAT = agentChat(instances.ready);

test("chat: a tool approval is decided per tool, and the decisions are what go back", async ({
  page,
}) => {
  await page.goto(`${AGENT_CHAT}?chat=approves`);
  await page.getByTestId("chat-input").fill("Tidy the cluster up.");
  await page.getByTestId("chat-send").click();

  const prompt = page.getByTestId("chat-awaiting-reply");
  await expect(prompt).toBeVisible({ timeout: 30_000 });
  await expect(prompt).toHaveAttribute("data-kind", "tool_approval");

  await test.step("1. it names what it is asking to run, and why", async () => {
    // The tools by name, because "the agent wants to run some tools" is not something a
    // reader can vouch for. The hint is the runtime's own sentence about the risk.
    const tools = page.getByTestId("chat-approval-tool");
    await expect(tools).toHaveCount(2);
    await expect(tools.first()).toHaveText("kubectl_apply");
    await expect(tools.last()).toHaveText("shell_exec");
    await expect(prompt).toContainText("Approve only what you recognise");
  });

  await test.step("2. one allowed and one denied, in the same submission", async () => {
    // Per row, not per form: the decision belongs to the tool, and a control wired to
    // the form would send the same answer for both.
    const rows = page.getByTestId("chat-approval-tool");
    await rows.first().locator("..").getByRole("button", { name: "Allow" }).click();
    await rows.last().locator("..").getByRole("button", { name: "Deny" }).click();

    // The reason appears with the rejection and belongs to that tool alone — keyed on
    // the tool's id, so a form that kept one reason for the page would fail here.
    const reason = page.getByTestId("chat-approval-reason-call-2");
    await expect(reason).toBeVisible();
    await expect(page.getByTestId("chat-approval-reason-call-1")).toHaveCount(0);
    await reason.fill("It deletes a cache I still need.");
  });

  await test.step("3. the agent is told which was which", async () => {
    await page.getByTestId("chat-approval-submit").click();

    /*
     * Read back off the reply rather than off the form: the fixture reads the decisions
     * out of the payload it received and says them, so this fails if the page sent both
     * as approvals, paired the reason with the wrong tool, or dropped the reason.
     */
    const transcript = page.getByTestId("chat-transcript");
    await expect(transcript).toContainText("call-1 approved", { timeout: 30_000 });
    await expect(transcript).toContainText("call-2 rejected");
    await expect(transcript).toContainText("It deletes a cache I still need.");
    // And the turn is no longer parked, so the conversation is usable again.
    await expect(prompt).toHaveCount(0);
  });
});

test("chat: one tool is approved or rejected on the prompt itself", async ({ page }) => {
  /*
   * One tool is a different prompt, not a shorter one: there is nothing to decide
   * between, so the decision is the prompt's own Approve and Reject rather than a row's
   * Allow and Deny behind a Submit. Both paths send the same payload, and only this one
   * has ever been reachable by a reader with a single-tool agent.
   */
  await page.goto(`${AGENT_CHAT}?chat=approves-one`);
  await page.getByTestId("chat-input").fill("Clear the cache.");
  await page.getByTestId("chat-send").click();

  const prompt = page.getByTestId("chat-awaiting-reply");
  await expect(prompt).toBeVisible({ timeout: 30_000 });
  await expect(page.getByTestId("chat-approval-tool")).toHaveCount(1);

  // Rejecting asks why, with the caret already in the field — the one place it can be,
  // there being no other tool to choose between.
  await page.getByTestId("chat-approval-reject").click();
  const reason = page.getByTestId("chat-approval-reason-call-2");
  await expect(reason).toBeFocused();
  await page.keyboard.type("Not on a Friday.");

  await page.getByTestId("chat-approval-submit").click();
  const transcript = page.getByTestId("chat-transcript");
  await expect(transcript).toContainText("call-2 rejected", { timeout: 30_000 });
  await expect(transcript).toContainText("Not on a Friday.");
  await expect(prompt).toHaveCount(0);
});

test("chat: a question this build cannot answer says so, rather than guessing", async ({
  page,
}) => {
  /*
   * A turn started without the HITL extension carries its question as prose and no
   * correlation id, so there is nothing to answer against. It arises from a send this
   * build did not make — a `kubectl`-driven one, an older client — and the only honest
   * thing the page can do is say which of the two it is looking at.
   */
  await page.goto(`${AGENT_CHAT}?chat=asks-unknown`);
  await page.getByTestId("chat-input").fill("Do the thing.");
  await page.getByTestId("chat-send").click();

  const prompt = page.getByTestId("chat-awaiting-reply");
  await expect(prompt).toBeVisible({ timeout: 30_000 });
  await expect(prompt).toHaveAttribute("data-kind", "unknown");
  await expect(prompt).toContainText("without the extension that carries them");

  // No controls invented for it: there is nothing to answer, so the only way out is to
  // let the turn go. Asserted as an absence *after* the prompt is on screen, which is
  // what makes the absence mean anything.
  await expect(page.getByTestId("chat-approval-submit")).toHaveCount(0);
  await expect(page.getByTestId("chat-answer-text-0")).toHaveCount(0);

  await page.getByTestId("chat-dismiss-question").click();
  await expect(prompt).toHaveCount(0);
});
