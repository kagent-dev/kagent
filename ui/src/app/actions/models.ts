"use server";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse, ProviderModelsResponse } from "@/types";

/**
 * Gets all available models, grouped by provider.
 * @returns A promise with all models grouped by provider name.
 */
export async function getModels(): Promise<BaseResponse<ProviderModelsResponse>> {
  try {
    // Update fetchApi to expect the new response type
    const response = await fetchApi<BaseResponse<ProviderModelsResponse>>("/models");
    return response;
  } catch (error) {
    // Update createErrorResponse type argument
    return createErrorResponse<ProviderModelsResponse>(error, "Error getting model configs");
  }
}
