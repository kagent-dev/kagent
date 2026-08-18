"use server";
import { createErrorResponse } from "./utils";
import { Provider, ConfiguredModelProvider, ConfiguredModelProviderModelsResponse } from "@/types";
import { BaseResponse } from "@/types";
import { getModelGrpcGateway } from "@/lib/grpc/client";

/**
 * Gets the list of supported (stock) providers
 * @returns A promise with the list of supported providers
 */
export async function getSupportedModelProviders(): Promise<BaseResponse<Provider[]>> {
  try {
    const gateway = await getModelGrpcGateway();
    return {
      message: "Successfully listed supported model providers",
      data: await gateway.listSupportedModelProviders(),
    };
  } catch (error) {
    return createErrorResponse<Provider[]>(error, "Error getting supported providers");
  }
}

/**
 * Gets the list of configured model providers from ModelProvider CRDs
 * @returns A promise with the list of configured model providers
 */
export async function getConfiguredProviders(): Promise<BaseResponse<ConfiguredModelProvider[]>> {
  try {
    const gateway = await getModelGrpcGateway();
    return {
      message: "Successfully listed configured model providers",
      data: await gateway.listConfiguredProviders(),
    };
  } catch (error) {
    return createErrorResponse<ConfiguredModelProvider[]>(error, "Error getting configured model providers");
  }
}

/**
 * Gets the models for a specific configured model provider
 * @param providerName - The name of the configured model provider
 * @param forceRefresh - Whether to force a refresh of the model list
 * @returns A promise with the list of models for the model provider
 */
export async function getConfiguredProviderModels(
  providerName: string,
  forceRefresh: boolean = false
): Promise<BaseResponse<ConfiguredModelProviderModelsResponse>> {
  try {
    const gateway = await getModelGrpcGateway();
    return {
      message: "Successfully retrieved models",
      data: await gateway.listProviderModels(providerName, forceRefresh),
    };
  } catch (error) {
    return createErrorResponse<ConfiguredModelProviderModelsResponse>(error, `Error getting models for model provider ${providerName}`);
  }
}
