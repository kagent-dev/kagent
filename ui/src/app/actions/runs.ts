"use server";

import { BaseResponse, CreateRunRequest, CreateRunResponse } from "@/lib/types";
import { fetchApi, createErrorResponse } from "./utils";

/**
 * Creates a new run
 * @param payload The run creation payload
 * @returns A promise with the created run
 */
export async function createRun(payload: CreateRunRequest): Promise<BaseResponse<CreateRunResponse>> {
  try {
    const response = await fetchApi<CreateRunResponse>(`/runs`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload),
    });

    if (!response) {
      throw new Error("Failed to create run");
    }

    return { success: true, data: response };
  } catch (error) {
    return createErrorResponse<CreateRunResponse>(error, "Error creating run");
  }
}
