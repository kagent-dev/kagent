import { apiClient } from "../client";
import type { AgentInstance } from "../domain/agentInstances";
import { type ApiResource, useApiResource } from "./useApiResource";

/** All conversations visible to the caller. */
export function useAgentInstances(
  allCreators = false,
): ApiResource<AgentInstance[]> {
  return useApiResource(
    ["agentInstances.list", allCreators],
    () => apiClient.agentInstances.list({ allCreators }),
  );
}

export interface AgentConversations {
  /** Every conversation with this agent the caller was allowed to see. */
  all: AgentInstance[];
  /** The ids the caller created, which are the ones that will open. */
  openableIds: Set<string>;
  /**
   * Why the list is only the caller's own, when it is.
   *
   * `all_creators` is authorised separately from the list and the controller
   * *refuses* the request when it is not allowed rather than quietly narrowing it —
   * checked in `Service.List`, which returns the authorisation error. So a reader
   * without that permission gets an error where they wanted a list, and answering
   * with their own conversations plus this sentence is better than answering with
   * nothing. Undefined when the wide read succeeded.
   */
  widerReadRefused?: string;
}

/**
 * The conversations with one agent — a `(AgentTemplate, Harness)` pair.
 *
 * Narrowed by the server: `ListAgentInstances` takes `agent_template` and `harness`
 * and resolves them through the prepared revision, so the filtering happens before
 * the page is cut. Filtering in the browser would search one page of a paged read
 * and report "no conversations" about a row further down.
 *
 * Held back until it has all three parts of the address, which `useApiResource`
 * reports as idle rather than as loading.
 */
export function useAgentConversations(
  namespace: string | undefined,
  agentTemplate: string | undefined,
  harness: string | undefined,
): ApiResource<AgentConversations> {
  return useApiResource(
    namespace && agentTemplate && harness
      ? ["agentInstances.forAgent", namespace, agentTemplate, harness]
      : null,
    async () => {
      const scope = {
        agentTemplate: { namespace: namespace ?? "", name: agentTemplate ?? "" },
        harness: { namespace: namespace ?? "", name: harness ?? "" },
      };
      /*
       * The narrow read is the one that must succeed, so it is awaited on its own
       * terms: it decides which rows open, and a failure there is a failure of the
       * page. The wide one is allowed to be refused.
       */
      const [wide, own] = await Promise.all([
        apiClient.agentInstances
          .list({ ...scope, allCreators: true })
          .then(
            (rows) => ({ rows, refused: undefined as string | undefined }),
            (cause: unknown) => ({
              rows: undefined,
              refused: cause instanceof Error ? cause.message : String(cause),
            }),
          ),
        apiClient.agentInstances.list(scope),
      ]);

      return {
        all: wide.rows ?? own,
        openableIds: new Set(own.map((row) => row.id)),
        widerReadRefused: wide.refused,
      };
    },
  );
}

/** One agent instance. Held back until the route has given us its ID. */
export function useAgentInstance(
  id: string | undefined,
): ApiResource<AgentInstance> {
  return useApiResource(
    id ? ["agentInstances.get", id] : null,
    () => apiClient.agentInstances.get(id ?? ""),
  );
}
