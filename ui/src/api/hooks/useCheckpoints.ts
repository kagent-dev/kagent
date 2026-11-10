import { apiClient } from "../client";
import type { Checkpoint } from "../domain/checkpoints";
import { type ApiResource, useApiResource } from "./useApiResource";
import { useSWRConfig } from "swr";

/**
 * The turn boundaries saved against one conversation.
 *
 * Held back until the conversation is known, which `useApiResource` reports as idle
 * rather than as loading.
 */
export function useCheckpoints(id: string | undefined): ApiResource<Checkpoint[]> & {
  forget: (checkpointId: string) => Promise<void>;
} {
  const { mutate } = useSWRConfig();
  const key = id ? ["agentInstances.checkpoints.list", id] : null;
  const resource = useApiResource(
    key,
    () => apiClient.agentInstances.checkpoints.list(id ?? ""),
  );
  return {
    ...resource,
    // Confirmed deletion is already authoritative even if the next list fails.
    // Update the shared record independently of that subsequent read.
    forget: async (checkpointId) => {
      if (key) {
        await mutate<Checkpoint[]>(key, (rows) => rows?.filter((row) => row.id !== checkpointId), { revalidate: false });
      }
    },
  };
}
