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
  return useApiResource(id ? ["agentInstances.checkpoints.list", id] : null, async () => {
    // Every page: the chat draws a line per boundary, so a partial list would silently
    // stop drawing them partway up the transcript.
    const all: Checkpoint[] = [];
    for (let page = 0; page < PAGE_LIMIT; page += 1) {
      const read = await apiClient.agentInstances.checkpoints.list({
        id,
        limit: PAGE_SIZE,
        offset: all.length,
      });
      all.push(...read.checkpoints);
      if (read.checkpoints.length === 0 || all.length >= read.total) return all;
    }
    return all;
  });
}

/** The controller refuses anything over 100. */
const PAGE_SIZE = 100;
/** Enough for ten thousand boundaries against one conversation, and not unbounded. */
const PAGE_LIMIT = 100;
