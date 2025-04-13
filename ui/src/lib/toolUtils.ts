import { AgentTool, Component, MCPToolConfig, ToolConfig, McpServerTool, InlineTool } from "@/types/datamodel";

export const isMcpTool = (tool: unknown): tool is { type: "McpServer"; mcpServer: McpServerTool } => {
  if (!tool || typeof tool !== "object") return false;

  const possibleTool = tool as Partial<AgentTool>;

  return (
    possibleTool.type === "McpServer" &&
    !!possibleTool.mcpServer &&
    typeof possibleTool.mcpServer === "object" &&
    typeof possibleTool.mcpServer.toolServer === "string" &&
    Array.isArray(possibleTool.mcpServer.toolNames)
  );
};

export const isInlineTool = (tool: unknown): tool is { type: "Inline"; inline: InlineTool } => {
  if (!tool || typeof tool !== "object") return false;

  const possibleTool = tool as Partial<AgentTool>;

  return possibleTool.type === "Inline" && !!possibleTool.inline && typeof possibleTool.inline === "object" && typeof possibleTool.inline.provider === "string";
};

export const getToolDisplayName = (tool?: AgentTool | Component<ToolConfig>): string => {
  if (!tool) return "No name";

  // Check if the tool is of Component<ToolConfig> type
  if (typeof tool === "object" && "provider" in tool && "label" in tool) {
    if (tool.provider === "autogen_ext.tools.mcp.SseMcpToolAdapter") {
      // Use the config.tool.name for the display name
      return (tool.config as MCPToolConfig).tool.name || "No name";
    }
    return tool.label || "No name";
  }

  // Handle AgentTool types
  if (isMcpTool(tool) && tool.mcpServer) {
    // For McpServer tools, use the first tool name if available
    return tool.mcpServer.toolNames.length > 0 ? tool.mcpServer.toolNames[0] : tool.mcpServer.toolServer;
  } else if (isInlineTool(tool) && tool.inline) {
    // For Inline tools, use the label if available, otherwise fall back to provider and make sure to use the last part of the provider
    const providerParts = tool.inline.provider.split(".");
    const providerName = providerParts[providerParts.length - 1];
    return tool.inline.label || providerName || "Inline Tool";
  } else {
    console.warn("Unknown tool type:", tool);
    return "Unknown Tool";
  }
};

export const getToolDescription = (tool?: AgentTool | Component<ToolConfig>): string => {
  if (!tool) return "No description";

  // Check if the tool is of Component<ToolConfig> type
  if (typeof tool === "object" && "provider" in tool && "description" in tool) {
    if (tool.provider === "autogen_ext.tools.mcp.SseMcpToolAdapter") {
      return (tool.config as MCPToolConfig).tool.description || "No description";
    }
    return tool.description || "No description";
  }

  // Handle AgentTool types
  if (isInlineTool(tool) && tool.inline) {
    return tool.inline.description || "No description";
  } else if (isMcpTool(tool)) {
    return "MCP Server Tool";
  } else {
    console.warn("Unknown tool type:", tool);
    return "No description";
  }
};

export const getToolIdentifier = (tool?: AgentTool | Component<ToolConfig>): string => {
  if (!tool) return "unknown";

  // Handle Component<ToolConfig> type
  if (typeof tool === "object" && "provider" in tool) {
    if (tool.provider === "autogen_ext.tools.mcp.SseMcpToolAdapter") {
      // For MCP adapter tools
      const mcpConfig = tool.config as MCPToolConfig;
      return `mcptool-${mcpConfig.tool.name}`;
    }
    
    // For regular component tools
    return `component-${tool.provider}`;
  }

  // Handle AgentTool types
  if (isMcpTool(tool) && tool.mcpServer) {
    const toolName = tool.mcpServer.toolNames[0] || "unknown";
    return `mcptool-${tool.mcpServer.toolServer}-${toolName}`;
  } else if (isInlineTool(tool) && tool.inline) {
    return `component-${tool.inline.provider}`;
  } else {
    console.warn("Unknown tool type:", tool);
    return `unknown-${JSON.stringify(tool).slice(0, 20)}`;
  }
};

export const getToolProvider = (tool?: AgentTool | Component<ToolConfig>): string => {
  if (!tool) return "unknown";

  // Check if the tool is of Component<ToolConfig> type
  if (typeof tool === "object" && "provider" in tool) {
    return tool.provider;
  }
  
  // Handle AgentTool types
  if (isInlineTool(tool) && tool.inline) {
    return tool.inline.provider;
  } else if (isMcpTool(tool) && tool.mcpServer) {
    return tool.mcpServer.toolServer;
  } else {
    console.warn("Unknown tool type:", tool);
    return "unknown";
  }
};

export const isSameTool = (toolA?: AgentTool, toolB?: AgentTool): boolean => {
  if (!toolA || !toolB) return false;
  return getToolIdentifier(toolA) === getToolIdentifier(toolB);
};

export const componentToAgentTool = (component: Component<ToolConfig>): AgentTool => {
  if (component.provider === "autogen_ext.tools.mcp.SseMcpToolAdapter") {
    const mcpConfig = component.config as MCPToolConfig;
    return {
      type: "McpServer",
      mcpServer: {
        toolServer: mcpConfig.tool.name || "unknown",
        toolNames: [mcpConfig.tool.name || "unknown"]
      }
    };
  } else {
    return {
      type: "Inline",
      inline: {
        provider: component.provider,
        label: component.label || undefined,
        description: component.description || undefined
      }
    };
  }
};


export const findComponentForAgentTool = (agentTool: AgentTool, components: Component<ToolConfig>[]): Component<ToolConfig> | undefined => {
  if (isMcpTool(agentTool)) {
    // isMcpTool type guard ensures mcpServer exists and has required properties
    return components.find(
      (c) => 
        c.provider === "autogen_ext.tools.mcp.SseMcpToolAdapter" && 
        (c.config as MCPToolConfig).tool.name === agentTool.mcpServer.toolNames[0]
    );
  } else if (isInlineTool(agentTool)) {
    // isInlineTool type guard ensures inline exists and has required properties
    return components.find((c) => c.provider === agentTool.inline.provider);
  }
  return undefined;
};