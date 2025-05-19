"use server";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse, ModelConfig, CreateModelConfigPayload, UpdateModelConfigPayload } from "@/lib/types";

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

    // Sort models by namespace/name
    response.sort((a, b) => {
      const aFullName = `${a.namespace}/${a.name}`;
      const bFullName = `${b.namespace}/${b.name}`;
      return aFullName.localeCompare(bFullName);
    });

    return {
      success: true,
      data: response,
    };
  } catch (error) {
    return createErrorResponse<ModelConfig[]>(error, "Error getting model configs");
  }
}

/**
 * Gets a specific model by namespace and name
 * @param namespace The model configuration namespace
 * @param configName The model configuration name
 * @returns A promise with the model data
 */
export async function getModelConfig(namespace: string, configName: string): Promise<BaseResponse<ModelConfig>> {
  try {
    const response = await fetchApi<ModelConfig>(`/modelconfigs/${namespace}/${configName}`);

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
 * @param namespace The namespace of the model configuration to update
 * @param configName The name of the model configuration to update
 * @param config The updated configuration data
 * @returns A promise with the updated model
 */
export async function updateModelConfig(namespace: string, configName: string, config: UpdateModelConfigPayload): Promise<BaseResponse<ModelConfig>> {
  try {
    const response = await fetchApi<ModelConfig>(`/modelconfigs/${namespace}/${configName}`, {
      method: "PUT",
      body: JSON.stringify(config),
      headers: {
        "Content-Type": "application/json",
      },
    });

    if (!response) {
      throw new Error("Failed to update model config");
    }

    revalidatePath("/models"); // Revalidate list page
    revalidatePath(`/models/new?edit=true&namespace=${namespace}&id=${configName}`); // Revalidate edit page if needed

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
 * @param namespace The namespace of the model config to delete
 * @param configName The name of the model configuration to delete
 * @returns A promise with the deleted model
 */
export async function deleteModelConfig(namespace: string, configName: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/modelconfigs/${namespace}/${configName}`, {
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
