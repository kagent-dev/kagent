import { apiClient } from "../client";
import type { AgentResponse } from "../domain/agents";
import { type ApiResource, useApiResource } from "./useApiResource";

/** Every agent in the cluster. */
export function useAgents(): ApiResource<AgentResponse[]> {
  return useApiResource(["agents.list"], () => apiClient.agents.list());
}

/** One agent. Holds off until both parts of the ref are known. */
export function useAgent(
  namespace: string | undefined,
  name: string | undefined,
): ApiResource<AgentResponse> {
  return useApiResource(
    namespace && name ? ["agents.get", namespace, name] : null,
    () => apiClient.agents.get(namespace!, name!),
  );
}
