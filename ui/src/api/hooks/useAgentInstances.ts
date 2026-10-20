import { ApiError } from "../ApiError";
import { apiClient } from "../client";
import type { AgentInstance } from "../domain/agentInstances";
import { type ApiResource, useApiResource } from "./useApiResource";

/** The list `useAgentInstances` returns, with the reason a wider read was refused. */
export interface AgentInstanceList extends ApiResource<AgentInstance[]> {
  /**
   * Why the list is only the caller's own, when `allCreators` was asked for and refused.
   *
   * The controller refuses an `all_creators` read from a caller who may not make it
   * rather than narrowing it, so the list is re-read as the caller's own and this says
   * why. Undefined when the wide read succeeded, or was not asked for.
   */
  widerReadRefused?: string;
}

/** All conversations visible to the caller. */
export function useAgentInstances(allCreators = false): AgentInstanceList {
  const resource = useApiResource(
    ["agentInstances.list", allCreators],
    async () => {
      if (!allCreators) {
        return { rows: await apiClient.agentInstances.list({ allCreators }), refused: undefined };
      }
      try {
        return {
          rows: await apiClient.agentInstances.list({ allCreators: true }),
          refused: undefined as string | undefined,
        };
      } catch (cause) {
        // Only a refusal is answered with the caller's own rows; anything else is a real failure.
        if (!(cause instanceof ApiError) || cause.status !== 403) throw cause;
        return {
          rows: await apiClient.agentInstances.list({ allCreators: false }),
          refused: cause.message,
        };
      }
    },
  );

  const rows = resource.data?.rows;
  return {
    ...resource,
    data: rows,
    isEmpty: rows !== undefined && !resource.error && rows.length === 0,
    widerReadRefused: resource.data?.refused,
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
        own,
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
