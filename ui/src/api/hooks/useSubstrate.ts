import { apiClient } from "../client";
import type {
  SubstrateActorPage,
  SubstrateStatusResponse,
  SubstrateSummary,
  SubstrateWorkerPage,
} from "../domain/substrate";
import type {
  SubstrateActorSortField,
  SubstratePageInput,
  SubstrateWorkerSortField,
} from "../operations";
import { type ApiResource, useApiResource } from "./useApiResource";

/**
 * Agent Substrate inventory, optionally narrowed to one namespace.
 *
 * Two things callers should read rather than assume: `enabled` is false when the
 * controller has no ate-api endpoint configured, which is a normal deployment
 * and not a failure; and `ateApiError` can be set on an otherwise successful
 * response, meaning the Kubernetes-derived halves are complete while the
 * runtime ones are partial. Both deserve their own message on screen — neither
 * is an `error`.
 */
export function useSubstrateStatus(
  namespace?: string,
): ApiResource<SubstrateStatusResponse> {
  return useApiResource(["substrate.status", namespace ?? ""], () =>
    apiClient.substrate.status(namespace),
  );
}

/**
 * The substrate inventory as counts, plus the two lists small enough to send whole.
 *
 * This is what the tiles read, and it is the only place a *total* comes from: the
 * actor and worker reads below are pages, and a page's length is not a total.
 * Counting rows on screen and labelling the result "Actors" would report 100 for a
 * cluster running four hundred thousand.
 *
 * It is also the expensive read on this page. ate-api reports no totals, so the
 * controller walks every one of its pages to count — seconds on a large cluster,
 * against milliseconds for a page. Poll it no faster than the counts need to be
 * right: a caller that does not show them beside a live table wants it far less often
 * than the pages, and `computedAt` says how old the answer it got is.
 */
export function useSubstrateSummary(namespace?: string): ApiResource<SubstrateSummary> {
  return useApiResource(["substrate.summary", namespace ?? ""], () =>
    apiClient.substrate.summary(namespace),
  );
}

/**
 * One page of actors, ordered and narrowed server-side.
 *
 * The filter and the sort are part of the key, because both change which rows this read
 * answers with: typing in the search box or clicking a header re-reads rather than
 * re-rendering what was already fetched. That is the whole point of them being the
 * server's — filtering or ordering here would reach one page, and a match on page nine
 * would read on screen as "no matches".
 *
 * What it costs is worth naming. ate-api offers neither an order nor a filter, so the
 * controller walks every one of its pages to apply them: each keystroke past the
 * debounce, and each header click, is a walk of the inventory.
 */
export function useSubstrateActors(
  input: SubstratePageInput<SubstrateActorSortField>,
): ApiResource<SubstrateActorPage> {
  const {
    namespace = "",
    filter = "",
    limit = 0,
    pageToken = "",
    sortField = "default",
    sortOrder = "asc",
  } = input;
  return useApiResource(
    ["substrate.actors", namespace, filter, limit, pageToken, sortField, sortOrder],
    () =>
      apiClient.substrate.actors({
        namespace,
        filter,
        limit,
        pageToken,
        sortField,
        sortOrder,
      }),
  );
}

/** One page of workers. The mirror of `useSubstrateActors`. */
export function useSubstrateWorkers(
  input: SubstratePageInput<SubstrateWorkerSortField>,
): ApiResource<SubstrateWorkerPage> {
  const {
    namespace = "",
    filter = "",
    limit = 0,
    pageToken = "",
    sortField = "default",
    sortOrder = "asc",
  } = input;
  return useApiResource(
    ["substrate.workers", namespace, filter, limit, pageToken, sortField, sortOrder],
    () =>
      apiClient.substrate.workers({
        namespace,
        filter,
        limit,
        pageToken,
        sortField,
        sortOrder,
      }),
  );
}
