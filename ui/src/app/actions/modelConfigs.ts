"use server";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse, ModelConfig, CreateModelConfigPayload, UpdateModelConfigPayload } from "@/lib/types";
import { k8sRefUtils } from "@/lib/k8sUtils";

/**
 * Gets all available models
 * @returns A promise with all models
 */
export async function getModelConfigs(): Promise<BaseResponse<ModelConfig[]>> {
  try {
    const response = await fetchApi<ModelConfig[]>("/modelconfigs");

    if (!response) {
      throw new Error("Failed to get model configs");
    }

    // Sort models by name
    response.sort((a, b) => a.ref.localeCompare(b.ref));

    return {
      success: true,
      data: response,
    };
  } catch (error) {
    return createErrorResponse<ModelConfig[]>(error, "Error getting model configs");
  }
}

/**
 * Gets a specific model by name
 * @param configRef The model configuration ref string
 * @returns A promise with the model data
 */
export async function getModelConfig(configRef: string): Promise<BaseResponse<ModelConfig>> {
  try {
    const response = await fetchApi<ModelConfig>(`/modelconfigs/${configRef}`);

    if (!response) {
      throw new Error("Failed to get model config");
    }

    return {
      success: true,
      data: response,
    };
  } catch (error) {
    return createErrorResponse<ModelConfig>(error, "Error getting model");
  }
}

/**
 * Creates a new model configuration
 * @param config The model configuration to create
 * @returns A promise with the created model
 */
export async function createModelConfig(config: CreateModelConfigPayload): Promise<BaseResponse<ModelConfig>> {
  try {
    const response = await fetchApi<ModelConfig>("/modelconfigs", {
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
    return createErrorResponse<ModelConfig>(error, "Error creating model configuration");
  }
}

/**
 * Updates an existing model configuration
 * @param configRef The ref string of the model configuration to update
 * @param config The updated configuration data
 * @returns A promise with the updated model
 */
export async function updateModelConfig(
  configRef: string,
  config: UpdateModelConfigPayload
): Promise<BaseResponse<ModelConfig>> {
  try {
    const response = await fetchApi<ModelConfig>(`/modelconfigs/${configRef}`, {
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

    const ref = k8sRefUtils.fromRef(configRef);
    revalidatePath(`/models/new?edit=true&name=${ref.name}&namespace=${ref.namespace}`); // Revalidate edit page if needed

    return {
      success: true,
      data: response,
    };
  } catch (error) {
    return createErrorResponse<ModelConfig>(error, "Error updating model configuration");
  }
}

/**
 * Deletes a model configuration
 * @param configRef The ref string of the model configuration to delete
 * @returns A promise with the deleted model
 */
export async function deleteModelConfig(configRef: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/modelconfigs/${configRef}`, {
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
