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

export interface AppErrors {
  /** Console errors and uncaught exceptions seen so far, deliberate ones removed. */
  readonly messages: string[];
}

/**
 * Extra console output one spec provokes on purpose.
 *
 * Declared per spec with `test.use({ expectedNoise: [...] })`, never for the whole
 * suite, so an allowance stays where its justification is. A spec that
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
        if (expectedNoise.some((pattern) => pattern.test(text))) return;
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
