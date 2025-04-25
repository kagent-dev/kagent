"use server";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse, Model, CreateModelConfigPayload, UpdateModelConfigPayload, Provider } from "@/lib/types";

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

    // Sort models by name
    response.sort((a, b) => a.name.localeCompare(b.name));

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

/**
 * Creates a new model configuration
 * @param config The model configuration to create
 * @returns A promise with the created model
 */
export async function createModelConfig(config: CreateModelConfigPayload): Promise<BaseResponse<Model>> {
  try {
    const response = await fetchApi<Model>("/modelconfigs", {
      method: "POST",
      body: JSON.stringify(config),
    });

    if (!response) {
      throw new Error("Failed to create model config");
    }

    return {
      success: true,
      data: response,
    };
  } catch (error) {
    return createErrorResponse<Model>(error, "Error creating model configuration");
  }
}

/**
 * Updates an existing model configuration
 * @param configName The name of the model configuration to update
 * @param config The updated configuration data
 * @returns A promise with the updated model
 */
export async function updateModelConfig(
  configName: string,
  config: UpdateModelConfigPayload
): Promise<BaseResponse<Model>> {
  try {
    const response = await fetchApi<Model>(`/modelconfigs/${configName}`, {
      method: "PUT", // Or PATCH depending on backend implementation
      body: JSON.stringify(config),
      headers: {
        "Content-Type": "application/json",
      },
    });

    if (!response) {
      throw new Error("Failed to update model config");
    }
    
    revalidatePath("/models"); // Revalidate list page
    revalidatePath(`/models/new?edit=true&id=${configName}`); // Revalidate edit page if needed

    return {
      success: true,
      data: response,
    };
  } catch (error) {
    return createErrorResponse<Model>(error, "Error updating model configuration");
  }
}

/**
 * Deletes a model configuration
 * @param configName The name of the model configuration to delete
 * @returns A promise with the deleted model
 */
export async function deleteModelConfig(configName: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/modelconfigs/${configName}`, {
      method: "DELETE",
      headers: {
        "Content-Type": "application/json",
      },
    });
    
    revalidatePath("/models");
    return { success: true };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting model configuration");
  }
}
