// Navigation helpers for the persistent sidebar (AppSidebar.tsx).
//
// Listing routes live as sidebar links inside the "Main navigation" landmark.
// Create routes are reached from page-level actions (e.g. "New Model" on the
// Models list) or by direct URL where no listing action exists yet.

import { type Page } from "@playwright/test";

/**
 * Wait for the full-screen LoadingState overlay to clear. It sits on top of the
 * page during route transitions, so a follow-up click can hit the overlay and flake.
 */
async function waitForOverlayGone(page: Page): Promise<void> {
  await page.getByTestId("loading-overlay").waitFor({ state: "hidden" });
}

function sidebarNav(page: Page) {
  return page.getByRole("navigation", { name: "Main navigation" });
}

/** Click a sidebar link, e.g. gotoSidebarLink(page, "Models", "**\/models"). */
export async function gotoSidebarLink(
  page: Page,
  linkName: string,
  urlGlob?: string | RegExp,
): Promise<void> {
  await sidebarNav(page).getByRole("link", { name: linkName, exact: true }).click();
  if (urlGlob) await page.waitForURL(urlGlob);
  await waitForOverlayGone(page);
}

/** Navigate directly to a create page (no global Create menu in the sidebar shell). */
export async function gotoCreatePath(
  page: Page,
  path: string,
  urlGlob?: string | RegExp,
): Promise<void> {
  await page.goto(path);
  if (urlGlob) await page.waitForURL(urlGlob);
  await waitForOverlayGone(page);
}
