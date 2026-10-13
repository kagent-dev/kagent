import { isAgentSettling, type Agent } from "../domain/agents";
import { apiClient } from "../client";
import { useApiResource } from "./useApiResource";
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

export function useAgentsAcrossNamespaces(namespaces: readonly string[] | undefined) {
  const key = namespaces ? [...namespaces].sort().join(",") : undefined;
  const resource = useApiResource(key !== undefined ? ["agents.listAll", key] : null, async () => {
    const names = key ? key.split(",") : [];
    const results = await Promise.allSettled(
      names.map((namespace) => apiClient.agentBuildingBlocks.agents(namespace)),
    );
    const agents: Agent[] = [];
    const refused: { namespace: string; reason: string }[] = [];
    results.forEach((result, index) => {
      if (result.status === "fulfilled") {
        agents.push(...result.value);
      } else {
        refused.push({
          namespace: names[index],
          reason: result.reason instanceof Error ? result.reason.message : String(result.reason),
        });
      }
    });
    if (names.length > 0 && refused.length === names.length) {
      throw new Error(refused.map((entry) => entry.reason).join("; "));
    }
    return { agents, refused };
  });
  useRefreshWhile(Boolean(resource.data?.agents.some(isAgentSettling)), resource.refresh, SETTLING_AGENT_POLL_MS);
  return resource;
}

const KEYS = ["agents."];

/** Re-reads every Agent list and detail on screen. */
export function useInvalidateAgents(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
