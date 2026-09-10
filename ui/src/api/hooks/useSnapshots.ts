import { useMemo } from "react";
import { apiClient } from "../client";
import type { AgentInstance } from "../domain/agentInstances";
import type { Checkpoint, CheckpointSort } from "../domain/checkpoints";
import { useAgentInstances } from "./useAgentInstances";
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
  sort?: CheckpointSort;
  page: number;
  pageSize: number;
}

/**
 * One page of the caller's saved boundaries, narrowed and ordered by the controller.
 *
 * Nothing is re-narrowed here: a client-side filter over a server-side page reports "no
 * matches" about rows it never read. Turning a page is one request, because the
 * conversations are read under their own key rather than inside this one — they do not
 * depend on the query, and failing to read them costs the links, not the page.
 */
export function useSnapshots(query: SnapshotQuery): ApiResource<SnapshotPage> {
  const sortKey = query.sort ? `${query.sort.field}:${query.sort.descending ? "d" : "a"}` : "";
  const page = useApiResource(
    ["snapshots.list", query.filter, sortKey, query.page, query.pageSize],
    () =>
      apiClient.agentInstances.checkpoints.list({
        filter: query.filter,
        sort: query.sort,
        limit: query.pageSize,
        offset: (query.page - 1) * query.pageSize,
      }),
  );
  const conversations = useAgentInstances();

  const data = useMemo<SnapshotPage | undefined>(() => {
    if (!page.data) return undefined;
    const byId = new Map((conversations.data ?? []).map((row) => [row.id, row]));
    return {
      snapshots: page.data.checkpoints.map((checkpoint) => ({
        ...checkpoint,
        conversation: byId.get(checkpoint.agentInstanceId),
      })),
      total: page.data.total,
    };
  }, [page.data, conversations.data]);

  return {
    ...page,
    data,
    // Counted here rather than left to the shared rule, which reads a two-key object as
    // "not empty" and so never lets the page say there is nothing saved.
    isEmpty: !page.isLoading && !page.error && data?.snapshots.length === 0,
    refresh: async () => {
      await Promise.all([page.refresh(), conversations.refresh()]);
    },
  };
}
