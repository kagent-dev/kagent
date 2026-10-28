import { apiClient } from "../client";
import type { ApiError } from "../ApiError";
import type {
  ModelConfig,
  Provider,
  ProviderModelsResponse,
 ConfiguredModelProvider,
 ModelDiscoveryResult,
} from "../domain/models";
import { type ApiResource, useApiResource } from "./useApiResource";

/** Every model configuration agents can be pointed at. */
export function useModels(): ApiResource<ModelConfig[]> {
  return useApiResource(["models.list"], () => apiClient.models.list());
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

export function useConfiguredModelProviders(): ApiResource<ConfiguredModelProvider[]> {
 return useApiResource(["models.configuredProviders"], () => apiClient.models.configuredProviders());
}
export function useModelDiscovery(namespace?: string, name?: string): ApiResource<ModelDiscoveryResult> {
 return useApiResource(namespace && name ? ["models.discovery", namespace, name] : null,
   () => apiClient.models.discovery(namespace!,name!), {
     refreshInterval: 10000,
     // SWR pauses polling on errors. Override the app's disabled retries so
     // transient read failures recover; missing/forbidden sources need user action.
     shouldRetryOnError: (error: ApiError) => error.isUnreachable || error.status === 429 || (error.status !== undefined && error.status >= 500),
     errorRetryInterval: 5000,
     errorRetryCount: 5,
   });
}
