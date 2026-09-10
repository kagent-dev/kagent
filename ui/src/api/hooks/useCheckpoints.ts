import { apiClient } from "../client";
import type { Checkpoint } from "../domain/checkpoints";
import { type ApiResource, useApiResource } from "./useApiResource";

/**
 * The turn boundaries saved against one conversation.
 *
 * Held back until the conversation is known, which `useApiResource` reports as idle
 * rather than as loading.
 */
export function useCheckpoints(id: string | undefined): ApiResource<Checkpoint[]> {
  return useApiResource(
    id ? ["agentInstances.checkpoints.list", id] : null,
    () => apiClient.agentInstances.checkpoints.list(id ?? ""),
  );
}
