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
    if (isMcpProvider(tool.provider)) {
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
    if (isMcpProvider(tool.provider)) {
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
    if (isMcpProvider(tool.provider)) {
      // For MCP adapter components, use toolServer (from label) and tool name
      const mcpConfig = tool.config as MCPToolConfig;
      const toolServer = tool.label || mcpConfig.tool.name || "unknown"; // Prefer label as toolServer
      const toolName = mcpConfig.tool.name || "unknown";
      return `mcptool-${toolServer}-${toolName}`;
    }

    // For regular component tools (includes Inline)
    return `component-${tool.provider}`;
  }

  // Handle AgentTool types
  if (isMcpTool(tool) && tool.mcpServer) {
    // For MCP agent tools, use toolServer and first tool name
    const toolName = tool.mcpServer.toolNames[0] || "unknown";
    // Ensure mcpServer and toolServer exist before accessing
    const toolServer = tool.mcpServer?.toolServer || "unknown";
    return `mcptool-${toolServer}-${toolName}`;
  } else if (isInlineTool(tool) && tool.inline) {
    // For Inline agent tools
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
  if (isMcpProvider(component.provider)) {
    const mcpConfig = component.config as MCPToolConfig;
    return {
      type: "McpServer",
      mcpServer: {
        toolServer: component.label || mcpConfig.tool.name || "unknown",
        toolNames: [mcpConfig.tool.name || "unknown"]
      }
    };
  } else {
    return {
      type: "Inline",
      inline: {
        provider: component.provider,
        label: component.label || undefined,
        description: component.description || undefined,
        config: component.config || undefined
      }
    };
  }
};

export const findComponentForAgentTool = (
  agentTool: AgentTool,
  components: Component<ToolConfig>[]
): Component<ToolConfig> | undefined => {
  const agentToolId = getToolIdentifier(agentTool);
  if (agentToolId === "unknown") {
    console.warn("Could not get identifier for agent tool:", agentTool);
    return undefined;
  }

  return components.find((c) => getToolIdentifier(c) === agentToolId);
};

export function isMcpProvider(provider: string): boolean {
  return provider === "autogen_ext.tools.mcp.SseMcpToolAdapter" || provider === "autogen_ext.tools.mcp.StdioMcpToolAdapter";
}

// Extract category from tool identifier
export const getToolCategory = (tool: Component<ToolConfig>) => {
  if (isMcpProvider(tool.provider)) {
    return tool.label || "MCP Server";
  }

  const toolId = getToolIdentifier(tool);
  const parts = toolId.split(".");
  if (parts.length >= 3 && parts[1] === "tools") {
    return parts[2]; // e.g., kagent.tools.grafana -> grafana
  }
  if (parts.length >= 2) {
    return parts[1]; // e.g., kagent.builtin -> builtin
  }
  return "other"; // Default category
};