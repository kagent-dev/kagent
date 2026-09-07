/**
 * Counting what a page asked the backend for.
 *
 * ## Why not count requests
 *
 * Because there are none. In mock mode the API is served by a substituted
 * `Transport` (`src/mocks/transport.ts`), so an operation never becomes an HTTP
 * request: `page.on("request")` sees only the navigation, and a `window.fetch`
 * wrapper sees nothing at all. Both instruments read zero whether the page is
 * polling or idle.
 *
 * The mock backend therefore tallies calls per RPC and publishes the tally on the
 * page. That is also closer to what these tests mean: "polling refreshed
 * everything on this page" is a claim about reads, not about HTTP, and it stays
 * true the next time the transport changes.
 *
 * ## Why a missing key throws
 *
 * A counter that answered `0` for an RPC name nobody registered would make a
 * misspelling look like a page that never read anything — an instrument that fails
 * silently in the direction of "nothing happened". The mock seeds a zero for every
 * RPC it serves and creates no others, so an absent key means the name is wrong,
 * and this helper says so instead of returning a number.
 *
 * Against a real backend, count requests instead: `playwright/live/**` does, and
 * that is still the right instrument there.
 */

import type { Page } from "@playwright/test";

/** Where `src/mocks/transport.ts` publishes its per-RPC counts. */
const PROPERTY = "__kagentMockCalls";

/**
 * The RPCs the browser suite watches, spelled as the mock backend keys them.
 *
 * Named here rather than inline in each spec so a rename is one edit, and so a
 * typo in a spec is a compile error rather than a lookup that throws at runtime.
 */
export const rpc = {
  listModelConfigs: "kagent.api.v1alpha1.ModelService/ListModelConfigs",
  listToolServers: "kagent.api.v1alpha1.ToolService/ListToolServers",
  listPromptTemplates: "kagent.api.v1alpha1.PromptTemplateService/ListPromptTemplates",
  listNamespaces: "kagent.api.v1alpha1.SystemService/ListNamespaces",
  substrateStatus: "kagent.api.v1alpha1.SystemService/GetSubstrateStatus",
  listAgentTemplates: "kagent.api.v1alpha1.AgentTemplateService/ListAgentTemplates",
  listAgentInstances: "kagent.api.v1alpha1.AgentInstanceService/ListAgentInstances",
  getAgentInstance: "kagent.api.v1alpha1.AgentInstanceService/GetAgentInstance",
  suspendAgentInstance: "kagent.api.v1alpha1.AgentInstanceService/SuspendAgentInstance",
  resumeAgentInstance: "kagent.api.v1alpha1.AgentInstanceService/ResumeAgentInstance",
} as const;

export type WatchedRpc = (typeof rpc)[keyof typeof rpc];

/** How many times the page has called one RPC since it loaded. */
export async function operationCalls(page: Page, name: WatchedRpc): Promise<number> {
  const counts = await operationCallCounts(page, [name]);
  return counts[name];
}

/**
 * The counts for several RPCs, read together.
 *
 * One evaluation rather than several, so the numbers describe the same moment —
 * which matters when the assertion is about a page that is polling while being
 * measured.
 */
export async function operationCallCounts<T extends WatchedRpc>(
  page: Page,
  names: readonly T[],
): Promise<Record<T, number>> {
  const result = await page.evaluate(
    ({ property, names: wanted }) => {
      const counts = (
        window as unknown as Record<string, Record<string, number> | undefined>
      )[property];
      if (!counts) return { error: "absent" as const };

      const missing = wanted.filter((name) => counts[name] === undefined);
      if (missing.length > 0) return { error: "unknown" as const, missing };

      return {
        counts: Object.fromEntries(wanted.map((name) => [name, counts[name]])),
      };
    },
    { property: PROPERTY, names: [...names] as string[] },
  );

  if ("error" in result && result.error === "absent") {
    throw new Error(
      `The mock backend published no call counts on window.${PROPERTY}. ` +
        `Either the app is not running in mock mode, or it had not started when this was read.`,
    );
  }
  if ("error" in result && result.error === "unknown") {
    throw new Error(
      `The mock backend serves no such RPC: ${result.missing?.join(", ")}. ` +
        `Check the name against src/mocks/transport.ts — a count of zero is never ` +
        `reported for an unknown RPC, because that reads as "the page did nothing".`,
    );
  }

  return (result as { counts: Record<T, number> }).counts;
}
