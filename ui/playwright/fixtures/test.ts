/**
 * The shared fixture. Every spec imports `{ test, expect }` from here rather than
 * from `@playwright/test`, so they all get the same guard: anything the app logs
 * as an error, or throws and fails to catch, is collected and asserted on at the
 * end of the test.
 *
 * That guard is why a spec can trust its own green: a page can satisfy every
 * assertion while throwing in an effect, and without this the suite would not
 * notice.
 */

import { test as base, expect, type Page } from "@playwright/test";

/**
 * Console output a spec deliberately provokes, which is evidence rather than a defect.
 *
 * Kept as short as it can be. It used to forgive the browser's log of a 500, from
 * back when the error scenario answered one — but the API is gRPC-Web served by a
 * substituted transport now, so a failed call never becomes a failed HTTP request and
 * that line cannot appear. An allowance for noise that can no longer occur costs
 * nothing directly and misleads twice: it reads as evidence that HTTP failures still
 * happen here, and it widens the guard for every spec at once. Declare noise on the
 * spec that earns it instead — the one entry below is here only because it belongs to
 * no spec.
 */
const EXPECTED_NOISE: RegExp[] = [
  /*
   * Firefox, under parallel load, logging its own mock harness rather than the app.
   *
   * Firefox occasionally routes a request with an empty URL through the service
   * worker while the worker is still taking over the page, and MSW's fetch handler
   * has nothing to answer it with. It appears in whichever spec happens to be
   * starting at the time, which is why it is here and not on one spec: it belongs to
   * the harness, not to any journey.
   *
   * The pattern names `mockServiceWorker.js` deliberately. A real failure to load a
   * real asset produces a message naming that asset, and still fails — the allowance
   * is only for the mock worker reporting on itself, which cannot happen in a build
   * at all, since the Dockerfile deletes that file.
   */
  /A ServiceWorker intercepted the request and encountered an unexpected error[\s\S]*mockServiceWorker\.js/,
];

export interface AppErrors {
  /** Console errors and uncaught exceptions seen so far, deliberate ones removed. */
  readonly messages: string[];
}

/**
 * Extra console output one spec provokes on purpose.
 *
 * Declared per spec with `test.use({ expectedNoise: [...] })` rather than added to
 * the shared list, so an allowance stays where its justification is. A spec that
 * drives a 404 on purpose needs that 404 forgiven; the rest of the suite must still
 * fail on one, because a 404 is also what a missing asset looks like — this repository
 * has already shipped a service worker to production that way once.
 */
export type ExpectedNoise = readonly RegExp[];

export const test = base.extend<{
  appErrors: AppErrors;
  expectedNoise: ExpectedNoise;
}>({
  expectedNoise: [[], { option: true }],

  appErrors: [
    async (
      { page, expectedNoise }: { page: Page; expectedNoise: ExpectedNoise },
      run,
    ) => {
      const messages: string[] = [];

      page.on("console", (message) => {
        if (message.type() !== "error") return;
        const text = message.text();
        const allowed = [...EXPECTED_NOISE, ...expectedNoise];
        if (allowed.some((pattern) => pattern.test(text))) return;
        messages.push(text);
      });
      page.on("pageerror", (error) => messages.push(`uncaught: ${error.message}`));

      await run({ messages });

      // Asserted here rather than in each spec so no spec can forget to.
      expect(
        messages,
        `the app logged errors:\n${messages.join("\n")}`,
      ).toEqual([]);
    },
    { auto: true },
  ],
});

export { expect };
