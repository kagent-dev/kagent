"use server";

import { getSystemGrpcGateway } from "@/lib/grpc/client";
import { createErrorResponse } from "./utils";
import type { BaseResponse, SubstrateStatusResponse } from "@/types";

export async function getSubstrateStatus(
  namespace?: string,
): Promise<BaseResponse<SubstrateStatusResponse>> {
  try {
    const gateway = await getSystemGrpcGateway();
    const status = await gateway.getSubstrateStatus(namespace?.trim() ?? "");
    return {
      message: "Successfully listed substrate status",
      data: status,
    };
  } catch (error) {
    return createErrorResponse<SubstrateStatusResponse>(error, "Error loading substrate status");
  }
}
