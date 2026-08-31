import type { Locator } from "@playwright/test";

/**
 * Reading colours that are still on their way somewhere.
 *
 * antd transitions a table cell's background over 200ms, so `getComputedStyle` taken in
 * the same tick as the hover or press that triggered it returns the colour being left
 * rather than the colour being reached. Measured on an idle machine: `transition:
 * background-color 0.2s`, at rest `rgba(0, 0, 0, 0)`, immediately after `mouse.down()`
 * still `rgba(0, 0, 0, 0)`, and only 400ms later the pressed `rgba(109, 40, 217, 0.3)`.
 *
 * Sampled once, that produced two different untruths. A test asserting the colour
 * *changed* was comparing two unpainted frames, and passed only because they happened
 * to differ for an unrelated reason — then failed outright under a full parallel run,
 * which is how this was found. A test asserting the colour did *not* change passed
 * without ever having looked at the state it was about.
 *
 * So a claim that something changes polls until it does, and a claim that nothing
 * changes waits the transition out first. There is no event for a transition that
 * never starts, which is why the second one is a duration and not a wait for a signal.
 */

/** Comfortably longer than the 200ms transition, with a frame to paint in. */
const SETTLE_MS = 350;

/** What the element looks like right now, mid-transition or not. */
export function paint(locator: Locator) {
  return locator.evaluate((el) => {
    const style = getComputedStyle(el);
    return {
      background: style.backgroundColor,
      shadow: style.boxShadow,
      colour: style.color,
    };
  });
}

/** What the element looks like once it has stopped moving. */
export async function settledPaint(locator: Locator) {
  await locator.page().waitForTimeout(SETTLE_MS);
  return paint(locator);
}

/** Just the background, for polling towards a colour. */
export function background(locator: Locator) {
  return locator.evaluate((el) => getComputedStyle(el).backgroundColor);
}
