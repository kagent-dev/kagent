'use server'
import { ToolServer, ToolServerWithTools } from "@/types/datamodel";
import { fetchApi } from "./utils";
import { BaseResponse } from "@/lib/types";
import { revalidatePath } from "next/cache";

/**
 * Fetches all tool servers
 * @returns Promise with server data
 */
export async function getServers(): Promise<BaseResponse<ToolServerWithTools[]>> {
  const response = await fetchApi<ToolServerWithTools[]>("/toolservers");

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
 * Deletes a server
 * @param serverName NAme of the server to delete
 * @returns Promise with delete result
 */
export async function deleteServer(serverName: string) {
  try {
    await fetchApi(`/toolservers/${serverName}`, {
      method: "DELETE",
      headers: {
        "Content-Type": "application/json",
      },
    });

     revalidatePath("/servers");
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
export async function createServer(serverData: ToolServer): Promise<BaseResponse<ToolServer>> {
  const response = await fetchApi<ToolServer>("/toolservers", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(serverData),
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
