import { apiClient } from "../client";
import type { AgentInstance } from "../domain/agentInstances";
import type { Checkpoint, CheckpointSort } from "../domain/checkpoints";
import { type ApiResource, useApiResource } from "./useApiResource";

/** A saved boundary, with the conversation it was taken of when that still exists. */
export interface Snapshot extends Checkpoint {
  /**
   * The conversation, when the caller can still see it — deleting one does not release
   * the snapshots taken of it. Only decides whether the row's name links anywhere; the
   * name itself is the checkpoint's own.
   */
  conversation?: AgentInstance;
}

export interface SnapshotPage {
  snapshots: Snapshot[];
  /** Everything the filter matched, not just this page. */
  total: number;
}

export interface SnapshotQuery {
  filter: string;
  sort: readonly CheckpointSort[];
  page: number;
  pageSize: number;
}

/**
 * One page of the caller's saved boundaries, narrowed and ordered by the controller.
 *
 * Nothing is re-narrowed here: a client-side filter over a server-side page reports "no
 * matches" about rows it never read. The one extra request, for conversations, is not
 * per row — it only says which names can link somewhere.
 */
export function useSnapshots(query: SnapshotQuery): ApiResource<SnapshotPage> {
  const sortKey = query.sort.map((by) => `${by.field}:${by.descending ? "d" : "a"}`).join(",");
  return useApiResource(
    ["snapshots.list", query.filter, sortKey, query.page, query.pageSize],
    async () => {
      const [page, conversations] = await Promise.all([
        apiClient.agentInstances.checkpoints.list({
          filter: query.filter,
          sort: query.sort,
          limit: query.pageSize,
          offset: (query.page - 1) * query.pageSize,
        }),
        apiClient.agentInstances.list(),
      ]);
      const byId = new Map(conversations.map((row) => [row.id, row]));
      return {
        snapshots: page.checkpoints.map((checkpoint) => ({
          ...checkpoint,
          conversation: byId.get(checkpoint.agentInstanceId),
        })),
        total: page.total,
      };
    },
  );
}
