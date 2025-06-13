'use server'
import { ToolServer, ToolServerWithTools } from "@/types/datamodel";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse } from "@/lib/types";
import { revalidatePath } from "next/cache";

/**
 * Fetches all tool servers
 * @returns Promise with server data
 */
export async function getServers(): Promise<BaseResponse<ToolServerWithTools[]>> {
  try {
    const response = await fetchApi<ToolServerWithTools[]>("/toolservers");

    if (!response) {
      throw new Error("Failed to get tool servers");
    }

    return {
      success: true,
      data: response,
    };
  } catch (error) {
    return createErrorResponse<ToolServerWithTools[]>(error, "Error getting tool servers");
  }
}

/**
 * Deletes a server
 * @param serverName Name of the server to delete
 * @returns Promise with delete result
 */
export async function deleteServer(serverName: string): Promise<BaseResponse<void>> {
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
    return createErrorResponse<void>(error, "Error deleting tool server");
  }
}

/**
 * Creates a new server
 * @param serverData Server data to create
 * @returns Promise with create result
 */
export async function createServer(serverData: ToolServer): Promise<BaseResponse<ToolServer>> {
  try {
    const response = await fetchApi<ToolServer>("/toolservers", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(serverData),
    });

    return {
      success: true,
      data: response,
    };
  } catch (error) {
    return createErrorResponse<ToolServer>(error, "Error creating tool server");
  }
}
