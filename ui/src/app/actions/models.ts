"use server";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse, Model } from "@/lib/types";

/**
 * Gets all available models
 * @returns A promise with all models
 */
export async function getModels(): Promise<BaseResponse<Model[]>> {
  try {
    const response = await fetchApi<Model[]>("/modelconfigs");

    if (!response) {
      throw new Error("Failed to get models");
    }

    return {
      success: true,
      data: response,
    };
  } catch (error) {
    return createErrorResponse<Model[]>(error, "Error getting models");
  }
}

/**
 * Gets a specific model by name
 * @param configName The model configuration name
 * @returns A promise with the model data
 */
export async function getModel(configName: string): Promise<BaseResponse<Model>> {
  try {
    const response = await fetchApi<Model>(`/modelconfigs/${configName}`);

    if (!response) {
      throw new Error("Failed to get model");
    }

    return {
      success: true,
      data: response,
    };
  } catch (error) {
    return createErrorResponse<Model>(error, "Error getting model");
  }
}