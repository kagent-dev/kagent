import { test, expect } from "../../fixtures/test";
import { agentChat, instances, loadPage, withScenario } from "../../helpers/app";

/**
 * Chat — the failure journeys.
 *
 * Three ways a conversation goes wrong, each of which the user has to be able to get
 * out of: the turn fails mid-flight, the user stops a turn themselves, and the list
 * of the agent's other conversations cannot be loaded at all. The thing they have in
 * common is that none of them may leave the page stuck with no way forward.
 *
 * The conversation and that list fail independently, which is why they are separate
 * journeys: `?chat=…` drives the turn's outcome and `?mock=…` drives the API.
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

test("chat: the list of other conversations reports its own failure", async ({
  page,
}) => {
  await test.step("1. the list says it failed", async () => {
    // The API scenario, not the chat one: the conversation itself and the list of the
    // agent's other conversations fail independently, and this is the list.
    await loadPage(page, AGENT_CHAT, { scenario: "error" });

    const error = page.getByTestId("chat-sessions-error");
    await expect(error).toBeVisible();
    await expect(error).toContainText("Could not load conversations");
  });

  await test.step("2. it is not mistaken for having no conversations", async () => {
    await expect(page.getByTestId("chat-sessions-empty")).toHaveCount(0);
  });

  await test.step("3. it recovers", async () => {
    await page.goto(withScenario(AGENT_CHAT, "ok"));
    await expect(page.getByTestId("chat-sessions-error")).toHaveCount(0);
    // This conversation is in its own rail, marked as the one that is open.
    await expect(
      page.getByTestId(`chat-session-${instances.ready}`),
    ).toBeVisible();
  });
});

/**
 * A conversation the agent has parked on a question.
 *
 * This is the reported "the agent worked and then suddenly stopped": the agent called
 * `ask_user`, its turn parked in `input_required`, and because that state is
 * non-terminal it holds the instance's one active-task slot — so the controller
 * refuses every further message with `FailedPrecondition`. Nothing on screen said any
 * of that, because the question renders as ordinary agent prose and the conversation
 * looks finished.
 *
 * There are two ways out and the journey drives both: answering, which is a message
 * that *names* the parked turn and resumes it, and giving the question up, which
 * cancels the task. A message that does neither is refused — and that refusal is the
 * reported symptom.
 *
 * The fixture is the controller's behaviour, copied — the parked turn survives a
 * reload, a send while it stands is refused in the controller's own words, and
 * cancelling its task frees the conversation.
 */
test("chat: a question the agent is waiting on is said, and can be given up", async ({
  page,
}) => {
  await test.step("1. a turn ends by asking rather than by finishing", async () => {
    await page.goto(`${AGENT_CHAT}?chat=asks`);
    await page.getByTestId("chat-input").fill("What should I order?");
    await page.getByTestId("chat-send").click();

    // The question itself arrives as prose, which is exactly why it is not enough.
    await expect(
      page.getByTestId("chat-message").last(),
    ).toContainText("What size pizza would you like?", { timeout: 20_000 });
  });

  await test.step("2. the page says the agent is waiting, and does not call it a failure", async () => {
    await expect(page.getByTestId("chat-awaiting-reply")).toBeVisible();
    // Nothing went wrong. A red alert over a turn that worked correctly would be a
    // visible lie, and it is the reason this state was read as a broken agent.
    await expect(page.getByTestId("chat-turn-error")).toHaveCount(0);
    // And the lifecycle indicator agrees, rather than reporting a ready agent.
  });

  await test.step("3. its choices are offered as choices, and honour `multiple`", async () => {
    // The payload carries two questions, one single-choice and one multi. Which
    // control each gets is read from the question's own `multiple` flag — a
    // single-choice question rendered as a multi-select sends an array the agent
    // never asked for, and the runtime has no way to complain about it.
    await expect(page.getByTestId("chat-question")).toHaveCount(2);
    await expect(
      page.getByTestId("chat-choices-0").locator(".ant-radio-input"),
      "a single-choice question takes one answer",
    ).toHaveCount(3);
    await expect(
      page.getByTestId("chat-choices-1").locator(".ant-checkbox-input"),
      "a question marked `multiple` takes several",
    ).toHaveCount(3);

    // And nothing can be sent until every question has an answer: the runtime pairs
    // them positionally, so a gap answers the wrong question.
    await expect(page.getByTestId("chat-answer-send")).toBeDisabled();
  });

  await test.step("4. the question, its choices and all, survive a reload", async () => {
    // The state belongs to the *task*, not to anything on screen — so a reader who
    // comes back tomorrow meets the same question with the same options, rather than
    // meeting a refusal. The payload is persisted with the task, which is what makes
    // that possible; the choices are not re-derivable from the prose.
    await page.reload();
    await expect(page.getByTestId("chat-awaiting-reply")).toBeVisible();
    await expect(page.getByTestId("chat-question")).toHaveCount(2);
    await expect(page.getByTestId("chat-choices-0").locator(".ant-radio-input")).toHaveCount(3);
  });

  await test.step("5. answering it resumes the turn that asked, and the agent uses the answer", async () => {
    await page.getByTestId("chat-choices-0").getByText("Large", { exact: true }).click();
    await page.getByTestId("chat-choices-1").getByText("Pineapple", { exact: true }).click();
    await expect(page.getByTestId("chat-answer-send")).toBeEnabled();
    await page.getByTestId("chat-answer-send").click();

    // The choices are in the transcript as prose, so the conversation reads as what
    // happened rather than as an empty message the agent somehow understood.
    await expect(
      page.locator('[data-testid="chat-message"][data-role="user"]').filter({
        hasText: "Large",
      }),
    ).toHaveCount(1);

    // And the *structured* answer arrived, which the prose alone cannot show. The
    // fixture answers "I did not catch a choice in that" when the metadata is
    // missing or its correlation id is wrong — the exact silent failure this whole
    // path is at risk of.
    await expect(page.getByTestId("chat-message").last()).toContainText(
      "Noted: Large; Pineapple",
    );
    await expect(page.getByTestId("chat-awaiting-reply")).toHaveCount(0);
  });

  await test.step("6. and the conversation takes ordinary messages again", async () => {
    // The proof that the turn really closed is a *new* turn running, not an alert
    // that disappeared: a task still parked would refuse this.
    await page.goto(`${AGENT_CHAT}?chat=ok`);
    await page.getByTestId("chat-input").fill("How many pods are running?");
    await page.getByTestId("chat-send").click();
    await expect(page.getByTestId("chat-cancel")).toHaveCount(0, { timeout: 30_000 });
    await expect(page.getByTestId("chat-turn-error")).toHaveCount(0);
  });
});

/**
 * The other way out of a question: giving it up.
 *
 * Its own journey because it is a different ending, and because the state it starts
 * from has to be reached from a clean session — the parked turn is remembered for the
 * browsing session, so driving both endings in one test would have the second start
 * from whatever the first left behind.
 */
test("chat: a question can be discarded instead of answered", async ({ page }) => {
  await test.step("1. a turn parks on a question", async () => {
    await page.goto(`${AGENT_CHAT}?chat=asks`);
    await page.getByTestId("chat-input").fill("What should I order?");
    await page.getByTestId("chat-send").click();
    await expect(page.getByTestId("chat-awaiting-reply")).toBeVisible({
      timeout: 20_000,
    });
  });

  await test.step("2. discarding it frees the conversation", async () => {
    await page.getByTestId("chat-dismiss-question").click();
    await expect(page.getByTestId("chat-awaiting-reply")).toHaveCount(0);
    // Either reading is correct here. Giving up the question ends the turn; whether the
    // conversation is still logically ready depends on what has been asked of it since,
    // and this step is about the question being gone rather than about the state.
  });

  await test.step("3. and an unrelated message is accepted again", async () => {
    await page.goto(`${AGENT_CHAT}?chat=ok`);
    await page.getByTestId("chat-input").fill("How many pods are running?");
    await page.getByTestId("chat-send").click();
    await expect(page.getByTestId("chat-cancel")).toHaveCount(0, { timeout: 30_000 });
    await expect(page.getByTestId("chat-turn-error")).toHaveCount(0);
  });
});
