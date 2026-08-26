/**
 * Page-level drivers.
 *
 * These assert on rendered DOM through roles and test ids, never on prose, so
 * they survive the page rebuilds still ahead. Where a test id is used it is one
 * the app already ships for the purpose.
 */

import { expect, type Locator, type Page } from "@playwright/test";

/** Routes the suite drives. Mirrors `src/router/routes.ts`. */
export const routes = {
  dashboard: "/",
  login: "/login",
  agents: "/agents",
  agentNew: "/agents/new",
  models: "/models",
  modelNew: "/models/new",
  mcpServers: "/mcp",
  prompts: "/prompts",
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
export const agentChat = (id: string, namespace = "kagent") =>
  `/agents/${namespace}/${id}/chat`;

/** Where one agent's record lives. */
export const agentDetail = (id: string, namespace = "kagent") =>
  `/agents/${namespace}/${id}`;

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

/** Resolves once no loading indicator is left on the page. */
export async function expectSettled(page: Page): Promise<void> {
  await expect(page.locator(".ant-spin-spinning")).toHaveCount(0);
}
