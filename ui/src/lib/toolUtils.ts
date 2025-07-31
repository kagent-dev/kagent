import { Tool, McpServerTool, AgentTool, ToolResponse } from "@/types/datamodel";

export const isAgentTool = (tool: unknown): tool is { type: "Agent"; agent: AgentTool } => {
  if (!tool || typeof tool !== "object") return false;

  const possibleTool = tool as Partial<Tool>;
  return possibleTool.type === "Agent" && !!possibleTool.agent && typeof possibleTool.agent === "object" && typeof possibleTool.agent.ref === "string";
};

export const isMcpTool = (tool: unknown): tool is { type: "McpServer"; mcpServer: McpServerTool } => {
  if (!tool || typeof tool !== "object") return false;

  const possibleTool = tool as Partial<Tool>;

  return (
    possibleTool.type === "McpServer" &&
    !!possibleTool.mcpServer &&
    typeof possibleTool.mcpServer === "object" &&
    typeof possibleTool.mcpServer.toolServer === "string" &&
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
      const serverNameRef = tool.mcpServer.toolServer;
      const toolNames = tool.mcpServer.toolNames;

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
      toolServer: serverNameRef,
      toolNames: Array.from(toolNamesSet)
    }
  }));

  return {
    groupedTools: [...groupedMcpTools, ...nonMcpTools],
    errors
  };
};

// Utility functions for ToolResponse type
export const getToolResponseDisplayName = (tool: ToolResponse): string => {
  return tool.id || "Unknown Tool";
};

export const getToolResponseDescription = (tool: ToolResponse): string => {
  return tool.description || "No description available";
};

export const getToolResponseCategory = (tool: ToolResponse): string => {

  if (tool.server_name === 'kagent/kagent-tool-server') {
    const parts = tool.id.split("_");
    if (parts.length > 1) {
      return parts[0];
    } else {
      return tool.id;
    } 
  }
  return tool.server_name;
};

export const getToolResponseIdentifier = (tool: ToolResponse): string => {
  return `${tool.server_name}-${tool.id}`;
};

// Convert ToolResponse to Tool for agent creation
export const toolResponseToAgentTool = (toolResponse: ToolResponse): Tool => {
  console.log("toolResponseToAgentTool", toolResponse);
  // Check if this is an MCP tool server
    if (isMcpTool(toolResponse)) {
    return {
      type: "McpServer",
      mcpServer: {
        toolServer: toolResponse.server_name,
        toolNames: [toolResponse.id]
      }
    };
  }
  
  // For non-MCP tools (like kagent built-in tools), create a generic MCP structure
  // This allows built-in tools to work through the MCP interface
  return {
    type: "McpServer",
    mcpServer: {
      toolServer: toolResponse.server_name,
      toolNames: [toolResponse.id]
    }
  };
};
