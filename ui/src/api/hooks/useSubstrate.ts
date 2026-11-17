import { apiClient } from "../client";
import type {
  SubstrateActorPage,
  SubstrateSummary,
  SubstrateWorkerPage,
  SubstrateWorkerPoolEntry,
} from "../domain/substrate";
import type {
  SubstrateActorPageInput,
  SubstrateWorkerPageInput,
  SubstrateScopeInput,
} from "../operations";
import { type ApiResource, useApiResource } from "./useApiResource";

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
 * The worker pools a harness in `namespace` may name. A form that offers them still
 * accepts a typed name, so a caller who may not list pools is not blocked.
 */
export function useWorkerPools(namespace?: string): ApiResource<SubstrateWorkerPoolEntry[]> {
  return useApiResource(namespace ? ["substrate.workerPools", namespace] : null, () =>
    apiClient.substrate.workerPools(namespace),
  );
}

/** One page in Substrate's native order. */
export function useSubstrateActors(input: SubstrateActorPageInput): ApiResource<SubstrateActorPage> {
  const { atespace = "", limit = 0, pageToken = "" } = input;
  return useApiResource(
    ["substrate.actors", atespace, limit, pageToken],
    () => apiClient.substrate.actors({ atespace, limit, pageToken }),
  );
}

/** Namespace filtering may leave an empty page with a next token. */
export function useSubstrateWorkers(input: SubstrateWorkerPageInput): ApiResource<SubstrateWorkerPage> {
  const { namespace = "", limit = 0, pageToken = "" } = input;
  return useApiResource(
    ["substrate.workers", namespace, limit, pageToken],
    () => apiClient.substrate.workers({ namespace, limit, pageToken }),
  );
}
