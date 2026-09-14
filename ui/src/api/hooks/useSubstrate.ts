import { apiClient } from "../client";
import type {
  SubstrateActorPage,
  SubstrateStatusResponse,
  SubstrateSummary,
  SubstrateWorkerPage,
} from "../domain/substrate";
import type {
  SubstrateActorPageInput,
  SubstrateWorkerPageInput,
  SubstrateScopeInput,
} from "../operations";
import { type ApiResource, useApiResource } from "./useApiResource";

/** Inventory with independent ATE atespace and Kubernetes namespace filters. */
export function useSubstrateStatus(
  scope: SubstrateScopeInput = {},
): ApiResource<SubstrateStatusResponse> {
  return useApiResource(["substrate.status", scope.namespace ?? "", scope.atespace ?? ""], () =>
    apiClient.substrate.status(scope),
  );
}

/**
 * Counts across every page in scope, plus worker pools and actor templates.
 * ATE provides no aggregates, so computing these counts walks every page in scope.
 */
export function useSubstrateSummary(scope: SubstrateScopeInput = {}): ApiResource<SubstrateSummary> {
  return useApiResource(["substrate.summary", scope.namespace ?? "", scope.atespace ?? ""], () =>
    apiClient.substrate.summary(scope),
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
  input: SubstrateActorPageInput,
): ApiResource<SubstrateActorPage> {
  const {
    atespace = "",
    filter = "",
    limit = 0,
    pageToken = "",
    sortField = "default",
    sortOrder = "asc",
  } = input;
  return useApiResource(
    ["substrate.actors", atespace, filter, limit, pageToken, sortField, sortOrder],
    () =>
      apiClient.substrate.actors({
        atespace,
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
  input: SubstrateWorkerPageInput,
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
