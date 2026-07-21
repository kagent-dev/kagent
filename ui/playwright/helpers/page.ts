// Page-level driver helpers. Backend-agnostic — they assert on rendered DOM
// (roles/text), so they work regardless of how data is mocked.

import { expect, type Locator, type Page } from "@playwright/test";

/**
 * Wait for the full-screen LoadingState overlay to clear. It sits on top of the
 * page (z-10, backdrop-blur) while data loads, so content behind it counts as
 * "visible" to Playwright even though a user can't see/use it yet. Call this
 * before asserting on or interacting with page content.
 */
export async function waitForAppReady(page: Page): Promise<void> {
  await expect(page.getByTestId("loading-overlay")).toHaveCount(0);
}

/** Navigate to a path, wait for loading to finish, and optionally assert the page's <h1>. */
export async function loadPage(
  page: Page,
  path: string,
  opts: { heading?: string } = {},
): Promise<void> {
  await page.goto(path);
  await waitForAppReady(page);
  if (opts.heading) {
    await expect(page.getByRole("heading", { level: 1, name: opts.heading })).toBeVisible();
  }
}

/**
 * Navigate to `path` and run `assert`, re-navigating and retrying until it passes.
 *
 * The list endpoints are served from the controller-runtime *cached* (informer)
 * client, so a read taken immediately after a write can return a stale object —
 * the cache is updated asynchronously from the watch stream, not synchronously by
 * the write. A single `goto` + DOM poll can't recover: the stale server-rendered
 * HTML never re-fetches, so `toContainText`/`toHaveCount` just poll unchanged
 * markup until they time out. Reloading between attempts re-queries the backend
 * until the cache catches up.
 *
 * Keep the assertions inside `assert` short-timeout (e.g. `{ timeout: 2_000 }`) so
 * a stale render fails fast and triggers another reload instead of spending the
 * whole budget polling one stale page.
 */
export async function reloadUntil(
  page: Page,
  path: string,
  assert: () => Promise<void>,
  opts: { timeout?: number } = {},
): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await waitForAppReady(page);
    await assert();
  }).toPass({ timeout: opts.timeout ?? 20_000 });
}

/** Assert the ErrorState component ("Error Encountered") is not on the page. */
export async function expectNoErrors(page: Page): Promise<void> {
  await expect(page.getByText("Error Encountered")).toHaveCount(0);
}

/**
 * Scroll a list row (or any element) into view and assert it's actually within the
 * viewport. Use after a mutation to prove the item's changed state is visible on the
 * real list — `toBeVisible` alone passes for off-screen rows, so this makes the
 * "scrolled into view" guarantee explicit (and keeps the row on-screen in the video).
 */
export async function expectScrolledIntoView(locator: Locator): Promise<void> {
  await locator.scrollIntoViewIfNeeded();
  await expect(locator).toBeInViewport();
}

type ToastType = "success" | "error" | "warning" | "info";

/**
 * Assert a sonner toast with the given text is visible. Toasts auto-dismiss, so
 * call this promptly after the triggering action. Pass `type` to also assert the
 * severity (data-type on the toast <li>).
 */
export async function expectToast(
  page: Page,
  text: string | RegExp,
  opts: { type?: ToastType } = {},
): Promise<void> {
  const selector = opts.type ? `[data-sonner-toast][data-type="${opts.type}"]` : "[data-sonner-toast]";
  await expect(page.locator(selector).filter({ hasText: text })).toBeVisible();
}
