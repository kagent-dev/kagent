"use server";
import { createErrorResponse } from "./utils";
import { Provider } from "@/lib/types";
import { BaseResponse } from "@/lib/types";
import { fetchApi } from "./utils";

/**
 * Gets the list of supported providers
 * @returns A promise with the list of supported providers
 */
export async function getSupportedProviders(): Promise<BaseResponse<Provider[]>> {
    try {
      const response = await fetchApi<Provider[]>("/providers");
  
      if (!response) {
        throw new Error("Failed to get supported providers");
      }
  
      return {
        success: true,
        data: response,
      };
    } catch (error) {
      return createErrorResponse<Provider[]>(error, "Error getting supported providers");
    }
  }