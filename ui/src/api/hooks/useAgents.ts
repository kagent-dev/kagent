import { isNotFound, isPermissionDenied } from "../ApiError";
import type { Agent } from "../domain/agents";
import { parseRef } from "../domain/common";
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

/** Confirms which references no longer exist when a collection read may be filtered. */
export function useMissingAgents(refs: readonly string[]) {
  return useApiResource(refs.length ? ["agents.missing", ...refs] : null, async () => {
    const results = await Promise.all(refs.map(async (ref) => {
      const { namespace, name } = parseRef(ref);
      try {
        await apiClient.agentBuildingBlocks.agent(namespace, name);
        return "present";
      } catch (error) {
        if (isNotFound(error)) return "missing";
        if (isPermissionDenied(error)) return "denied";
        throw error;
      }
    }));
    return {
      missing: refs.filter((_, index) => results[index] === "missing"),
      denied: results.includes("denied"),
    };
  });
}

const KEYS = ["agents."];

/** Re-reads every Agent list and detail on screen. */
export function useInvalidateAgents(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
