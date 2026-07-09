// Navigation helpers for the persistent header (src/components/Header.tsx).
//
// Routes live inside two Radix dropdown menus ("Create" and "View"), not flat
// links. Menu items only enter the DOM (as role="menuitem") once the menu is
// open. Header markup is duplicated for desktop/mobile, but the hidden block is
// out of the accessibility tree, so role-based locators resolve to the visible
// one on a desktop viewport.

import { type Page } from "@playwright/test";

async function openMenu(page: Page, trigger: "Create" | "View"): Promise<void> {
  await page.getByRole("button", { name: trigger, exact: true }).click();
}

async function chooseFrom(
  page: Page,
  trigger: "Create" | "View",
  item: string,
  urlGlob?: string | RegExp,
): Promise<void> {
  await openMenu(page, trigger);
  // Exact match: "New Agent" is a substring of "New Agent Harness".
  await page.getByRole("menuitem", { name: item, exact: true }).click();
  if (urlGlob) await page.waitForURL(urlGlob);
}

/** Open the "View" menu and go to a listing page, e.g. gotoView(page, "Models", "**\/models"). */
export function gotoView(page: Page, item: string, urlGlob?: string | RegExp): Promise<void> {
  return chooseFrom(page, "View", item, urlGlob);
}

/** Open the "Create" menu and go to a creation page, e.g. gotoCreate(page, "New Agent", "**\/agents/new"). */
export function gotoCreate(page: Page, item: string, urlGlob?: string | RegExp): Promise<void> {
  return chooseFrom(page, "Create", item, urlGlob);
}

/** Click the direct "Home" link in the header. */
export async function gotoHome(page: Page): Promise<void> {
  await page.getByRole("link", { name: "Home" }).first().click();
  await page.waitForURL(/\/(agents)?$/);
}
