import { type Locator, type Page } from "@playwright/test";
import { test, expect } from "../fixtures/test";
import { dataRows, expectListLoaded, expectNoLoadFailure, loadApp } from "../helpers/app";
import { liveRoutes } from "./helpers/live";

/**
 * Every page, against a real controller.
 *
 * The mock suite already asserts each page's loading, empty and failure states,
 * which it can because it can ask the mock backend for them. A real cluster cannot
 * be asked for a 500, so this spec asserts the thing the mock suite structurally
 * cannot: that the pages work against what a controller actually sends.
 *
 * That has not been a formality on this project. Every defect found by pointing the
 * app at a real backend was a place where a fixture had taught a shape the
 * controller does not use — a null collection where an array was declared, a create
 * response wrapped one way in the fixtures and another by the API, a resource name
 * sent where a bare name belonged. None of them were visible to a green mock suite.
 */

/**
 * What each page draws once it has an answer — success *or* failure, which `loadApp`
 * does not wait for, it waiting for the shell.
 *
 * Without it the sweep asked its question before the page had asked the controller
 * anything: nothing had rendered either way, so every page passed, and a read that
 * failed a second later passed with it. Demonstrated by delaying one read and then
 * failing it — the guard saw nothing and the alert arrived after the step had moved on.
 *
 * The failure states are in here beside the good ones deliberately. Gated on success
 * alone this waits out sixty seconds and reports "no summary", where what happened is on
 * screen and has a message on it: `expectNoLoadFailure` reads it out instead.
 *
 * Each list's summary is the signal rather than its rows, because it renders "N of M"
 * for a read that succeeded — zero rows included — and nothing at all while one is in
 * flight.
 */
/** Any of these ids being on screen, whichever page they belong to. */
const anyOf = (page: Page, ...testIds: string[]) =>
  page.locator(testIds.map((id) => `[data-testid="${id}"]`).join(", "));

/** A list's summary, which only a successful read draws, or the alert a failure does. */
const listAnswered = (page: Page, list: string) =>
  anyOf(page, `${list}-summary`, `${list}-error`);

const answered: Record<
  Exclude<keyof typeof liveRoutes, "agentTemplateNew">,
  (page: Page) => Locator
> = {
  dashboard: (page) =>
    anyOf(
      page,
      "recent-agents",
      "recent-agents-empty",
      "recent-agents-unavailable",
      "dashboard-error",
    ),
  agents: (page) => listAnswered(page, "agents"),
  agentTemplates: (page) => listAnswered(page, "templates"),
  models: (page) => listAnswered(page, "models"),
  mcpServers: (page) => listAnswered(page, "mcp-servers"),
  prompts: (page) => listAnswered(page, "prompts"),
  // No summary on this one, so its table says it: rows, or "No schedules were found."
  schedules: (page) =>
    anyOf(page, "schedules-empty", "schedules-error").or(
      page.locator('[data-testid="schedules-table"] tbody tr.ant-table-row'),
    ),
  // The tiles draw an em-dash where the number goes until the read lands, so a digit is
  // what says it has. `live/substrate.spec.ts` owns what the page then reports.
  substrate: (page) =>
    page
      .getByTestId("substrate-stat-actors-value")
      .filter({ hasText: /\d/ })
      .or(page.getByTestId("substrate-inventory-error")),
};

test("live: every page loads against the cluster and reports no failure", async ({
  page,
}) => {
  /*
   * What the controller refused, as it arrives.
   *
   * The rendering is a second-hand account of a read and it lags in both directions: a
   * page that has just asked draws neither its summary nor its error, and the summary
   * can be on screen for a frame before the answer lands. A check made in that gap
   * passes on a read that fails a moment later — measured here by delaying one read to
   * 1.5s and failing it, which the shell-only order and the summary-gated order both
   * passed while the page ended up saying "Could not load agents · HTTP 500".
   *
   * A response has no such gap. Asserted per page for what has landed by then, and
   * again at the end — which is the one that catches the case above: measured, the
   * agents page's gate resolved with nothing refused yet, and the two 500s were in here
   * three seconds later. Each is labelled with the page in hand when it landed, which
   * for a late one is the page after the one that asked.
   */
  const refused: string[] = [];
  let current = "";
  page.on("response", (response) => {
    if (response.ok() || !response.url().includes("/api/")) return;
    refused.push(`${current}: ${response.status()} ${new URL(response.url()).pathname}`);
  });

  for (const [name, path] of Object.entries(liveRoutes)) {
    if (name === "agentTemplateNew") continue; // A form, covered by the lifecycle spec.

    await test.step(`${name} (${path})`, async () => {
      current = name;
      await loadApp(page, path);
      // The page drew something, either way — see `answered`. A route added to
      // `liveRoutes` without an entry there fails to compile rather than sweeping past.
      await expect(
        answered[name as keyof typeof answered](page).first(),
        `${name} never came back, either way`,
      ).toBeVisible({ timeout: 60_000 });
      // And what it drew was not a failure. The distinction worth keeping: a page that
      // could not reach the controller must not be read as a page with nothing on it.
      await expectNoLoadFailure(page);
      expect(refused, "the controller refused a read").toEqual([]);
    });
  }

  // The reads that failed late, each named by the page that asked.
  expect(refused, "a read failed after its page had been checked").toEqual([]);
});

test("live: the agents the cluster installed are listed, each on its harness", async ({
  page,
}) => {
  await loadApp(page, liveRoutes.agents);
  await expectListLoaded(page, "agents");
  await expectNoLoadFailure(page);

  await test.step("the install's own agents are present", async () => {
    // Off the summary: it counts every row fetched rather than the 25 on screen, and
    // "some" rather than a number keeps this from breaking when the chart's default
    // set changes — while still failing if the list came back empty.
    await expect(page.getByTestId("agents-summary")).toContainText(/\bof [1-9]\d*\b/, {
      timeout: 60_000,
    });
  });

  await test.step("each row names the harness its agent runs on", async () => {
    // An agent is a (template, harness) pair the controller materialises, so a row
    // naming no harness is a pair that did not resolve. Read off the element holding
    // the harness and nothing else: the row carries the namespace too, and on every
    // cluster this runs against both are `kagent`.
    const harnesses = page.locator('[data-testid^="agent-harness-"]');
    await expect(harnesses.first()).toBeVisible({ timeout: 60_000 });
    for (const harness of await harnesses.allTextContents()) {
      expect(harness.trim(), "an agent row named no harness").not.toBe("");
    }
  });
});

test("live: the models the cluster installed are listed with their provider", async ({
  page,
}) => {
  await loadApp(page, liveRoutes.models);
  await expectListLoaded(page, "models");
  await expectNoLoadFailure(page);

  await expect(page.getByTestId("models-summary")).toContainText(/\bof [1-9]\d*\b/, {
    timeout: 60_000,
  });

  // The provider is the column most easily left blank: the row renders its tag whether
  // or not `spec.provider` came back, so the claim is that the tag says something.
  const provider = dataRows(page).first().locator(".ant-tag").first();
  await expect(provider, "the first model row named no provider").toHaveText(/\S/);
});

test("live: tool servers report the tools they discovered", async ({ page }) => {
  await loadApp(page, liveRoutes.mcpServers);
  await expectListLoaded(page, "mcp-servers");
  await expectNoLoadFailure(page);

  await test.step("the summary counts servers and tools", async () => {
    /*
     * The numbers, because the words are static: "N of M servers · K tools" says
     * "server" on a page that counted nothing, which is what this asserted before.
     *
     * Both are derived — the servers from the rows, the tools by summing what each
     * discovered — and only the server count is required to be non-zero. Discovery is
     * asynchronous (#2849), so a freshly installed cluster reporting zero tools for a
     * while is telling the truth.
     */
    const summary = page.getByTestId("mcp-servers-summary");
    await expect(summary).toContainText(/\bof [1-9]\d* servers?\b/, { timeout: 60_000 });
    await expect(summary).toContainText(/·\s*\d+ tools?\b/);
  });
});
