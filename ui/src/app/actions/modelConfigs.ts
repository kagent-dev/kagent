"use server";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse, ModelConfig, CreateModelConfigRequest, UpdateModelConfigPayload } from "@/types";
import { k8sRefUtils } from "@/lib/k8sUtils";

/**
 * Gets all available models
 * @returns A promise with all models
 */
export async function getModelConfigs(): Promise<BaseResponse<ModelConfig[]>> {
  try {
    const response = await fetchApi<BaseResponse<ModelConfig[]>>("/modelconfigs");

    if (!response) {
      throw new Error("Failed to get model configs");
    }

    // Sort models by name
    response.data?.sort((a, b) => a.ref.localeCompare(b.ref));

    return {
      message: "Models fetched successfully",
      data: response.data,
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
    const response = await fetchApi<BaseResponse<ModelConfig>>(`/modelconfigs/${configRef}`);

    if (!response) {
      throw new Error("Failed to get model config");
    }

    return {
      message: "Model config fetched successfully",
      data: response.data,
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
export async function createModelConfig(config: CreateModelConfigRequest): Promise<BaseResponse<ModelConfig>> {
  try {
    const response = await fetchApi<BaseResponse<ModelConfig>>("/modelconfigs", {
      method: "POST",
      body: JSON.stringify(config),
    });

    if (!response) {
      throw new Error("Failed to create model config");
    }

    return {
      message: "Model config created successfully",
      data: response.data,
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
// The update handler Gets the ModelConfig from the controller-runtime *cached*
// client then Updates it; while a reconcile is in flight the cached resourceVersion
// can be stale, so the write is rejected with a Kubernetes conflict ("the object has
// been modified; please apply your changes to the latest version"). Re-running the
// PUT re-reads a fresher version once the informer cache catches up, and since the
// PUT replaces the full spec it is idempotent.
const CONFLICT_MESSAGE_RE =
  /the object has been modified|operation cannot be fulfilled|please apply your changes|conflict/i;

function isConflictError(error: unknown): boolean {
  return error instanceof Error && CONFLICT_MESSAGE_RE.test(error.message);
}

/** Run `fn`, retrying only on a Kubernetes write conflict with a small linear backoff. */
async function retryOnConflict<T>(fn: () => Promise<T>, maxAttempts = 4): Promise<T> {
  for (let attempt = 1; ; attempt++) {
    try {
      return await fn();
    } catch (error) {
      if (attempt >= maxAttempts || !isConflictError(error)) {
        throw error;
      }
      await new Promise((resolve) => setTimeout(resolve, 150 * attempt));
    }
  }
}

export async function updateModelConfig(
  configRef: string,
  config: UpdateModelConfigPayload
): Promise<BaseResponse<ModelConfig>> {
  try {
    const response = await retryOnConflict(() =>
      fetchApi<BaseResponse<ModelConfig>>(`/modelconfigs/${configRef}`, {
        method: "PUT",
        body: JSON.stringify(config),
        headers: {
          "Content-Type": "application/json",
        },
      })
    );

    if (!response) {
      throw new Error("Failed to update model config");
    }

    revalidatePath("/models"); // Revalidate list page

    const ref = k8sRefUtils.fromRef(configRef);
    revalidatePath(`/models/new?edit=true&name=${ref.name}&namespace=${ref.namespace}`); // Revalidate edit page if needed

    return {
      message: "Model config updated successfully",
      data: response.data,
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
    return { message: "Model config deleted successfully" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting model configuration");
  }
}
