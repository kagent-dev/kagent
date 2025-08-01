"use server";

import { BaseResponse } from "@/lib/types";
import { fetchApi } from "./utils";
import { ToolResponse } from "@/types/datamodel";

/**
 * Gets all available tools
 * @returns A promise with all tools
 */
export async function getTools(): Promise<ToolResponse[]> {
  try {
    const response = await fetchApi<BaseResponse<ToolResponse[]>>("/tools");
    if (!response) {
      throw new Error("Failed to get built-in tools");
    }
    return response.data || [];
  } catch (error) {
    throw new Error(`Error getting built-in tools. ${error}`);
  }
}
