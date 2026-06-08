"use server";

import { fetchApi, createErrorResponse } from "./utils";
import type { BaseResponse, SubstrateStatusResponse } from "@/types";

export async function getSubstrateStatus(
  namespace?: string,
): Promise<BaseResponse<SubstrateStatusResponse>> {
  try {
    const qs = namespace?.trim() ? `?namespace=${encodeURIComponent(namespace.trim())}` : "";
    const response = await fetchApi<BaseResponse<SubstrateStatusResponse>>(`/substrate/status${qs}`);
    if (!response?.data) {
      throw new Error("Failed to load substrate status");
    }
    return {
      message: response.message ?? "Substrate status fetched",
      data: response.data,
    };
  } catch (error) {
    return createErrorResponse<SubstrateStatusResponse>(error, "Error loading substrate status");
  }
}
