"use server";
import { createErrorResponse } from "./utils";
import { Provider } from "@/types";
import { BaseResponse } from "@/types";
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
