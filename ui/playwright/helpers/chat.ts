/**
 * Driving a turn, and knowing when it is over.
 *
 * `expect(chat-cancel).toHaveCount(0)` is a wait wearing an assertion's clothes, and
 * its report reads "cancel should not be here" when the reply had simply not finished
 * (#2610). `MockChatClient` publishes a tally of turns started and finished — as
 * `src/mocks/transport.ts` does for RPC calls — so the wait can be on the event, and
 * the assertion after it on the page, at the default budget.
 */

import { expect, type Page } from "@playwright/test";

/** Where `src/api/chat/mockChatClient.ts` publishes what it is doing. */
const PROPERTY = "__kagentMockChat";

/** The tally, as the page holds it. */
interface Turns {
  started: number;
  finished: number;
}

/**
 * A sent turn, identified by the count of turns finished before it went — which is
 * what keeps the wait below about this turn and not the previous one.
 */
export interface SentTurn {
  readonly finishedBefore: number;
}

function readTurns(page: Page): Promise<Turns | null> {
  return page.evaluate(
    (property) =>
      (window as unknown as Record<string, Turns | undefined>)[property] ?? null,
    PROPERTY,
  );
}

/**
 * The token for the turn about to be sent. Polled because the counters are hung off
 * the chat client, which is built the first time a conversation is read.
 */
export async function beginTurn(page: Page): Promise<SentTurn> {
  await expect
    .poll(async () => await readTurns(page), {
      message:
        `The chat fixture published nothing on window.${PROPERTY}. Either the app is ` +
        `not running in mock mode, or this page never opened a conversation.`,
    })
    .not.toBeNull();

  const turns = await readTurns(page);
  return { finishedBefore: turns!.finished };
}

/** Types a message and sends it, returning the token its turn is waited on with. */
export async function sendMessage(page: Page, text: string): Promise<SentTurn> {
  const turn = await beginTurn(page);
  await page.getByTestId("chat-input").fill(text);
  await page.getByTestId("chat-send").click();
  return turn;
}

/**
 * Waits for the turn to stop streaming, then asserts the page has noticed. Both
 * halves matter: the first alone races the script, the second alone proves nothing.
 */
export async function expectTurnFinished(page: Page, turn: SentTurn): Promise<void> {
  await page.waitForFunction(
    ({ property, after }) => {
      const turns = (window as unknown as Record<string, Turns | undefined>)[property];
      return turns !== undefined && turns.finished > after;
    },
    { property: PROPERTY, after: turn.finishedBefore },
  );

  await expect(
    page.getByTestId("chat-cancel"),
    "the composer should come back once the turn is over",
  ).toHaveCount(0);
}

/**
 * Waits for the transcript to have drawn what it loaded, before anything in it is
 * pressed.
 *
 * The seeded conversation's reply carries a mermaid diagram, which renders after the
 * page does — its import alone takes seconds on the dev server — and replaces its
 * source listing with a taller SVG. Everything below it moves down until the
 * transcript re-pins to its foot on the next frame, and a press that straddles that
 * moment lands its button-down on the control and its button-up beside it: the
 * control takes the focus and does nothing. A failed turn's Retry went unpressed on a
 * loaded Firefox run that way. The composer sits outside the transcript and is not
 * moved, so sending needs no wait.
 */
export async function expectTranscriptSettled(page: Page): Promise<void> {
  // The transcript replaces its loading skeleton in the commit that renders the history,
  // pending diagrams included, so a count of none means drawn rather than not yet
  // mounted. Only a message sent before the history arrived opens it sooner, which is
  // why this is for a conversation whose history is already on screen.
  await expect(page.getByTestId("chat-transcript")).toBeVisible();
  await expect(page.getByTestId("chat-mermaid-pending")).toHaveCount(0);
}

/** Sends a message and waits out the turn it starts, asserting nothing in between. */
export async function sendAndAwaitTurn(page: Page, text: string): Promise<void> {
  await expectTurnFinished(page, await sendMessage(page, text));
}
