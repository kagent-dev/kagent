"use server";

import { BaseResponse } from "@/lib/types";
import { fetchApi } from "./utils";
import { Tool } from "@/types/datamodel";

export async function getTools(): Promise<BaseResponse<Tool[]>> {
  try {
    const response = await fetchApi<Tool[]>("/tools");
    if (!response) {
      throw new Error("Failed to get built-in tools");
    }

    return { success: true, data: response };
  } catch (error) {
    console.error("Error getting built-in tools:", error);
    return { success: false, error: "Failed to get built-in tools. Please try again.", data: [] };
  }
}


export async function bulkSaveTools(tools: Tool[]): Promise<BaseResponse<Tool[]>> {
  const response = await fetchApi<Tool[]>(`/tools/bulk`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(tools),
  });

  if (!response) {
    return {
      success: false,
      error: "Failed to save tools. Please try again.",
    };
  }

  return {
    success: true,
    data: response,
  };
}
