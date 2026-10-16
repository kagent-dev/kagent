import { apiClient } from "../client";
import type { AgentInstance } from "../domain/agentInstances";
import { type ApiResource, useApiResource } from "./useApiResource";

/** Conversations created by the caller. */
export function useAgentInstances(): ApiResource<AgentInstance[]> {
  return useApiResource(
    ["agentInstances.list"],
    () => apiClient.agentInstances.list(),
  );
}

export interface AgentConversations {
  /** Conversations created by the caller. */
  own: AgentInstance[];
}

/**
 * The conversations with one agent — a Agent definition.
 *
 * Narrowed by the server: `ListAgentInstances` takes `agent`
 * and resolves them through the prepared revision, so the filtering happens before
 * the page is cut. Filtering in the browser would search one page of a paged read
 * and report "no conversations" about a row further down.
 *
 * Held back until it has all three parts of the address, which `useApiResource`
 * reports as idle rather than as loading.
 */
export function useAgentConversations(
  namespace: string | undefined,
  agent: string | undefined,
): ApiResource<AgentConversations> {
  return useApiResource(
    namespace && agent
      ? ["agentInstances.forAgent", namespace, agent]
      : null,
    async () => {
      const scope = {
        agent: { namespace: namespace ?? "", name: agent ?? "" },
      };
      return { own: await apiClient.agentInstances.list(scope) };
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
