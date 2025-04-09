"use server";

import { BaseResponse } from "@/lib/types";
import { fetchApi } from "./utils";
import { Component, ToolConfig } from "@/types/datamodel";

export async function getTools(): Promise<BaseResponse<Component<ToolConfig>[]>> {
  try {
    const response = await fetchApi<Component<ToolConfig>[]>("/tools");
    if (!response) {
      throw new Error("Failed to get built-in tools");
    }

    return { success: true, data: response };
  } catch (error) {
    console.error("Error getting built-in tools:", error);
    return { success: false, error: "Failed to get built-in tools. Please try again.", data: [] };
  }
}
