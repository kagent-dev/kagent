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
 * Counting rows on screen and labelling the result "Actors" would report 20 for a
 * cluster running four hundred thousand.
 */
export function useSubstrateSummary(namespace?: string): ApiResource<SubstrateSummary> {
  return useApiResource(["substrate.summary", namespace ?? ""], () =>
    apiClient.substrate.summary(namespace),
  );
}

/**
 * One page of actors, narrowed server-side.
 *
 * The filter and the page token are part of the key, so typing in the search box
 * re-reads rather than re-rendering the previous answer — which is the whole point
 * of the filter being server-side. Filtering here instead would search only the
 * rows already fetched, and a match on page nine would read as "no matches".
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
  // The sort is part of the key for the same reason the filter is: it changes what
  // the server returns, so asking for a different one must re-read rather than
  // re-render the previous answer in a new order — which would be the client-side
  // sorting this replaced.
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

/** One page of worker assignments. The mirror of `useSubstrateActors`. */
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
