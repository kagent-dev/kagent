/**
 * Page-level drivers.
 *
 * These assert on rendered DOM through roles and test ids, never on prose, so
 * they survive the page rebuilds still ahead. Where a test id is used it is one
 * the app already ships for the purpose.
 */

import { expect, test, type Locator, type Page } from "@playwright/test";

import { LIVE_PROJECT } from "../../playwright.config";

/** Routes the suite drives. Mirrors `src/router/routes.ts`. */
export const routes = {
  dashboard: "/",
  login: "/login",
  agents: "/agents",
  models: "/models",
  modelNew: "/models/new",
  mcpServers: "/mcp",
  mcpServerNew: "/mcp/new",
  prompts: "/prompts",
  promptNew: "/prompts/new",
  substrate: "/substrate",
  /* The templates list is a tab of the agents page now. The old address still
     resolves — it redirects here — but a test should go where the reader goes. */
  agentTemplates: "/agents?tab=templates",
  harnesses: "/agents?tab=harnesses",
  harnessNew: "/harnesses/new",
  agentTemplateNew: "/agent-templates/new",
} as const;

/**
 * The fixture conversations the suite drives, by the id the API addresses them with.
 *
 * An `AgentInstance` is one conversation, addressed as `(namespace, id)` where the
 * id is a UUID — so these are the ids from `src/mocks/fixtures.ts`, named here for
 * what each one is *for* rather than pasted into every spec.
 */
export const instances = {
  /** Ready, named by the reader, and the one with a seeded transcript behind it. */
  ready: "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
  /** Suspended, so it can be resumed. Unnamed, so it renders as untitled. */
  suspended: "b28e4f13-5c66-4d90-8f2b-77a1e9c34d05",
  /** Failed, with a reason the conversation's record page shows. */
  failed: "d4b02f87-3a55-4c18-9e6b-1f70c9a8e332",
  /** Somebody else's: listable under its agent, and not openable. */
  someoneElses: "8e5f2b09-6c14-4a7d-83b0-9d1c7e40f5a6",
} as const;

/**
 * The fixture agents, which are `(AgentTemplate, Harness)` pairs.
 *
 * An agent is named by its template, so a pair is written the way the address reads:
 * the template, then the harness that runs it.
 */
export const agents = {
  /** `instances.ready` and its siblings are conversations with this one. */
  k8s: { template: "k8s-agent-7f3a91c", harness: "k8s-agent" },
  /**
   * One template, two harnesses — so two agents that share a name.
   *
   * The pair that makes an agent a pair rather than a template: keyed on the
   * template alone, these two would be one row and their conversations would merge.
   */
  sharedOnK8s: { template: "shared-brain", harness: "k8s-agent" },
  sharedOnFastLane: { template: "shared-brain", harness: "fast-lane" },
  /** Admitted, but with no successful revision — so no conversation can start. */
  preparing: { template: "support-triage-2b91d0e", harness: "support-triage" },
} as const;

/** Where one agent lives: its namespace, its template, and the harness it runs on. */
export const agentPage = (
  agent: { template: string; harness: string },
  namespace = "kagent",
) => `/agents/${namespace}/${agent.template}/on/${agent.harness}`;

/**
 * Where clicking an agent's name goes: a conversation that does not exist yet.
 *
 * Distinct from `agentPage`, which is the agent's own page listing what it already
 * has. Nothing is created until the first message is sent, which is why the two are
 * different addresses rather than the same one behaving differently.
 */
export const agentNewChat = (
  agent: { template: string; harness: string },
  namespace = "kagent",
) => `${agentPage(agent, namespace)}/new`;

/**
 * The other conversation with the same agent that the caller can actually see.
 *
 * Cut from the same `(Harness, AgentTemplate)` pair as `instances.ready`, so the rail
 * lists it as a sibling. It is the *suspended* one because the other siblings in the
 * fixtures were created by somebody else, and the list returns only the caller's own
 * instances unless `all_creators` is asked for — a sibling behind that switch would
 * not be in the rail at all.
 */
export const SIBLING_OF_READY = instances.suspended;

/** Where one agent's conversation lives. */
export const agentChat = (id: string) =>
  `/agents/${id}/chat`;

/** Where one agent's record lives. */
export const agentDetail = (id: string) =>
  `/agents/${id}`;

/**
 * How the mock backend should behave for a navigation.
 *
 * The app reads this from the query string on every request (see
 * `src/mocks/scenario.ts`), which is what makes the loading, empty and failure
 * paths drivable from a test without a second build or a stubbed module.
 */
export type MockScenario = "ok" | "empty" | "error" | "slow";

/**
 * Adds the scenario to a path.
 *
 * Always pass one explicitly: the app remembers the last scenario for the
 * browsing session, so a bare path inherits whatever the previous step asked
 * for — which is convenient in a browser and a trap in a test.
 */
export function withScenario(path: string, scenario: MockScenario): string {
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}mock=${scenario}`;
}

/** Navigates, then optionally waits for the page's heading to confirm it arrived. */
export async function loadPage(
  page: Page,
  path: string,
  options: { scenario?: MockScenario; title?: string } = {},
): Promise<void> {
  const { scenario = "ok", title } = options;
  await page.goto(withScenario(path, scenario));
  if (title) await expectPageTitle(page, title);
}

/** The current page's heading, as rendered by the shared page frame. */
export function pageTitle(page: Page): Locator {
  return page.getByTestId("page-title");
}

export async function expectPageTitle(page: Page, title: string): Promise<void> {
  await expect(pageTitle(page)).toHaveText(title);
}

/** A table row containing the given text — the row a user would point at. */
export function rowNamed(page: Page, text: string): Locator {
  return page.getByRole("row").filter({ hasText: text });
}

/** Data rows only, excluding the header row and any placeholder row. */
export function dataRows(page: Page): Locator {
  return page.locator("tbody tr.ant-table-row");
}

/**
 * How long one read inside a journey may take, which is not how long the journey may.
 *
 * These specs asked for sixty seconds an assertion while the mock lane's whole
 * `LIFECYCLE_TIMEOUT` is sixty — so an assertion could never exhaust its own budget, and
 * a broken one was reported as "Test timeout of 60000ms exceeded" rather than by name.
 * The numbers were sized for the live budget and inherited unchanged by the mock run.
 * Same argument as `PRESS_TIMEOUT`: when this is what failed, this should be what says
 * so.
 */
export const READ_TIMEOUT = process.env.UI_LOOP_LIVE === "true" ? 60_000 : 20_000;


/** A navigation-sized budget, for the app booting rather than for what it rendered. */
const APP_BOOT_TIMEOUT = 15_000;

/**
 * Whether this run is against a cluster rather than the fixtures.
 *
 * For the few places where the two backends differ in kind and not merely in speed —
 * a reload restarts the in-browser fixture backend, and a controller fills a status
 * only on a real one. A spec branching on this is saying so out loud, which is better
 * than a claim that quietly means something different on each.
 */
export function isLiveRun(): boolean {
  return test.info().project.name === LIVE_PROJECT;
}

/**
 * Navigates, for a spec in `shared/` that runs against either backend.
 *
 * `loadPage` cannot: it appends a `?mock=` scenario, which is meaningless to a cluster
 * and misleading in a live trace. So the scenario is added only where there is a mock
 * backend to read it, and the wait is on the shell rather than on a heading, a live
 * page taking longer to have one.
 */
export async function loadApp(page: Page, path: string): Promise<void> {
  const live = isLiveRun();
  await page.goto(live ? path : withScenario(path, "ok"), {
    waitUntil: "domcontentloaded",
  });
  await expect(page.getByTestId("app-content")).toBeVisible({
    timeout: live ? 60_000 : APP_BOOT_TIMEOUT,
  });
}

/**
 * Fails when the page is reporting that it could not reach the backend.
 *
 * Worth calling before asserting on content: the alternative is a failure reading "the
 * table is empty" when the truth is "the backend did not answer" — the same distinction
 * the app itself is careful about.
 *
 * **Call it after something that proves the read landed**, never straight after a
 * navigation. It is a count taken once, and `loadApp` returns on the shell: asked in
 * that gap it passes on every page, including one whose read fails a moment later. A
 * list's `expectListLoaded` or its own summary is the signal to put in front of it —
 * `live/pages.spec.ts` keeps the table of what each page draws.
 */
export async function expectNoLoadFailure(page: Page): Promise<void> {
  /*
   * Both families, because the app says it two ways. A page with room for an alert draws
   * `<thing>-error`; one with only a line of copy where the data goes draws
   * `<thing>-unavailable` — the dashboard's recent list, the schedules table, the tools
   * chart, the schedule history, each rendered on `error` and nothing else. Matching the
   * first alone, this passed on a dashboard that could not reach the controller.
   */
  const alerts = page.locator(
    '[data-testid$="-error"], [data-testid$="-unavailable"]',
  );
  const count = await alerts.count();
  if (count === 0) return;

  const texts = await alerts.allInnerTexts();
  expect(count, `the page reported a failure to load: ${texts.join(" | ")}`).toBe(0);
}

/**
 * Resolves once the app is on screen and no loading indicator is left on it.
 *
 * Waiting for a spinner to be absent is also true of a page that has not started
 * rendering, so after a `goto` or a `reload` this returned at once and the next
 * assertion's own five seconds became the app's boot budget.
 */
export async function expectSettled(page: Page): Promise<void> {
  await expect(page.locator("#root")).not.toBeEmpty({ timeout: APP_BOOT_TIMEOUT });
  await expect(page.locator(".ant-spin-spinning")).toHaveCount(0);
}

/**
 * A name no human would choose, carrying the run that made it.
 *
 * A spec that creates on a real cluster deletes what it made, but a run killed between
 * the two cannot — so the name has to be enough for a person to identify the litter
 * without the harness. Unique per run on the fixtures too, where it costs nothing and
 * keeps a shared spec reading the same on both backends.
 */
export const throwawayName = (label: string): string =>
  `e2e-live-${label}-${process.pid}-${Date.now().toString(36)}`;

/**
 * Narrows a list page to one name, using the search box the page already offers.
 *
 * Needed by any spec that creates a row and then reads it back on a list it did not
 * seed. These tables page at 25, so on a cluster whose list already fills a page the
 * new row is on page two — present, correct, and invisible to a locator. Searching for
 * a name only this run could have made puts it on screen wherever it landed, and is
 * what a reader looking for their own resource would do.
 *
 * Client-side, over every row fetched, so it is not a second read that could disagree
 * with the first.
 *
 * @param list the page's test-id prefix — `models` for `models-filters-search`.
 */
export async function searchList(page: Page, list: string, term: string): Promise<void> {
  await page.getByTestId(`${list}-filters-search`).fill(term);
}

/**
 * Asserts how many rows the whole list holds — which is not how many are on screen.
 *
 * Read off the `<list>-summary` line ("3 of 27 configurations"), whose second number is
 * computed from every row fetched rather than from the page being shown. Counting
 * `dataRows` instead answers a different question on any list longer than 25, and
 * answers it wrongly while `searchList` is narrowing the table to one row.
 *
 * The summary renders only after a successful load, so waiting for it to say a number
 * also distinguishes "the list holds that many" from "the read failed".
 */
export async function expectListTotal(
  page: Page,
  list: string,
  total: number,
  timeout = READ_TIMEOUT,
): Promise<void> {
  await expect(page.getByTestId(`${list}-summary`)).toContainText(
    new RegExp(`\\bof ${total}\\b`),
    { timeout },
  );
}

/**
 * The list's total, once the summary has stopped moving.
 *
 * Read once, the summary is not the signal it looks like. Every list draws it on
 * `!error && !isLoading`, and SWR reports `isLoading` false on the first paint — the
 * fetcher runs in an effect, after it — so a page that has not asked anything yet draws
 * "0 of 0" for a frame. Measured on the harnesses tab: a total of 0 read off a tab
 * holding four, and the journey then asserted "one more than nothing".
 *
 * So it is read twice and has to agree with itself. A list still arriving disagrees, and
 * one that is genuinely empty says 0 twice — which is the distinction "a list still
 * fetching has no rows either" was always about.
 */
async function settledTotal(page: Page, list: string): Promise<number> {
  const summary = page.getByTestId(`${list}-summary`);
  await expect(summary).toContainText(/\bof \d+\b/, { timeout: READ_TIMEOUT });

  let previous: number | undefined;
  let total = 0;
  await expect(async () => {
    const text = (await summary.textContent()) ?? "";
    const read = /\bof (\d+)\b/.exec(text)?.[1];
    expect(read, `no total could be read from "${text}"`).toBeDefined();
    const value = Number(read);
    const agreed = previous === value;
    previous = value;
    total = value;
    expect(agreed, `still moving, at ${value}`).toBe(true);
  }).toPass({ timeout: READ_TIMEOUT, intervals: [250] });

  return total;
}

/**
 * Resolves once the list has answered, which is what makes a row count mean anything.
 *
 * A list still fetching has no rows either, so a `count()` taken too early reads zero
 * and every question asked of it gets the answer "not there" — including a cleanup
 * asking whether there is anything left to delete.
 */
export async function expectListLoaded(page: Page, list: string): Promise<void> {
  await settledTotal(page, list);
}

/** What `expectListTotal` would be reading now, for a count taken before a change. */
export async function readListTotal(page: Page, list: string): Promise<number> {
  return settledTotal(page, list);
}
