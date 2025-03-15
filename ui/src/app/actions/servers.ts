import { Component, MCPServer, MCPServerConfig, ToolConfig } from "@/types/datamodel";
import { fetchApi, getCurrentUserId } from "./utils";
import { BaseResponse, SseServerParams, StdioServerParameters } from "@/lib/types";


  /**
   * Fetches all MCP servers
   * @returns Promise with server data
   */
  export async function getServers(): Promise<BaseResponse<MCPServer[]>> {
    const response = await fetchApi<MCPServer[]>("/mcp");

    if (!response) {
        return {
            success: false,
            error: "Failed to get MCP servers. Please try again.",
            data: []
        };
    }

    return {
      success: true,
      data: response
    };
  }
  
  /**
   * Refreshes tools for a specific server
   * @param serverId ID of the server to refresh
   * @returns Promise with refresh result
   */
  export async function refreshServerTools(serverId: number) {

    const response = await fetchApi(`/mcp/${serverId}/refresh`, {
      method: "POST",
    });


    if (!response) {
      return {
        status: false,
        message: "Failed to refresh server. Please try again.",
      };
    }

    return {
      status: true,
      message: "Server tools refreshed successfully."
    };
  }
  
  /**
   * Deletes a server
   * @param serverId ID of the server to delete
   * @returns Promise with delete result
   */
  export async function deleteServer(serverId: number) {

    try {
        await fetchApi(`/mcp/${serverId}`, {
          method: "DELETE",
          headers: {
            "Content-Type": "application/json",
          },
        });
    
        return { success: true };
      } catch (error) {
        console.error("Error deleting MCP server:", error);
        return { success: false, error: "Failed to delete MCP server. Please try again." };
      }
  }
  
  /**
   * Creates a new server
   * @param serverData Server data to create
   * @returns Promise with create result
   */
  export async function createServer(serverData: MCPServerConfig): Promise<BaseResponse<MCPServer>> {

    const userId = await getCurrentUserId();
    const data = {
        user_id: userId,
        component: {
            config: {
                ...serverData,
            }
        }
    }

    const response = await fetchApi<MCPServer>("/mcp", {
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
      data: response
    };
  }

