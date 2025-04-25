"use server";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse } from "@/lib/types";


export type ProviderModel = {
  name: string;
  function_calling: boolean;
}

// Define the type for the expected API response structure
export type ProviderModelsResponse = Record<string, ProviderModel[]>;


/**
 * Gets all available models, grouped by provider.
 * @returns A promise with all models grouped by provider name.
 */
export async function getModels(): Promise<BaseResponse<ProviderModelsResponse>> {
  try {
    // Update fetchApi to expect the new response type
    const response = await fetchApi<ProviderModelsResponse>("/models");

    if (!response) {
      throw new Error("Failed to get models");
    }

    // The data is already in the desired grouped format
    return {
      success: true,
      data: response,
    };
  } catch (error) {
    // Update createErrorResponse type argument
    return createErrorResponse<ProviderModelsResponse>(error, "Error getting model configs");
  }
}
