import { isApiError } from "../ApiError";
import { apiClient } from "../client";
import type { AgentInstance } from "../domain/agentInstances";
import { type ApiResource, useApiResource } from "./useApiResource";

interface VisibleConversations {
  rows: AgentInstance[];
  widerReadRefused?: string;
}

/** Retry an owned read only when the server refuses broader visibility. */
async function visibleConversations(
  scope: { agent?: { namespace: string; name: string } },
  owned?: Promise<AgentInstance[]>,
): Promise<VisibleConversations> {
  try {
    return { rows: await apiClient.agentInstances.list({ ...scope, allCreators: true }) };
  } catch (cause) {
    if (!isApiError(cause) || cause.status !== 403) throw cause;
    return {
      rows: await (owned ?? apiClient.agentInstances.list(scope)),
      widerReadRefused: cause.message,
    };
  }
}

/** All visible metadata, with an explicit marker when only owned rows are available. */
export function useAgentInstances(
  allCreators = false,
): ApiResource<AgentInstance[]> & { widerReadRefused?: string } {
  const resource = useApiResource<VisibleConversations>(
    ["agentInstances.list", allCreators],
    async () => allCreators
      ? visibleConversations({})
      : { rows: await apiClient.agentInstances.list({ allCreators: false }) },
  );
  return {
    ...resource,
    data: resource.data?.rows,
    widerReadRefused: resource.data?.widerReadRefused,
    isEmpty: !resource.isLoading && !resource.error && resource.data?.rows.length === 0,
  };
}

export interface AgentConversations {
  /** Every conversation with this agent the caller was allowed to see. */
  all: AgentInstance[];
  /** Conversations created by the caller. */
  own: AgentInstance[];
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
 * Conversations with one Agent, filtered by the server before pagination.
 * Browser filtering could miss matching conversations on later pages.
 * The hook stays idle until the route supplies both namespace and Agent name.
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
      // Start both collections together; a denied broad read reuses the owned
      // request. Broad metadata cannot establish ownership for actions.
      const owned = apiClient.agentInstances.list(scope);
      const [wide, own] = await Promise.all([
        visibleConversations(scope, owned),
        owned,
      ]);
      return {
        all: wide.rows,
        own,
        widerReadRefused: wide.widerReadRefused,
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
