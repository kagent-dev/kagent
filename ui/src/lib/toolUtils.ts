import { k8sRefUtils } from "@/lib/k8sUtils";
import type{ Tool, McpServerTool, ToolsResponse, DiscoveredTool, TypedLocalReference, AgentResponse } from "@/types";


// Constants for MCP server types and defaults
const DEFAULT_API_GROUP = "kagent.dev";
const DEFAULT_MCP_KIND = "MCPServer";
const SERVICE_KIND = "Service";
const QUERYDOC_SERVER_NAME = "kagent-querydoc";
const TOOL_SERVER_NAME = "kagent-tool-server";
const MCP_SERVER_TYPE = "McpServer" as const;

export const isAgentTool = (value: unknown): value is { type: "Agent"; agent: TypedLocalReference } => {
  if (!value || typeof value !== "object") return false;

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const obj = value as any;
  return obj.type === "Agent" && obj.agent && typeof obj.agent === "object" && typeof obj.agent.name === "string";
};

// Compare server names it handles both "namespace/name" refs and plain names
export const serverNamesMatch = (serverName1: string, serverName2: string): boolean => {
  if (!serverName1 || !serverName2) return false;
  
  if (serverName1 === serverName2) return true;
  
  try {
    const name1 = k8sRefUtils.isValidRef(serverName1) 
      ? k8sRefUtils.fromRef(serverName1).name 
      : serverName1;
    const name2 = k8sRefUtils.isValidRef(serverName2) 
      ? k8sRefUtils.fromRef(serverName2).name 
      : serverName2;
    
    return name1 === name2;
  } catch {
    return false;
  }
};

export const isAgentResponse = (value: unknown): value is AgentResponse => {
  if (!value || typeof value !== "object") return false;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const obj = value as any;
  return !!obj.agent && typeof obj.agent === "object" && !!obj.agent.metadata && typeof obj.agent.metadata?.name === "string";
};

export const isMcpTool = (tool: unknown): tool is { type: "McpServer"; mcpServer: McpServerTool } => {
  if (!tool || typeof tool !== "object") return false;

  const possibleTool = tool as Partial<Tool>;

  return (
    possibleTool.type === "McpServer" &&
    !!possibleTool.mcpServer &&
    typeof possibleTool.mcpServer === "object" &&
    Array.isArray(possibleTool.mcpServer.toolNames)
  );
};

// Group MCP tools by server
export const groupMcpToolsByServer = (tools: Tool[]): {
  groupedTools: Tool[];
  errors: string[];
} => {
  if (!tools || !Array.isArray(tools)) {
    return { groupedTools: [], errors: ["Invalid input: tools must be an array"] };
  }

  const mcpToolsByServer = new Map<string, Set<string>>();
  const nonMcpTools: Tool[] = [];
  const errors: string[] = [];

  tools.forEach((tool) => {
    if (isMcpTool(tool)) {
      const mcpServer = (tool as Tool).mcpServer;
      const serverNameRef = mcpServer?.name || "";
      const toolNames = mcpServer?.toolNames || [];

      // Get existing set or create new one
      const existingNames = mcpToolsByServer.get(serverNameRef) || new Set<string>();
      toolNames.forEach(name => existingNames.add(name));
      mcpToolsByServer.set(serverNameRef, existingNames);
    } else if (isAgentTool(tool)) {
      nonMcpTools.push(tool);
    } else {
      const toolType = tool?.type || (tool ? 'malformed' : 'null/undefined');
      errors.push(`Invalid tool of type '${toolType}' was skipped`);
    }
  });

  // Convert to Tool objects- preserve original kind, apiGroup, and namespace from the first tool of each server
  const groupedMcpTools = Array.from(mcpToolsByServer.entries()).map(([serverNameRef, toolNamesSet]) => {
    // Find the first tool from this server to get its kind, apiGroup, and namespace
    const originalTool = tools.find(tool => 
      isMcpTool(tool) && tool.mcpServer?.name === serverNameRef
    );
    
    const originalMcpServer = originalTool?.mcpServer;
    
    return {
      type: MCP_SERVER_TYPE,
      mcpServer: {
        name: serverNameRef,
        namespace: originalMcpServer?.namespace,
        apiGroup: originalMcpServer?.apiGroup || DEFAULT_API_GROUP,
        kind: originalMcpServer?.kind || DEFAULT_MCP_KIND,
        toolNames: Array.from(toolNamesSet)
      }
    };
  });

  return {
    groupedTools: [...groupedMcpTools, ...nonMcpTools],
    errors
  };
};

export const getToolIdentifier = (tool: Tool): string => {
  if (isAgentTool(tool) && tool.agent) {
    return `agent-${tool.agent.name}`;
  } else if (isMcpTool(tool)) {
    const mcpTool = tool as Tool;
    return `mcp-${mcpTool.mcpServer?.name || "No name"}`;
  }
  return `unknown-tool-${Math.random().toString(36).substring(7)}`;
};

/**
 * Parse groupKind string to extract apiGroup and kind
 * @param groupKind - String in format "kind.apiGroup" or just "kind"
 * @returns Object with apiGroup and kind
 */
export const parseGroupKind = (groupKind: string): { apiGroup: string; kind: string } => {
  // Handle null, undefined, or empty string
  if (!groupKind || !groupKind.trim()) {
    return { apiGroup: DEFAULT_API_GROUP, kind: DEFAULT_MCP_KIND };
  }
  
  const parts = groupKind.trim().split('.');
  
  if (parts.length === 1) {
    const kind = parts[0];
    return { apiGroup: DEFAULT_API_GROUP, kind };
  }
  const kind = parts[0];
  const apiGroup = parts.slice(1).join('.');
  return { apiGroup, kind };
};

export const getToolDisplayName = (tool: Tool, defaultNamespace: string): string => {
  if (isAgentTool(tool) && tool.agent) {
    const name = tool.agent?.name;
    if (!name || name.trim() === "") {
      return "Unknown Agent";
    }
    return k8sRefUtils.toRef(tool.agent.namespace || defaultNamespace, name);
  } else if (isMcpTool(tool)) {
    const mcpTool = tool as Tool;
    const name = mcpTool.mcpServer?.name;
    if (!name || name.trim() === "") {
      return "Unknown Tool";
    }
    return k8sRefUtils.toRef(mcpTool.mcpServer?.namespace || defaultNamespace, name);
  }
  return "Unknown Tool";
};

export const getToolDescription = (tool: Tool, availableTools: ToolsResponse[]): string => {
  if (isAgentTool(tool) && tool.agent) {
    return "Agent";
  } else if (isMcpTool(tool)) {
    // For MCP tools, look up description from availableTools
    const mcpTool = tool as Tool;
    const foundServer = availableTools.find(t => t.server_name === mcpTool.mcpServer?.name);
    if (foundServer) {
      return foundServer.description;
    }
    return "MCP tool description not available";
  }
  return "No description available";
};

// Utility functions for DiscoveredTool type
export const getToolResponseDisplayName = (tool: ToolsResponse | undefined | null): string => {
  if (!tool || typeof tool !== "object") return "Unknown Tool";
  return (tool as ToolsResponse).id || "Unknown Tool";
};

export const getToolResponseDescription = (tool: ToolsResponse | undefined | null): string => {
  if (!tool || typeof tool !== "object") return "No description available";
  return (tool as ToolsResponse).description || "No description available";
};

export const getToolResponseCategory = (tool: ToolsResponse | undefined | null): string => {
  // Extract category from server reference or tool name
  if (!tool || typeof tool !== "object") return "Unknown";
  if ((tool as ToolsResponse).server_name === `kagent/${TOOL_SERVER_NAME}`) {
    const parts = (tool as ToolsResponse).id.split("_");
    if (parts.length > 1) {
      return parts[0];
    } else {
      return (tool as ToolsResponse).id;
    } 
  }
  return (tool as ToolsResponse).server_name;
};

export const getToolResponseIdentifier = (tool: ToolsResponse | undefined | null): string => {
  if (!tool || typeof tool !== "object") return "unknown-unknown";
  return `${(tool as ToolsResponse).server_name}-${(tool as ToolsResponse).id}`;
};

// Convert DiscoveredTool to Tool for agent creation
export const toolResponseToAgentTool = (tool: ToolsResponse, serverRef: string): Tool => {
  let { apiGroup, kind } = parseGroupKind(tool.group_kind);
  
  // Parse namespace and name from serverRef if it's in namespace/name format
  let name = serverRef;
  let namespace: string | undefined;
  
  if (k8sRefUtils.isValidRef(serverRef)) {
    const parsed = k8sRefUtils.fromRef(serverRef);
    name = parsed.name;
    namespace = parsed.namespace;
  }
  
  // Special handling for kagent-querydoc - must have empty apiGroup for Kubernetes Service
  if (serverRef.toLocaleLowerCase().includes(QUERYDOC_SERVER_NAME)) {
    apiGroup = "";
    kind = SERVICE_KIND;
  }
  
  return {
    type: MCP_SERVER_TYPE,
    mcpServer: {
      name,
      namespace,
      apiGroup,
      kind,
      toolNames: [tool.id]
    }
  };
};

// Utility functions for DiscoveredTool type (used in tools page)
export const getDiscoveredToolDisplayName = (tool: DiscoveredTool): string => {
  return tool.name || "Unknown Tool";
};

export const getDiscoveredToolDescription = (tool: DiscoveredTool): string => {
  return tool.description || "No description available";
};

export const getDiscoveredToolCategory = (tool: DiscoveredTool, serverRef: string): string => {
  // Extract category from server reference or tool name
  if (serverRef.includes(TOOL_SERVER_NAME)) {
    const parts = tool.name.split("_");
    if (parts.length > 1) {
      return parts[0];
    } else {
      return tool.name;
    } 
  }
  return serverRef;
};

export const getDiscoveredToolIdentifier = (tool: DiscoveredTool, serverRef: string): string => {
  return `${serverRef}-${tool.name}`;
};
