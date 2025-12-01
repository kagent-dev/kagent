'use server'
import { RemoteMCPServer, MCPServer, ToolServerCreateRequest, ToolServerResponse } from "@/types";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse } from "@/types";

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
    
    return response;
  } catch (error) {
    return createErrorResponse<RemoteMCPServer | MCPServer>(error, "Error creating MCP server");
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