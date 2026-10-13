import { expect, type Page } from "@playwright/test";

/**
 * Helpers for the live suite.
 *
 * The mock suite's helpers are built around `?mock=` scenarios, which is the one
 * thing a real controller cannot be told to do — there is no way to ask a cluster
 * for a 500. So the live specs assert what a cluster genuinely produces, and these
 * are the parts of that worth sharing.
 */

/** Where each page lives, so a renamed route breaks in one place. */
export const liveRoutes = {
  dashboard: "/",
  agents: "/agents",
  agentNew: "/agents/new",
  models: "/models",
  mcpServers: "/mcp",
  prompts: "/prompts",
  substrate: "/substrate",
} as const;

/**
 * Loads a page and waits for the app shell, not for the network to fall quiet.
 *
 * `networkidle` is the wrong signal against a real backend: a page that polls, or
 * an agent whose status is still reconciling, may never produce a quiet network,
 * and the wait would time out on a page that rendered correctly seconds earlier.
 */
export async function loadLive(page: Page, path: string): Promise<void> {
  await page.goto(path, { waitUntil: "domcontentloaded" });
  await page.waitForSelector('[data-testid="app-content"]', { timeout: 60_000 });
}

/**
 * Fails when the page is reporting that it could not reach the backend.
 *
 * Called by every spec before asserting on content, because the alternative is a
 * failure that reads as "the table is empty" when the truth is "the cluster did not
 * answer" — the same distinction the app itself is careful about.
 */
export async function expectNoLoadFailure(page: Page): Promise<void> {
  const alerts = page.locator('[data-testid$="-error"]');
  const count = await alerts.count();
  if (count === 0) return;

  const texts = await alerts.allInnerTexts();
  expect(
    count,
    `the page reported a failure to load: ${texts.join(" | ")}`,
  ).toBe(0);
}

/** A table row containing `text`. */
export const rowNamed = (page: Page, text: string) =>
  page.locator("tbody tr").filter({ hasText: text });

/** Rows that carry data, excluding the placeholder antd renders when there are none. */
export const dataRows = (page: Page) =>
  page.locator("tbody tr").filter({ hasNot: page.locator(".ant-table-placeholder") });

/**
 * A name no human would choose, carrying the run that made it.
 *
 * These specs create real resources on a real cluster. They delete what they create,
 * but a run killed between the two cannot, so the name has to be enough for a person
 * to identify the litter without the harness.
 */
export const throwawayName = (label: string): string =>
  `e2e-live-${label}-${process.pid}-${Date.now().toString(36)}`;
