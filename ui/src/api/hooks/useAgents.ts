import { useMemo } from "react";
import { isAgentSettling, type Agent } from "../domain/agents";
import { apiClient } from "../client";
import { type ApiResource, useApiResource } from "./useApiResource";
import { useInvalidateKeys } from "./useInvalidateKeys";
import { useRefreshWhile } from "./useRefreshWhile";

/**
 * How often an Agent the controller is still preparing is re-read. Its revision state
 * changes without anyone editing it, so a page read once would show "Updating" forever.
 */
export const SETTLING_AGENT_POLL_MS = 5000;

export function useAgent(namespace: string | undefined, name: string | undefined) {
  const resource = useApiResource(
    namespace && name ? ["agents.get", namespace, name] : null,
    () => apiClient.agentBuildingBlocks.agent(namespace!, name!),
  );
  useRefreshWhile(Boolean(resource.data && isAgentSettling(resource.data)), resource.refresh, SETTLING_AGENT_POLL_MS);
  return resource;
}

/**
 * The Agents in every namespace named, from one cluster-wide read.
 *
 * `ListAgents` with no namespace lists every namespace the caller's authorization
 * scope allows, so every caller on screen shares one key and one request whatever
 * namespaces it asks about, and a Refresh sends one read rather than one per
 * namespace. Each caller keeps only the namespaces it named.
 */
/** When the shared Agent list was last requested, across every caller on screen. */
let listRequestedAt = 0;

function listAllAgents(): Promise<Agent[]> {
  listRequestedAt = Date.now();
  return apiClient.agentBuildingBlocks.agents();
}

/**
 * The Agents in every namespace named, from one cluster-wide read.
 *
 * `ListAgents` with no namespace lists every namespace the caller's authorization
 * scope allows, so every caller on screen shares one key and one request whatever
 * namespaces it asks about, and a Refresh sends one read rather than one per
 * namespace. Each caller keeps only the namespaces it named.
 *
 * Each caller re-reads while one of its Agents is settling, and a tick skips while
 * another caller's read is fresh, so several callers poll the list about once an
 * interval rather than once each.
 */
export function useAgentsAcrossNamespaces(
  namespaces: readonly string[] | undefined,
): ApiResource<{ agents: Agent[] }> {
  const all = useApiResource(namespaces ? ["agents.list", ""] : null, listAllAgents);
  const key = namespaces ? [...namespaces].sort().join(",") : undefined;
  const data = useMemo(() => {
    if (!all.data || key === undefined) return undefined;
    const named = new Set(key.split(",").filter(Boolean));
    return { agents: all.data.filter((agent) => named.has(agent.namespace)) };
  }, [all.data, key]);
  useRefreshWhile(
    Boolean(data?.agents.some(isAgentSettling)),
    async () => {
      if (Date.now() - listRequestedAt < SETTLING_AGENT_POLL_MS * 0.8) return;
      await all.refresh();
    },
    SETTLING_AGENT_POLL_MS,
  );
  return { ...all, data, isEmpty: !all.error && data?.agents.length === 0 };
}

const KEYS = ["agents."];

/** Re-reads every Agent list and detail on screen. */
export function useInvalidateAgents(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
