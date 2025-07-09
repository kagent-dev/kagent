"use server";
import { createErrorResponse } from "./utils";
import { Provider } from "@/lib/types";
import { BaseResponse } from "@/lib/types";
import { fetchApi } from "./utils";

/**
 * Gets the list of supported providers
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
   * Gets the list of supported memory providers
   * @returns A promise with the list of supported memory providers
   */
export async function getSupportedMemoryProviders(): Promise<BaseResponse<Provider[]>> {
    try {
      const response = await fetchApi<BaseResponse<Provider[]>>("/providers/memories");
  
      if (!response) {
        throw new Error("Failed to get supported memory providers");
      }
  
      return {
        message: "Supported memory providers fetched successfully",
        data: response.data,
      };
    } catch (error) {
      return createErrorResponse<Provider[]>(error, "Error getting supported memory providers");
    }
  }