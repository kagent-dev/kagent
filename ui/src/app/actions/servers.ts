'use server'
import { Component, ToolServer, ToolServerConfig } from "@/types/datamodel";
import { fetchApi, getCurrentUserId } from "./utils";
import { BaseResponse } from "@/lib/types";

/**
 * Fetches all tool servers
 * @returns Promise with server data
 */
export async function getServers(): Promise<BaseResponse<ToolServer[]>> {
  const response = await fetchApi<ToolServer[]>("/toolservers");

  if (!response) {
    return {
      success: false,
      error: "Failed to get tool servers. Please try again.",
      data: [],
    };
  }

  return {
    success: true,
    data: response,
  };
}

/**
 * Refreshes tools for a specific server
 * @param serverId ID of the server to refresh
 * @returns Promise with refresh result
 */
export async function refreshServerTools(serverId: number) {
  const response = await fetchApi(`/toolservers/${serverId}/refresh`, {
    method: "POST",
  });

  return response;
}

/**
 * Deletes a server
 * @param serverId ID of the server to delete
 * @returns Promise with delete result
 */
export async function deleteServer(serverId: number) {
  try {
    await fetchApi(`/toolservers/${serverId}`, {
      method: "DELETE",
      headers: {
        "Content-Type": "application/json",
      },
    });

    return { success: true };
  } catch (error) {
    console.error("Error deleting tool server:", error);
    return { success: false, error: "Failed to delete tool server. Please try again." };
  }
}

/**
 * Creates a new server
 * @param serverData Server data to create
 * @returns Promise with create result
 */
export async function createServer(serverData: Component<ToolServerConfig>): Promise<BaseResponse<ToolServer>> {
  const userId = await getCurrentUserId();
  const data = {
    user_id: userId,
    component: {
      ...serverData,
    },
  };

  const response = await fetchApi<ToolServer>("/toolservers", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(data),
  });

  if (!response) {
    return {
      success: false,
      error: "Failed to create server. Please try again.",
    };
  }

  return {
    success: true,
    data: response,
  };
}
