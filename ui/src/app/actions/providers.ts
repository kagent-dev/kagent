"use server";
import { createErrorResponse } from "./utils";
import { Provider, ConfiguredProvider, ConfiguredProviderModelsResponse } from "@/types";
import { BaseResponse } from "@/types";
import { fetchApi } from "./utils";

/**
 * Gets the list of supported (stock) providers
 * @returns A promise with the list of supported providers
 */
export async function getSupportedModelProviders(): Promise<BaseResponse<Provider[]>> {
    try {
      const response = await fetchApi<BaseResponse<Provider[]>>("/providers/models");
      return response;
    } catch (error) {
      return createErrorResponse<Provider[]>(error, "Error getting supported providers");
    }
  }

/**
 * Gets the list of configured providers from Provider CRDs
 * @returns A promise with the list of configured providers
 */
export async function getConfiguredProviders(): Promise<BaseResponse<ConfiguredProvider[]>> {
  try {
    const response = await fetchApi<BaseResponse<ConfiguredProvider[]>>("/providers/configured");
    return response;
  } catch (error) {
    return createErrorResponse<ConfiguredProvider[]>(error, "Error getting configured providers");
  }
}

/**
 * Gets the models for a specific configured provider
 * @param providerName - The name of the configured provider
 * @param forceRefresh - Whether to force a refresh of the model list
 * @returns A promise with the list of models for the provider
 */
export async function getConfiguredProviderModels(
  providerName: string,
  forceRefresh: boolean = false
): Promise<BaseResponse<ConfiguredProviderModelsResponse>> {
  try {
    const queryParam = forceRefresh ? "?refresh=true" : "";
    const response = await fetchApi<BaseResponse<ConfiguredProviderModelsResponse>>(
      `/providers/configured/${providerName}/models${queryParam}`
    );
    return response;
  } catch (error) {
    return createErrorResponse<ConfiguredProviderModelsResponse>(error, `Error getting models for provider ${providerName}`);
  }
}
