"use server";
import { revalidatePath } from "next/cache";
import { createErrorResponse } from "./utils";
import { BaseResponse, ModelConfig, CreateModelConfigRequest, UpdateModelConfigPayload } from "@/types";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { getModelGrpcGateway } from "@/lib/grpc/client";

/**
 * Gets all available models
 * @returns A promise with all models
 */
export async function getModelConfigs(): Promise<BaseResponse<ModelConfig[]>> {
  try {
    const gateway = await getModelGrpcGateway();
    const models = await gateway.listModelConfigs();

    // Sort models by name
    models.sort((a, b) => a.ref.localeCompare(b.ref));

    return {
      message: "Models fetched successfully",
      data: models,
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
    const ref = k8sRefUtils.fromRef(configRef);
    const gateway = await getModelGrpcGateway();
    const modelConfig = await gateway.getModelConfig(ref.namespace, ref.name);

    return {
      message: "Model config fetched successfully",
      data: modelConfig,
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
    const gateway = await getModelGrpcGateway();
    const modelConfig = await gateway.createModelConfig(config);

    return {
      message: "Model config created successfully",
      data: modelConfig,
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
    const ref = k8sRefUtils.fromRef(configRef);
    const gateway = await getModelGrpcGateway();
    const modelConfig = await gateway.updateModelConfig(ref.namespace, ref.name, config);

    revalidatePath("/models");
    revalidatePath(`/models/new?edit=true&name=${ref.name}&namespace=${ref.namespace}`);

    return {
      message: "Model config updated successfully",
      data: modelConfig,
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
    const ref = k8sRefUtils.fromRef(configRef);
    const gateway = await getModelGrpcGateway();
    await gateway.deleteModelConfig(ref.namespace, ref.name);

    revalidatePath("/models");
    return { message: "Model config deleted successfully" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting model configuration");
  }
}
