import { apiClient } from "../client";
import type {
  ModelConfig,
  Provider,
  ProviderModelsResponse,
} from "../domain/models";
import {
  type ApiCollectionResource,
  type ApiResource,
  useApiCollection,
  useApiResource,
} from "./useApiResource";

/** Every model configuration agents can be pointed at. */
export function useModels(): ApiCollectionResource<ModelConfig> {
  return useApiCollection(["models.list"], () => apiClient.models.list());
}

/** One model configuration. Holds off until both parts of the ref are known. */
export function useModel(
  namespace: string | undefined,
  name: string | undefined,
): ApiResource<ModelConfig> {
  return useApiResource(
    namespace && name ? ["models.get", namespace, name] : null,
    () => apiClient.models.get(namespace!, name!),
  );
}

/** Models on offer, grouped by provider — for the "create model" picker. */
export function useProviderModels(): ApiResource<ProviderModelsResponse> {
  return useApiResource(["models.providerModels"], () =>
    apiClient.models.providerModels(),
  );
}

/**
 * The providers the controller supports, each with the parameters its config
 * block accepts. Feeds the provider picker and the per-provider parameter fields.
 */
export function useProviders(): ApiResource<Provider[]> {
  return useApiResource(["models.providers"], () => apiClient.models.providers());
}
