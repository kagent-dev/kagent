"use server";

import { BaseResponse } from "@/lib/types";
import { fetchApi } from "./utils";
import { DBTool } from "@/types/datamodel";

export async function getTools(): Promise<BaseResponse<DBTool[]>> {
  try {
    const response = await fetchApi<DBTool[]>("/tools");
    if (!response) {
      throw new Error("Failed to get built-in tools");
    }

    return { success: true, data: response };
  } catch (error) {
    console.error("Error getting built-in tools:", error);
    return { success: false, error: "Failed to get built-in tools. Please try again.", data: [] };
  }
}
