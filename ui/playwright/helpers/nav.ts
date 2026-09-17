/**
 * Navigation drivers for the persistent shell (`src/components/Structure/**`).
 *
 * The sidebar is an antd Menu whose entries carry stable test ids (`nav-<key>`),
 * and the header's Create menu is a dropdown whose items only enter the DOM once
 * it is open.
 */

import { expect, type Page } from "@playwright/test";

/** Sidebar entries, keyed as in `src/components/Structure/navItems.ts`. */
/*
 * There is no `agentInstances` entry any more, and that is the change rather than
 * an omission: an agent *is* an AgentInstance, so the agents page is the instances
 * page. Two entries in the navigation for one idea — one of them naming a resource
 * the API does not serve — is what this replaced.
 */
export type NavKey =
  | "dashboard"
  | "agents"
  | "models"
  | "mcpServers"
  | "prompts"
  | "substrate";

export const navLabels: Record<NavKey, string> = {
  dashboard: "Dashboard",
  agents: "Agents",
  models: "Models",
  mcpServers: "MCP Servers",
  prompts: "Prompts",
  substrate: "Substrate",
};

/** Clicks a sidebar entry and waits for the route to change. */
export async function clickNav(
  page: Page,
  key: NavKey,
  expectedUrl: RegExp,
): Promise<void> {
  await page.getByTestId(`nav-${key}`).click();
  await page.waitForURL(expectedUrl);
}

/** Asserts the persistent shell chrome is present. */
export async function expectShell(page: Page): Promise<void> {
  await expect(page.getByTestId("app-header")).toBeVisible();
  await expect(page.getByTestId("app-sidebar")).toBeVisible();
  await expect(page.getByTestId("app-content")).toBeVisible();
}

/** Asserts the shell chrome is absent — for routes that render standalone. */
export async function expectNoShell(page: Page): Promise<void> {
  await expect(page.getByTestId("app-header")).toHaveCount(0);
  await expect(page.getByTestId("app-sidebar")).toHaveCount(0);
}
