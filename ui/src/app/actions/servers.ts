import { MCPServer } from "@/types/datamodel";
import { fetchApi } from "./utils";

// Mock data for servers
const mockServers = [
    {
      id: 1,
      user_id: "user-1",
      server_id: "kubernetes",
      created_at: "2025-03-01T09:15:00Z",
      updated_at: "2025-03-01T09:15:00Z",
      last_connected: "2025-03-12T14:30:00Z",
      is_active: true,
      component: {
        provider: "mcp.server.kubernetes",
        component_type: "mcp_server",
        description: "Kubernetes MCP Server",
        label: "Kubernetes MCP",
        config: {
          type: "command",
          details: "npx mcp-server-kubernetes"
        }
      }
    },
    {
      id: 2,
      user_id: "user-1",
      server_id: "cloud-mcp",
      created_at: "2025-02-15T11:10:00Z",
      updated_at: "2025-02-15T11:10:00Z",
      last_connected: "2025-03-10T08:45:00Z",
      is_active: true,
      component: {
        provider: "mcp.server.cloud",
        component_type: "mcp_server",
        description: "Cloud MCP Service",
        label: "Cloud MCP",
        config: {
          type: "url",
          details: "https://api.example.com/mcp-tools"
        }
      }
    },
    {
      id: 3,
      user_id: "user-1",
      server_id: "local-mcp",
      created_at: "2025-01-20T16:25:00Z",
      updated_at: "2025-01-20T16:25:00Z",
      last_connected: "2025-03-05T17:20:00Z",
      is_active: true,
      component: {
        provider: "mcp.server.local",
        component_type: "mcp_server",
        description: "Local MCP Tools",
        label: "Local MCP",
        config: {
          type: "command",
          details: "npx mcp-tools-standard"
        }
      }
    }
  ];
  
  /**
   * Fetches all MCP servers
   * @returns Promise with server data
   */
  export async function getServers(): Promise<MCPServer[]> {

    const response = await fetchApi<MCPServer[]>("/servers");
    // Simulate API delay
    await new Promise(resolve => setTimeout(resolve, 800));
    
    // Return mock data
    return {
      status: true,
      data: mockServers
    };
  }
  
  /**
   * Refreshes tools for a specific server
   * @param serverId ID of the server to refresh
   * @returns Promise with refresh result
   */
  export async function refreshServerTools(serverId: number) {
    // Simulate API delay
    await new Promise(resolve => setTimeout(resolve, 1500));
    
    // Find the server
    const server = mockServers.find(s => s.id === serverId);
    
    if (!server) {
      return {
        status: false,
        message: "Server not found"
      };
    }
    
    // Update last_connected timestamp
    server.last_connected = new Date().toISOString();
    
    // Return success
    return {
      status: true,
      message: "Server refreshed successfully. Added 3 new tools.",
      data: {
        new_tools_count: 3,
        total_tools_count: 12
      }
    };
  }
  
  /**
   * Deletes a server
   * @param serverId ID of the server to delete
   * @returns Promise with delete result
   */
  export async function deleteServer(serverId: number) {
    // Simulate API delay
    await new Promise(resolve => setTimeout(resolve, 1000));
    
    // Check if server exists
    const server = mockServers.find(s => s.id === serverId);
    
    if (!server) {
      return {
        status: false,
        message: "Server not found"
      };
    }
    
    // In a real implementation, this would make an API call
    // For mock purposes, we'll just return success
    return {
      status: true,
      message: "Server and associated tools deleted successfully."
    };
  }
  
  /**
   * Creates a new server
   * @param serverData Server data to create
   * @returns Promise with create result
   */
  export async function createServer(serverData: any) {
    // Simulate API delay
    await new Promise(resolve => setTimeout(resolve, 1200));
    
    // In a real implementation, this would make an API call
    // For mock purposes, we'll just return success
    return {
      status: true,
      message: "Server created successfully",
      data: {
        id: Math.floor(Math.random() * 1000) + 10,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        ...serverData
      }
    };
  }