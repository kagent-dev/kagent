import { apiClient } from "../client";
import type {
  SubstrateActorPage,
  SubstrateStatusResponse,
  SubstrateSummary,
  SubstrateWorkerPage,
} from "../domain/substrate";
import type { SubstratePageInput } from "../operations";
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
 * against milliseconds for a page. Poll it least often; `computedAt` says how old
 * the answer is.
 */
export function useSubstrateSummary(namespace?: string): ApiResource<SubstrateSummary> {
  return useApiResource(["substrate.summary", namespace ?? ""], () =>
    apiClient.substrate.summary(namespace),
  );
}

/**
 * One page of actors.
 *
 * The key is the scope, the page size and the token — everything that changes which
 * rows come back, and nothing else. Ordering and searching are deliberately not in
 * it: ate-api offers neither, so neither does the controller, and a header click or
 * a keystroke rearranges the rows already in hand rather than asking for them again.
 * That is the whole of what reordering costs now; it used to be a re-read of the
 * entire inventory.
 *
 * What it does not buy is a search across the cluster. A page is what there is to
 * search, and the page saying so is the difference between a narrow answer and a
 * wrong one.
 */
export function useSubstrateActors(
  input: SubstratePageInput,
): ApiResource<SubstrateActorPage> {
  const { namespace = "", limit = 0, pageToken = "" } = input;
  return useApiResource(["substrate.actors", namespace, limit, pageToken], () =>
    apiClient.substrate.actors({ namespace, limit, pageToken }),
  );
}

/** One page of workers. The mirror of `useSubstrateActors`. */
export function useSubstrateWorkers(
  input: SubstratePageInput,
): ApiResource<SubstrateWorkerPage> {
  const { namespace = "", limit = 0, pageToken = "" } = input;
  return useApiResource(["substrate.workers", namespace, limit, pageToken], () =>
    apiClient.substrate.workers({ namespace, limit, pageToken }),
  );
}
