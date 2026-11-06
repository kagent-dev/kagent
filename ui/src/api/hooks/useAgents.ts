import type { Agent } from "../domain/agents";
import { namespaceReadError } from "./namespaceReadError";
import { apiClient } from "../client";
import { useApiResource } from "./useApiResource";
import { useInvalidateKeys } from "./useInvalidateKeys";

export function useAgent(namespace: string | undefined, name: string | undefined) {
  return useApiResource(
    namespace && name ? ["agents.get", namespace, name] : null,
    () => apiClient.agentBuildingBlocks.agent(namespace!, name!),
  );
}

/** Reads the named namespaces, or every visible namespace when passed null. */
export function useAgentsAcrossNamespaces(namespaces: readonly string[] | null | undefined) {
  const key = namespaces === null ? "*" : namespaces?.slice().sort().join(",");
  return useApiResource(key !== undefined ? ["agents.listAll", key] : null, async () => {
    const names = key === "*" ? [""] : key ? key.split(",") : [];
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
      throw namespaceReadError(results.filter((result) => result.status === "rejected"));
    }
    return { agents, refused };
  });
}

const KEYS = ["agents."];

/** Re-reads every Agent list and detail on screen. */
export function useInvalidateAgents(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
