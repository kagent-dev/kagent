/**
 * Drivers for the vendor extension framework.
 *
 * These assert the **mechanism**, never the example's content. A point either
 * mounts what was configured at it or it does not; the bundled Example extension is
 * only the thing being mounted, and it is free to change its copy, its styling,
 * or its own test ids without any of this needing an edit.
 *
 * The one handle these rely on is `vendor-slot-<id>`, which `VendorSlot` emits
 * for exactly this purpose.
 */

import { expect, type Locator, type Page } from "@playwright/test";

/** Every extension point the app offers. Mirrors `src/vendorExtensions/extensionPoints.ts`. */
export const EXTENSION_POINT_IDS = [
  "app_shell_appLayout_contentArea_leadingBanner",
  "app_shell_appLayout_contentArea_globalOverlay",
  "app_shell_appLayout_appSidebar_footer",
  "app_agents_agentsList_pageHeader_actions",
  "app_agents_agentsList_agentListItem_badge",
  "app_agents_agentChat_agentChatMessage_additionalActionsButton",
  "app_dashboard_dashboardOverview_summaryGrid_leadingCard",
] as const;

export type ExtensionPointId = (typeof EXTENSION_POINT_IDS)[number];

/** Whatever is mounted at a point, wherever it ended up in the DOM. */
export function slot(page: Page, id: ExtensionPointId): Locator {
  return page.locator(`[data-testid="vendor-slot-${id}"]`);
}

/** Every mounted slot on the page, regardless of point. */
export function allSlots(page: Page): Locator {
  return page.locator('[data-testid^="vendor-slot-"]');
}

/**
 * Sidebar nav entries, in DOM order, as their test ids.
 *
 * `evaluateAll` is a one-shot read with none of Playwright's auto-waiting, so
 * this waits for the sidebar to have rendered first. Without that it answers
 * `[]` on a page that simply has not finished mounting — which reads as "the
 * sidebar is empty" and would let a broken build pass.
 */
export async function navOrder(page: Page): Promise<string[]> {
  const entries = page.locator('[data-testid="app-sidebar"] [data-testid^="nav-"]');
  await entries.first().waitFor({ state: "attached" });
  return entries.evaluateAll((nodes) =>
    nodes.map((node) => node.getAttribute("data-testid") ?? ""),
  );
}

/** The nav entries the application itself ships, in the order it declares them. */
export const CORE_NAV_ORDER = [
  "nav-dashboard",
  "nav-agents",
  "nav-models",
  "nav-mcpServers",
  "nav-prompts",
  "nav-substrate",
];

/**
 * Asserts a point's content escaped the content area rather than rendering
 * inside it.
 *
 * This is the whole reason `portal` render mode exists: the content area is an
 * `overflow: auto` scroll box with its own stacking context, so a floating
 * overlay declared inside it would be clipped and trapped under sibling chrome.
 * Rendering in the right place is not observable from the component's own
 * markup — only from where it landed.
 */
export async function expectPortalled(page: Page, id: ExtensionPointId): Promise<void> {
  await expect(
    page.locator(`[data-testid="app-content"] [data-testid="vendor-slot-${id}"]`),
    "portalled content should not be inside the scrolling content area",
  ).toHaveCount(0);
  await expect(
    page.locator(`body > [data-testid="vendor-slot-${id}"]`),
    "portalled content should be mounted at the document root",
  ).toHaveCount(1);
}

/** Asserts a point's content rendered where the slot sits, not at the document root. */
export async function expectInline(
  page: Page,
  id: ExtensionPointId,
  within: Locator,
): Promise<void> {
  await expect(within.locator(`[data-testid="vendor-slot-${id}"]`)).toHaveCount(1);
  await expect(page.locator(`body > [data-testid="vendor-slot-${id}"]`)).toHaveCount(0);
}
