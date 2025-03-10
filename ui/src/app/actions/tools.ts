"use server";

import { BaseResponse, DiscoverToolsRequest } from "@/lib/types";
import { fetchApi } from "./utils";
import { Component, Tool, ToolConfig } from "@/types/datamodel";

export async function getBuiltInTools(): Promise<BaseResponse<Component<ToolConfig>[]>> {
  try {
    const response = await fetchApi<Tool[]>("/tools");

    // Get all component configs from response
    const tools = response.map((c) => c.component);

    if (!response) {
      throw new Error("Failed to get built-in tools");
    }

    return { success: true, data: tools };
  } catch (error) {
    console.error("Error getting built-in tools:", error);
    return { success: false, error: "Failed to get built-in tools. Please try again.", data: [] };
  }
}

export async function discoverMCPTools(payload: DiscoverToolsRequest): Promise<BaseResponse<Tool[]>> {
  try {
    const response = await fetchApi<Tool[]>(`/tools/discover`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload),
    });

    if (!response) {
      throw new Error("Failed to discover tools");
    }

    return { success: true, data: response };
  } catch (error) {
    console.error("Error discovering tools:", error);
    return { success: false, error: "Failed to discover tools. Please try again.", data: [] };
  }
}
