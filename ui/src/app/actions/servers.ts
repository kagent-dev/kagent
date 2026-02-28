'use server'
import { RemoteMCPServer, MCPServer, ToolServerCreateRequest, ToolServerResponse, ToolServerDetailResponse } from "@/types";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse } from "@/types";
import { revalidatePath } from "next/cache";

/**
 * Fetches all tool servers
 * @returns Promise with server data
 */
export async function getServers(): Promise<BaseResponse<ToolServerResponse[]>> {
  try {
    const response = await fetchApi<BaseResponse<ToolServerResponse[]>>(`/toolservers`);

    if (!response) {
      throw new Error("Failed to get MCP servers");
    }

    return {
      message: "MCP servers fetched successfully",
      data: response.data,
    };  
  } catch (error) {
    return createErrorResponse<ToolServerResponse[]>(error, "Error getting MCP servers");
  }
}

/**
 * Deletes a server
 * @param serverName Server name to delete (format: namespace/name)
 * @returns Promise with delete result
 */
export async function deleteServer(serverName: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi<BaseResponse<void>>(`/toolservers/${serverName}`, {
      method: "DELETE",
    });

    revalidatePath("/servers");
    return {
      message: "MCP server deleted successfully",
    };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting MCP server");
  }
}

/**
 * Creates a new server
 * @param serverData Server data to create
 * @returns Promise with create result
 */
export async function createServer(serverData: ToolServerCreateRequest): Promise<BaseResponse<RemoteMCPServer | MCPServer>> {
  try {
    const response = await fetchApi<BaseResponse<RemoteMCPServer | MCPServer>>("/toolservers", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(serverData),
    });

    revalidatePath("/servers");
    return response;
  } catch (error) {
    return createErrorResponse<RemoteMCPServer | MCPServer>(error, "Error creating MCP server");
  }
}

/**
 * Fetches a single tool server by ref (namespace/name) and groupKind
 * @param serverRef Server ref in format "namespace/name"
 * @param groupKind GroupKind of the tool server (e.g. "RemoteMCPServer.kagent.dev")
 * @returns Promise with detailed server data including full spec
 */
export async function getServer(serverRef: string, groupKind: string): Promise<BaseResponse<ToolServerDetailResponse>> {
  try {
    const response = await fetchApi<BaseResponse<ToolServerDetailResponse>>(`/toolservers/${serverRef}/${encodeURIComponent(groupKind)}`);

    if (!response) {
      throw new Error("Failed to get MCP server");
    }

    return {
      message: "MCP server fetched successfully",
      data: response.data,
    };
  } catch (error) {
    return createErrorResponse<ToolServerDetailResponse>(error, "Error getting MCP server");
  }
}

/**
 * Updates an existing server
 * @param serverRef Server ref in format "namespace/name"
 * @param groupKind GroupKind of the tool server (e.g. "RemoteMCPServer.kagent.dev")
 * @param serverData Updated server data
 * @returns Promise with update result
 */
export async function updateServer(serverRef: string, groupKind: string, serverData: ToolServerCreateRequest): Promise<BaseResponse<RemoteMCPServer | MCPServer>> {
  try {
    const response = await fetchApi<BaseResponse<RemoteMCPServer | MCPServer>>(`/toolservers/${serverRef}/${encodeURIComponent(groupKind)}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(serverData),
    });

    return response;
  } catch (error) {
    return createErrorResponse<RemoteMCPServer | MCPServer>(error, "Error updating MCP server");
  }
}

/**
 * Fetches all supported tool server types
 * @returns Promise with server data
 */
export async function getToolServerTypes(): Promise<BaseResponse<string[]>> {
  try {
    const response = await fetchApi<BaseResponse<string[]>>(`/toolservertypes`);

    if (!response) {
      throw new Error("Failed to get tool server types");
    }

    return {
      message: "Tool server types fetched successfully",
      data: response.data,
    };  
  } catch (error) {
    return createErrorResponse<string[]>(error, "Error getting tool server types");
  }
}