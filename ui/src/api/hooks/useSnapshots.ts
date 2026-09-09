import { apiClient } from "../client";
import type { AgentInstance } from "../domain/agentInstances";
import type { Checkpoint } from "../domain/checkpoints";
import { type ApiResource, useApiResource } from "./useApiResource";

/** A saved boundary, with the conversation it was taken of. */
export interface Snapshot extends Checkpoint {
  /**
   * The conversation, when it is one the caller can still see.
   *
   * Absent for a boundary whose conversation has gone — deleting a conversation does
   * not release the snapshots taken of it, so those rows are exactly the ones a reader
   * most wants to find here.
   */
  conversation?: AgentInstance;
}

/**
 * Every saved boundary the caller can see, across all their conversations.
 *
 * ## Why this is a fan-out
 *
 * `ListCheckpoints` takes an `agent_instance_id` and requires it — there is no RPC
 * that lists a caller's checkpoints. So the conversations are read first and each is
 * asked for its own, which is one request per conversation. That is the cost of the
 * API as it stands, and it is why this page is not offered from the shell's chrome:
 * it is a page you go to, not one that loads beside something else.
 *
 * A conversation whose read fails does not fail the page. One conversation the caller
 * has lost access to would otherwise take out the whole table, including the rows that
 * are the reason to be here — so its checkpoints are simply missing and the rest are
 * shown.
 */
export function useSnapshots(): ApiResource<Snapshot[]> {
  return useApiResource(["snapshots.list"], async () => {
    const conversations = await apiClient.agentInstances.list();
    const perConversation = await Promise.all(
      conversations.map(async (conversation) => {
        try {
          const checkpoints = await apiClient.agentInstances.checkpoints.list(
            conversation.id,
          );
          return checkpoints.map((checkpoint) => ({ ...checkpoint, conversation }));
        } catch (cause: unknown) {
          console.error(`Could not read snapshots of ${conversation.id}:`, cause);
          return [];
        }
      }),
    );
    // Newest first across every conversation, which is not the order the fan-out
    // returns them in: that is grouped by conversation, and a reader looking for what
    // they took a minute ago would have to know which one it was under.
    return perConversation
      .flat()
      .sort((left, right) => (right.createdAt ?? "").localeCompare(left.createdAt ?? ""));
  });
}
