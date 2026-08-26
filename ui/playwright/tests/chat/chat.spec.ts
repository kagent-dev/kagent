import { test, expect } from "../../fixtures/test";
import { SIBLING_OF_READY, agentChat, instances, loadPage } from "../../helpers/app";

/**
 * Chat — the conversation journey.
 *
 * One continuous story, because that is what a conversation is: open an agent that
 * already has history, add a turn to it, watch the turn stream in, add another, and
 * see the whole thing survive a reload. Splitting these would lose the only thing
 * worth checking — that the message you sent is the one that ended up in the
 * transcript you reload.
 *
 * ## The fixture is silent about the reader's message, because the gateway is
 *
 * `MockChatClient` records what the reader sent and never announces it, which is
 * what the A2A gateway does: measured on 2026-08-24, a completed turn emits a
 * `WORKING` frame and a `COMPLETED` frame and no message frame at all, while
 * `ListTasks` afterwards holds the user's message and nothing else. The fixture used
 * to echo it, and that one generosity kept this file green while the reader's own
 * words were invisible on a cluster until they reloaded. Every "the message is on
 * screen" assertion below therefore checks the client put it there.
 *
 * ## There is no session to open first
 *
 * An `AgentInstance` *is* the conversation: the A2A gateway files every task under
 * the instance as its `contextId`, and `ListTasks` for the instance is the
 * transcript. So arriving at an agent is arriving at its conversation, and the rail
 * beside it lists the *other* conversations with the same agent — which are the
 * sibling instances of the same harness and template.
 */

/** The instance with a seeded conversation behind it. */
const AGENT_CHAT = agentChat(instances.ready);

const FIRST_QUESTION = "How many pods are running?";
const SECOND_QUESTION = "And which of them is the reconciler?";

/**
 * The reply to a given question, as it *renders* rather than as it is written.
 *
 * The fixture answers in Markdown, so the bold and the backticks contribute their text
 * and the list items become their own lines. Asserting the rendered text keeps this a
 * check on the transcript rather than on the renderer, which the Markdown test below
 * covers separately.
 *
 * Taking the question as an argument is not tidiness: the fixture quotes it back, so
 * the two turns below have *different* replies — and an assertion that could not tell
 * them apart would pass on a second turn that re-rendered the first one's answer.
 */
const replyTo = (question: string) =>
  [
    "There are 3 pods running in the kagent namespace:",
    "",
    "kagent-controller — the reconciler",
    "kagent-ui — this page",
    "kagent-tools — the tool server",
    "",
    `You asked: "${question}".`,
  ].join("\n");

test("chat: history, sending, streaming, and tool rendering", async ({ page }) => {
  const messages = page.getByTestId("chat-message");
  // Scoped by role: the agent quotes the question back in its reply, so a plain
  // text filter matches the answer as well as the question that prompted it.
  const userMessages = page.locator('[data-testid="chat-message"][data-role="user"]');

  await test.step("1. the page opens on the conversation, ready to be typed into", async () => {
    await loadPage(page, AGENT_CHAT);
    // Nothing has to be picked or clicked first: the agent is the conversation.
    await expect(page.getByTestId("chat-input")).toBeVisible();
    await expect(page.getByTestId("chat-sessions")).toBeVisible();
  });

  await test.step("3. its history is already loaded", async () => {
    // The seeded conversation: a question, a tool call, its result, an answer.
    await expect(messages).toHaveCount(4);
    // The rail marks which conversation is open — this one.
    await expect(
      page.getByTestId(`chat-session-${instances.ready}`),
    ).toHaveAttribute("data-active", "true");
  });

  await test.step("4. a tool call and its result render as themselves, not as JSON blobs", async () => {
    const call = page.getByTestId("chat-tool-call");
    await expect(call).toHaveCount(1);
    await expect(call).toHaveAttribute("data-tool-name", "k8s_get_events");

    const result = page.getByTestId("chat-tool-result");
    await expect(result).toHaveCount(1);
    await expect(result).toContainText("liveness probe failed");
  });

  await test.step("5. messages are attributed to who said them", async () => {
    await expect(messages.first()).toHaveAttribute("data-role", "user");
    await expect(messages.last()).toHaveAttribute("data-role", "agent");
  });

  await test.step("6. nothing rules the conversation off from the box it is typed into", async () => {
    // Reported as a defect: a border above the composer with a band of empty space
    // over it, riding across the transcript as the page scrolled underneath. The
    // computed style is the assertion because a rule of the page's own background
    // colour is invisible in a screenshot and still wrong.
    const borderTop = await page
      .getByTestId("chat-composer")
      .evaluate((row) => getComputedStyle(row).borderTopWidth);
    expect(borderTop, "the composer should carry no rule above it").toBe("0px");
  });

  await test.step("7. sending a message adds it to the transcript immediately", async () => {
    await page.getByTestId("chat-input").fill(FIRST_QUESTION);
    await page.getByTestId("chat-send").click();

    // The reader's own words, before the server has said anything. The fixture
    // does not echo them back and neither does the gateway, so this can only pass
    // if the client put them there itself.
    await expect(userMessages.filter({ hasText: FIRST_QUESTION })).toHaveCount(1);
    // Composer clears, so the next message does not start with the last one.
    await expect(page.getByTestId("chat-input")).toHaveValue("");
  });

  await test.step("8. the turn reports itself as running, and offers a way out", async () => {
    await expect(page.getByTestId("chat-cancel")).toBeVisible();
    // The transcript's own line, which is now the only place a turn is reported: the
    // separate lifecycle bar under the composer said the same thing twice and went.
    await expect(page.getByTestId("chat-status")).toBeVisible();
  });

  await test.step("9. the turn's tool call and result arrive", async () => {
    await expect(page.getByTestId("chat-tool-call")).toHaveCount(2);
    await expect(
      page.getByTestId("chat-tool-result").last(),
    ).toContainText("3 pods running in kagent");
  });

  await test.step("10. the streamed reply lands exactly once", async () => {
    // Exact text, not a substring. Streaming appends, and the failure mode that
    // actually happened here was a doubled first chunk ("There There are 3
    // pods…") — which every `toContainText` in this file would have passed.
    await expect(
      messages.last().getByTestId("chat-message-text"),
      "the streamed reply should assemble exactly once",
    ).toHaveText(replyTo(FIRST_QUESTION));
  });

  await test.step("11. the turn finishes, the composer comes back, and the indicator settles", async () => {
    await expect(page.getByTestId("chat-send")).toBeVisible();
    await expect(page.getByTestId("chat-cancel")).toHaveCount(0);
    // Nothing reported once the turn is over: the status line belongs to a turn in
    // flight, so a finished one leaves it with nothing to say.
    await expect(page.getByTestId("chat-status")).toHaveCount(0);
  });

  await test.step("12. a second question behaves exactly like the first", async () => {
    // The report was that further questions behaved the same way — only the agent's
    // replies appeared. So the second turn is driven, not assumed from the first.
    await page.getByTestId("chat-input").fill(SECOND_QUESTION);
    await page.getByTestId("chat-send").click();

    await expect(
      userMessages.filter({ hasText: SECOND_QUESTION }),
      "the second question should be on screen as soon as it is sent, like the first",
    ).toHaveCount(1);

    await expect(page.getByTestId("chat-cancel")).toHaveCount(0);
    await expect(
      messages.last().getByTestId("chat-message-text"),
      "the second reply should assemble exactly once",
    ).toHaveText(replyTo(SECOND_QUESTION));
  });

  await test.step("13. a reload shows the identical conversation, not merely a similar one", async () => {
    // The acceptance the report asked for, and the strongest form of it available:
    // the same messages, saying the same things, in the same order. A count would
    // pass on a transcript that had lost the questions and gained two replies.
    const before = await messages.allInnerTexts();
    // Twelve: the seeded four, plus four for each turn driven above — the
    // question, the tool call, its result and the reply. Stated as a number rather
    // than read from the page, so a transcript that quietly lost the questions
    // cannot define its own expectation.
    expect(before.length, "the seeded four plus four for each of two turns").toBe(12);

    await page.reload();
    await expect(page.getByTestId("chat-input")).toBeVisible();
    await expect(messages).toHaveCount(before.length);
    expect(
      await messages.allInnerTexts(),
      "the reloaded conversation should be the one that was on screen",
    ).toEqual(before);

    // And the reader's own words are among them — which before this fix was the
    // only moment they ever appeared.
    await expect(userMessages.filter({ hasText: FIRST_QUESTION })).toHaveCount(1);
    await expect(userMessages.filter({ hasText: SECOND_QUESTION })).toHaveCount(1);
  });

  await test.step("14. and the composer does not sit on top of the conversation", async () => {
    /*
     * The other half of the same report — "a border above the prompt input which
     * overlaps the rendered conversation". Removing the rule stopped a line being
     * drawn across the transcript; this is about the box itself.
     *
     * The composer is `position: sticky; bottom: 0`, and the transcript scrolls its
     * own sentinel to the foot of the viewport after every turn — so the last
     * message lands directly under the box. Measured at 23px of overlap on a 420px
     * viewport, which is a line of text.
     *
     * The state driven here is the one the page puts *itself* in: a turn, and then
     * whatever the auto-scroll does. Scrolling manually to the very bottom would
     * measure a different position, one where the composer is at its natural place
     * and nothing overlaps whatever the transcript does — which is how an earlier
     * version of this assertion came to pass on the broken build.
     *
     * Boxes, not a screenshot: a few pixels of overlap cover a line of text and are
     * not something a glance at a still will catch.
     */
    await page.setViewportSize({ width: 1440, height: 420 });
    await page.getByTestId("chat-input").fill("one more, to make the page scroll");
    await page.getByTestId("chat-send").click();
    await expect(page.getByTestId("chat-cancel")).toHaveCount(0);

    /*
     * The box that scrolls must end above the box you type in.
     *
     * This used to measure the last message against the composer, which was the right
     * question while the transcript sat in the page's flow and could run underneath it.
     * The transcript owns a scroll box of its own now, so the last message is often
     * legitimately outside the visible area and its position says nothing — the
     * property that survives is that the two regions do not overlap.
     */
    const gap = await page.evaluate(() => {
      const transcript = document.querySelector('[data-testid="chat-transcript"]');
      const composer = document.querySelector('[data-testid="chat-composer"]');
      if (!transcript || !composer) return null;
      const box = transcript.parentElement;
      if (!box) return null;
      return composer.getBoundingClientRect().top - box.getBoundingClientRect().bottom;
    });

    expect(gap, "the transcript and the composer should both be on the page").not.toBeNull();
    expect(
      gap as number,
      "the foot of the conversation should be clear of the composer, not under it",
    ).toBeGreaterThanOrEqual(0);

    await page.setViewportSize({ width: 1440, height: 900 });
  });

  await test.step("15. switching conversations does not leak the previous one", async () => {
    // A sibling instance: the same harness and template, so the rail lists it as
    // another conversation with this agent.
    await page.getByTestId(`chat-session-${SIBLING_OF_READY}`).click();
    await page.waitForURL(new RegExp(`/agents/kagent/${SIBLING_OF_READY}/chat$`));

    await expect(page.getByTestId("chat-empty")).toBeVisible();
    await expect(
      page.getByTestId("chat-message"),
      "the previous conversation's messages should not carry over",
    ).toHaveCount(0);
  });

  await test.step("16. a suspended agent still takes a message, and resuming is the page's job", async () => {
    /*
     * Suspended is not "cannot answer" — it is "not resumed yet".
     *
     * The gateway does refuse a message for a suspended instance, and the composer used
     * to be disabled because of it. But the reader's intention is unambiguous: they
     * typed something and pressed send. Making them find a Resume control first —
     * having just been told the agent gave its worker back at the end of the last turn
     * — is a step the page can take for them rather than a detour on every message.
     */
    await expect(page.getByTestId("chat-input")).toBeEnabled();
    // And no alert claiming it cannot answer, because it can.
    await expect(page.getByTestId("chat-not-ready")).toHaveCount(0);
  });
});

/**
 * Scrolling away from the foot offers a way back, and sending returns there.
 *
 * Both were broken by the same thing and neither showed up as a failure. The
 * transcript used to be scrolled by the page, so the sentinel that decides whether the
 * reader is at the bottom was observed against the viewport. Once the transcript owned
 * its own scroll box that question was about the wrong element: the sentinel stayed
 * inside the window whether or not the reader had scrolled away from it, so the
 * observer went on reporting "at the bottom", the button never appeared, and following
 * a turn scrolled whatever ancestor `scrollIntoView` happened to pick.
 *
 * Asserted by scrolling the box rather than by looking at it, because the failure is
 * invisible in a screenshot — the transcript looks the same either way.
 */
test("chat: leaving the foot of a conversation offers a way back to it", async ({ page }) => {
  // A short window, so the conversation genuinely overflows its box rather than being
  // forced to by a style this test applied — the second measures the test's own hack.
  await page.setViewportSize({ width: 1280, height: 460 });
  await loadPage(page, AGENT_CHAT);
  await expect(page.getByTestId("chat-message").first()).toBeVisible({ timeout: 30_000 });

  const box = page.getByTestId("chat-transcript").locator("xpath=..");
  const button = page.getByTestId("chat-scroll-bottom");

  await test.step("1. at the foot, there is nothing to offer", async () => {
    await expect(button).toHaveCount(0);
  });

  await test.step("2. scrolling up offers the way back", async () => {
    await box.evaluate((node) => node.scrollTo({ top: 0 }));
    await expect(button).toBeVisible({ timeout: 10_000 });
  });

  await test.step("3. and it takes them there", async () => {
    await button.click();
    /*
     * The button going is the assertion, not a scroll offset.
     *
     * It is set by the same observer that decides whether the reader is at the foot,
     * so it going away *is* the transcript reporting that they are — and it is the
     * thing a reader sees. Measuring the offset instead reads a moving target: a
     * transcript that re-reads itself on a timer changes height under the assertion.
     */
    await expect(button).toHaveCount(0, { timeout: 10_000 });
  });
});

/**
 * The composer does not move when the conversation changes under it.
 *
 * The panel was `grid-template-rows: auto 1fr auto`, which assumes exactly three
 * children — and it has a varying number, because the notices above the transcript come
 * and go with the conversation's state. A fourth child pushed the `1fr` onto a
 * different row, so the transcript stopped being the part that grows and the composer
 * moved instead: a visible flicker when clicking between conversations, worst on short
 * ones where the composer is not pinned to the foot of the viewport anyway.
 *
 * Measured rather than eyeballed, because a couple of pixels is exactly the size of the
 * problem and is invisible in a screenshot.
 */
test("chat: the composer stays put when switching conversations", async ({ page }) => {
  await loadPage(page, AGENT_CHAT);
  await expect(page.getByTestId("chat-composer")).toBeVisible({ timeout: 30_000 });
  // After the transcript has arrived: measuring mid-read compares a loading state with a
  // loaded one, which is a different question from the one this asks.
  await expect(page.getByTestId("chat-message").first()).toBeVisible({ timeout: 30_000 });

  const composerTop = () =>
    page.getByTestId("chat-composer").evaluate((node) => node.getBoundingClientRect().top);
  const before = await composerTop();

  const rail = page.getByTestId("chat-sessions");
  await rail.locator(`a[data-testid="chat-session-${SIBLING_OF_READY}"]`).click();
  await page.waitForURL(new RegExp(`/agents/kagent/${SIBLING_OF_READY}/chat$`));
  await expect(page.getByTestId("chat-composer")).toBeVisible();
  // Settled, not mid-transition — the assertion is about where it ends up.
  await page.waitForTimeout(1000);

  expect(
    Math.abs((await composerTop()) - before),
    "switching conversations should not move the message box",
  ).toBeLessThanOrEqual(1);
});

/**
 * An agent's answer is Markdown, and this page renders it.
 *
 * Worth its own test because the failure is silent in both directions: a broken
 * renderer shows `**3 pods**` with the asterisks intact, which still reads as an
 * answer, and a renderer given raw HTML would run it. So this asserts the elements
 * exist — not the text, which is identical either way.
 */
test("chat: an agent's Markdown renders as elements, not as characters", async ({ page }) => {
  await loadPage(page, AGENT_CHAT);
  // The seeded history, before typing: sending early would race the load rather than
  // the render, and the assertion below is about the render.
  await expect(page.getByTestId("chat-message")).toHaveCount(4);

  await page.getByTestId("chat-input").fill("How many pods are running?");
  await expect(page.getByTestId("chat-send")).toBeEnabled();
  await page.getByTestId("chat-send").click();

  // The turn runs, then finishes. Both waits matter: without the first this can read the
  // transcript before the reply exists, and without the second it can read a half-streamed
  // list whose item count is whatever had arrived.
  await expect(page.getByTestId("chat-cancel")).toBeVisible();
  await expect(page.getByTestId("chat-cancel")).toHaveCount(0);

  const answer = page.getByTestId("chat-message").last().getByTestId("chat-message-text");

  // Emphasis and code became elements rather than surviving as punctuation.
  await expect(answer.locator("strong")).toHaveText("3 pods");
  await expect(answer.locator("code").first()).toHaveText("kagent");

  // The list is a list, so it reads as one to anything that is not a pair of eyes.
  await expect(answer.locator("ul li")).toHaveCount(3);

  // And the source characters are gone: this is what fails when the renderer is
  // bypassed and the raw Markdown is printed instead.
  await expect(answer).not.toContainText("**");
});
