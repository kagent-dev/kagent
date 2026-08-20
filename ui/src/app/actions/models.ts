"use server";
import { createErrorResponse } from "./utils";
import { BaseResponse, ProviderModelsResponse } from "@/types";
import { getModelGrpcGateway } from "@/lib/grpc/client";

/**
 * Gets all available models, grouped by provider.
 * @returns A promise with all models grouped by provider name.
 */
export async function getModels(): Promise<BaseResponse<ProviderModelsResponse>> {
  try {
    const gateway = await getModelGrpcGateway();
    return {
      message: "Successfully listed supported models",
      data: await gateway.listSupportedModels(),
    };
  } catch (error) {
    return createErrorResponse<ProviderModelsResponse>(error, "Error getting model configs");
  }
}
