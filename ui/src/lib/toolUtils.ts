import type{ Tool, McpServerTool, ToolsResponse, DiscoveredTool, TypedLocalReference, AgentResponse } from "@/types";

export const isAgentTool = (value: unknown): value is { type: "Agent"; agent: TypedLocalReference } => {
  if (!value || typeof value !== "object") return false;
  const obj = value as any;
  return obj.type === "Agent" && obj.agent && typeof obj.agent === "object" && typeof obj.agent.name === "string";
};

export const isAgentResponse = (value: unknown): value is AgentResponse => {
  if (!value || typeof value !== "object") return false;
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

  // Convert to Tool objects
  const groupedMcpTools = Array.from(mcpToolsByServer.entries()).map(([serverNameRef, toolNamesSet]) => ({
    type: "McpServer" as const,
    mcpServer: {
      name: serverNameRef,
      apiGroup: "kagent.dev",
      kind: "MCPServer",
      toolNames: Array.from(toolNamesSet)
    }
  }));

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

export const getToolDisplayName = (tool: Tool): string => {
  if (isAgentTool(tool) && tool.agent) {
    try {
      return tool.agent.name;
    } catch {
      return "Agent";
    }
  } else if (isMcpTool(tool)) {
    const mcpTool = tool as Tool;
    return mcpTool.mcpServer?.name || "No name";
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
  if ((tool as ToolsResponse).server_name === 'kagent/kagent-tool-server') {
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
  return {
    type: "McpServer",
    mcpServer: {
      name: serverRef,
      apiGroup: "kagent.dev",
      kind: "MCPServer",
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
  if (serverRef.includes('kagent-tool-server')) {
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
