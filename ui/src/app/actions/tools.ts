"use server";

import { BaseResponse, ToolsResponse } from "@/types";
import { fetchApi } from "./utils";

/**
 * Gets all available tools
 * @returns A promise with all tools
 */
export async function getTools(): Promise<ToolsResponse[]> {
  try {
    const response = await fetchApi<BaseResponse<ToolsResponse[]>>("/tools");
    if (!response) {
      throw new Error("Failed to get built-in tools");
    }
    return response.data || [];
  } catch (error) {
    throw new Error(`Error getting built-in tools. ${error}`);
  }
}
