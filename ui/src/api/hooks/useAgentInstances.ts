import { useMemo } from "react";
import { type Cache, unstable_serialize, useSWRConfig } from "swr";
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

async function ownConversations(): Promise<VisibleConversations> {
  return { rows: await apiClient.agentInstances.list({ allCreators: false }) };
}

/** All visible metadata, with an explicit marker when only owned rows are available. */
export function useAgentInstances(
  allCreators = false,
): ApiResource<AgentInstance[]> & { widerReadRefused?: string } {
  const resource = useApiResource<VisibleConversations>(
    ["agentInstances.list", allCreators],
    () => allCreators ? visibleConversations({}) : ownConversations(),
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

const FOR_AGENT_PREFIX = unstable_serialize(["agentInstances.forAgent"]);
const OWN_LIST = ["agentInstances.list", false] as const;
const OWN_LIST_KEY = unstable_serialize(OWN_LIST);

/**
 * The caller's conversations with one Agent, for the new-conversation rail.
 * The conversation page's cached list stands in until they land, and `refresh` re-reads that list too.
 */
export function useOwnAgentConversations(
  namespace: string | undefined,
  agent: string | undefined,
): ApiResource<AgentInstance[]> {
  const { cache, mutate } = useSWRConfig();
  const conversations = useAgentConversations(namespace, agent);
  const listed = conversations.isLoading
    ? (cache.get(OWN_LIST_KEY)?.data as VisibleConversations | undefined)?.rows
    : undefined;
  const own = conversations.data?.own;
  const cached = useMemo(
    () => listed?.filter((row) => row.agent === `${namespace}/${agent}`),
    [listed, namespace, agent],
  );
  const data = own ?? cached;
  return {
    ...conversations,
    data,
    isLoading: conversations.isLoading && !cached,
    isEmpty: !conversations.error && data?.length === 0,
    // No hook holds the conversation page's list here, so it is written rather than revalidated.
    refresh: async () => {
      await Promise.all([
        conversations.refresh(),
        mutate(OWN_LIST, ownConversations(), { revalidate: false }),
      ]);
    },
  };
}

/**
 * The caller's conversations, for the rail beside an open one.
 * Until that list lands, an agent page's cached rows holding `id` stand in, so opening a row keeps the list.
 */
export function useSiblingConversations(id: string | undefined): ApiResource<AgentInstance[]> {
  const { cache } = useSWRConfig();
  const own = useAgentInstances();
  const cached = own.isLoading && id ? cachedOwnRowsWith(cache, id) : undefined;
  return cached ? { ...own, data: cached, isLoading: false } : own;
}

function cachedOwnRowsWith(cache: Cache, id: string): AgentInstance[] | undefined {
  for (const key of cache.keys()) {
    if (!key.startsWith(FOR_AGENT_PREFIX)) continue;
    const rows = (cache.get(key)?.data as AgentConversations | undefined)?.own;
    if (rows?.some((row) => row.id === id)) return rows;
  }
  return undefined;
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
