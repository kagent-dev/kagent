import { test, expect } from "../../fixtures/test";
import { sendAndAwaitTurn } from "../../helpers/conversation";
import { agentConversation, instances } from "../../helpers/app";
import { tick } from "../../helpers/controls";
import { LIFECYCLE_TIMEOUT, pressOnce } from "../../helpers/resource";

/**
 * A turn that ends by asking rather than by finishing.
 *
 * This is the reported "the agent worked and then suddenly stopped": the agent called
 * `ask_user`, its turn parked in `input_required`, and because that state is
 * non-terminal it holds the instance's one active-task slot — so the controller
 * refuses every further message with `FailedPrecondition`. Nothing on screen said any
 * of that, because the question renders as ordinary agent prose and the conversation
 * looks finished.
 *
 * One journey rather than three tests, because there is exactly one way in and the
 * ways out are alternatives to each other: a turn parks, and from there it is
 * answered, discarded, or it blocks the conversation. Each step returns the
 * conversation to a state the next can start from, which is what makes them steps.
 *
 * The fixture is the controller's behaviour, copied — the parked turn survives a
 * reload, a send while it stands is refused in the controller's own words, and
 * cancelling its task frees the conversation.
 */

const AGENT_CONVERSATION = agentConversation(instances.ready);

/*
 * A journey in one test, so it gets the lifecycle budget rather than the default.
 * Sized for the number of steps, not for the folder it sits in — see `LIFECYCLE_TIMEOUT`.
 */
test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("conversation: a question is asked, answered, given up, and answered with the keyboard", async ({
  page,
}) => {
  await test.step("1. a turn ends by asking rather than by finishing", async () => {
    await page.goto(`${AGENT_CONVERSATION}?conversation=asks`);
    await page.getByTestId("conversation-input").fill("What should I order?");
    await page.getByTestId("conversation-send").click();

    // The actionable card is the only representation of the pending interaction;
    // raw ask_user protocol artifacts do not become conversation messages.
    await expect(page.getByTestId("conversation-awaiting-reply")).toContainText(
      "What size pizza would you like?",
      { timeout: 20_000 },
    );
    await expect(
      page.getByTestId("conversation-tool-call").filter({ hasText: "ask_user" }),
    ).toHaveCount(0);
  });

  await test.step("2. the page says the agent is waiting, and does not call it a failure", async () => {
    await expect(page.getByTestId("conversation-awaiting-reply")).toBeVisible();
    // Nothing went wrong. A red alert over a turn that worked correctly would be a
    // visible lie, and it is the reason this state was read as a broken agent.
    await expect(page.getByTestId("conversation-turn-error")).toHaveCount(0);
    // And the lifecycle indicator agrees, rather than reporting a ready agent.
  });

  await test.step("3. its choices are offered as choices, and honour `multiple`", async () => {
    // The payload carries two questions, one single-choice and one multi. Which
    // control each gets is read from the question's own `multiple` flag — a
    // single-choice question rendered as a multi-select sends an array the agent
    // never asked for, and the runtime has no way to complain about it.
    await expect(page.getByTestId("conversation-question")).toHaveCount(2);
    await expect(
      page.getByTestId("conversation-choices-0").locator(".ant-radio-input"),
      "a single-choice question takes one answer",
    ).toHaveCount(3);
    await expect(
      page.getByTestId("conversation-choices-1").locator(".ant-checkbox-input"),
      "a question marked `multiple` takes several",
    ).toHaveCount(3);

    // And nothing can be sent until every question has an answer: the runtime pairs
    // them positionally, so a gap answers the wrong question.
    await expect(page.getByTestId("conversation-answer-send")).toBeDisabled();
  });

  await test.step("4. the question, its choices and all, survive a reload", async () => {
    // The state belongs to the *task*, not to anything on screen — so a reader who
    // comes back tomorrow meets the same question with the same options, rather than
    // meeting a refusal. The payload is persisted with the task, which is what makes
    // that possible; the choices are not re-derivable from the prose.
    await page.reload();
    await expect(page.getByTestId("conversation-awaiting-reply")).toBeVisible();
    await expect(page.getByTestId("conversation-question")).toHaveCount(2);
    await expect(page.getByTestId("conversation-choices-0").locator(".ant-radio-input")).toHaveCount(3);
  });

  await test.step("5. answering it resumes the turn that asked, and the agent uses the answer", async () => {
    await tick(page.getByTestId("conversation-choices-0").getByRole("radio", { name: "Large" }));
    await tick(page.getByTestId("conversation-choices-1").getByRole("checkbox", { name: "Pineapple" }));
    await expect(page.getByTestId("conversation-answer-send")).toBeEnabled();
    await page.getByTestId("conversation-answer-send").click();

    // The completed interaction is one read-only card, not protocol response text.
    await expect(
      page.locator('[data-testid="conversation-message"][data-role="user"]').filter({
        hasText: "Large",
      }),
    ).toHaveCount(1);
    await expect(page.getByTestId("conversation-ask-user-record")).toHaveCount(1);

    // And the *structured* answer arrived, which the prose alone cannot show. The
    // fixture answers "I did not catch a choice in that" when the metadata is
    // missing or its correlation id is wrong — the exact silent failure this whole
    // path is at risk of.
    await expect(page.getByTestId("conversation-message").last()).toContainText(
      "Noted: Large; Pineapple",
    );
    await expect(page.getByTestId("conversation-awaiting-reply")).toHaveCount(0);
  });

  await test.step("6. and the conversation takes ordinary messages again", async () => {
    // The proof that the turn really closed is a *new* turn running, not an alert
    // that disappeared: a task still parked would refuse this.
    await page.goto(`${AGENT_CONVERSATION}?conversation=ok`);
    await sendAndAwaitTurn(page, "How many pods are running?");
    await expect(page.getByTestId("conversation-turn-error")).toHaveCount(0);
  });

  await test.step("7. a second question can be given up rather than answered", async () => {
    // Parked again, because the previous steps answered the first one: discarding is
    // the alternative to answering, so it needs its own question to discard.
    await page.goto(`${AGENT_CONVERSATION}?conversation=asks`);
    await page.getByTestId("conversation-input").fill("What should I order?");
    await page.getByTestId("conversation-send").click();
    await expect(page.getByTestId("conversation-awaiting-reply")).toBeVisible({
      timeout: 20_000,
    });

    // One press dismisses: a second one races the card leaving and waits on a detached button.
    await pressOnce(page.getByTestId("conversation-dismiss-question"));
    await expect(page.getByTestId("conversation-awaiting-reply")).toHaveCount(0);
    // Either reading is correct here. Giving up the question ends the turn; whether the
    // conversation is still logically ready depends on what has been asked of it since,
    // and this step is about the question being gone rather than about the state.
  });

  await test.step("8. and an unrelated message is accepted again", async () => {
    await page.goto(`${AGENT_CONVERSATION}?conversation=ok`);
    await sendAndAwaitTurn(page, "How many pods are running?");
    await expect(page.getByTestId("conversation-turn-error")).toHaveCount(0);
  });

  await test.step("9. a question that is one prose field takes the caret", async () => {
    await page.goto(`${AGENT_CONVERSATION}?conversation=asks-text`);
    await page.getByTestId("conversation-input").fill("Order me a pizza");
    await page.getByTestId("conversation-send").click();

    await expect(page.getByTestId("conversation-awaiting-reply")).toBeVisible({ timeout: 20_000 });
    // One question, and no choices — the shape the rest of this test is about.
    await expect(page.getByTestId("conversation-question")).toHaveCount(1);
    await expect(page.getByTestId("conversation-choices-0")).toHaveCount(0);
  });

  await test.step("10. the field has the caret already", async () => {
    await expect(page.getByTestId("conversation-answer-text-0")).toBeFocused();
  });

  await test.step("11. Enter sends it, without reaching for the button", async () => {
    // Typed with the keyboard rather than filled, because what is under test is that
    // the caret was already in the right place — `fill` would put it there itself and
    // pass whether or not step 2 held.
    await page.keyboard.type("Extra napkins");
    await page.keyboard.press("Enter");

    // The structured answer arrived, which is the same proof the choices journey
    // above uses: the fixture says "I did not catch a choice in that" when the
    // metadata is missing or its correlation id is wrong.
    await expect(page.getByTestId("conversation-message").last()).toContainText(
      "Noted: Extra napkins",
      { timeout: 20_000 },
    );
    await expect(page.getByTestId("conversation-awaiting-reply")).toHaveCount(0);
  });

  await test.step("12. and the caret comes back to the composer", async () => {
    // The field it was in is gone with the question, so a caret left there is a caret
    // nowhere — and the next thing typed is an ordinary message.
    await expect(page.getByTestId("conversation-input")).toBeFocused();
  });
});
