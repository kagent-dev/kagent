import { Tool, Component, MCPToolConfig, ToolConfig, McpServerTool, BuiltinTool, AgentTool } from "@/types/datamodel";

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

export const isBuiltinTool = (tool: unknown): tool is { type: "Builtin"; builtin: BuiltinTool } => {
  if (!tool || typeof tool !== "object") return false;

  const possibleTool = tool as Partial<Tool>;

  return possibleTool.type === "Builtin" && !!possibleTool.builtin && typeof possibleTool.builtin === "object" && typeof possibleTool.builtin.name === "string";
};

export const getToolDisplayName = (tool?: Tool | Component<ToolConfig>): string => {
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
  } else if (isBuiltinTool(tool) && tool.builtin) {
    // For Builtin tools, use the label if available, otherwise fall back to provider and make sure to use the last part of the provider
    const providerParts = tool.builtin.name.split(".");
    const providerName = providerParts[providerParts.length - 1];
    return tool.builtin.label || providerName || "Builtin Tool";
  } else if (isAgentTool(tool) && tool.agent) {
    return tool.agent.ref;
  } else {
    console.warn("Unknown tool type:", tool);
    return "Unknown Tool";
  }
};

export const getToolDescription = (tool?: Tool | Component<ToolConfig>): string => {
  if (!tool) return "No description";

  if (typeof tool === "object" && "provider" in tool) {
    const component = tool as Component<ToolConfig>; 
    if (isMcpProvider(component.provider)) {
      const desc = (component.config as MCPToolConfig)?.tool?.description;
      return typeof desc === 'string' && desc ? desc : "No description";
    } else {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const configDesc = (component.config as any)?.description;
      if (typeof configDesc === 'string' && configDesc) {
        return configDesc;
      }
      // Fallback if config.description is missing
      if (typeof component.description === 'string' && component.description) {
          // Use top-level description as fallback for Components
          return component.description;
      }
      return "No description";
    }
  }

  if (isBuiltinTool(tool) && tool.builtin) {
    return tool.builtin.description || "No description"; 
  } else if (isMcpTool(tool)) {
    return "MCP Server Tool";
  } else if (isAgentTool(tool) && tool.agent) {
    return tool.agent.description || "Agent Tool (No description provided)";
  } else {
    console.warn("Unknown tool type:", tool);
    return "No description";
  }
};

export const getToolIdentifier = (tool?: Tool | Component<ToolConfig>): string => {
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

    // For regular component tools (includes Builtin)
    return `component-${tool.provider}`;
  }

  // Handle AgentTool types
  if (isMcpTool(tool) && tool.mcpServer) {
    // For MCP agent tools, use toolServer and first tool name
    const toolName = tool.mcpServer.toolNames[0] || "unknown";
    // Ensure mcpServer and toolServer exist before accessing
    const toolServer = tool.mcpServer?.toolServer || "unknown";
    return `mcptool-${toolServer}-${toolName}`;
  } else if (isBuiltinTool(tool) && tool.builtin) {
    // For Builtin agent tools
    return `component-${tool.builtin.name}`;
  } else if (isAgentTool(tool) && tool.agent) {
    return `agent-${tool.agent.ref}`;
  } else {
    console.warn("Unknown tool type:", tool);
    return `unknown-${JSON.stringify(tool).slice(0, 20)}`;
  }
};

export const getToolProvider = (tool?: Tool | Component<ToolConfig>): string => {
  if (!tool) return "unknown";

  // Check if the tool is of Component<ToolConfig> type
  if (typeof tool === "object" && "provider" in tool) {
    return tool.provider;
  }
  
  // Handle AgentTool types
  if (isBuiltinTool(tool) && tool.builtin) {
    return tool.builtin.name;
  } else if (isMcpTool(tool) && tool.mcpServer) {
    return tool.mcpServer.toolServer;
  } else if (isAgentTool(tool) && tool.agent) {
    return tool.agent.ref;
  } else {
    console.warn("Unknown tool type:", tool);
    return "unknown";
  }
};

export const isSameTool = (toolA?: Tool, toolB?: Tool): boolean => {
  if (!toolA || !toolB) return false;
  return getToolIdentifier(toolA) === getToolIdentifier(toolB);
};

export const componentToAgentTool = (component: Component<ToolConfig>): Tool => {
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
    // Built-in component
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const configDesc = (component.config as any)?.description;
    const descriptionToStore = (typeof configDesc === 'string' && configDesc)
        ? configDesc 
        : (typeof component.description === 'string' && component.description ? component.description : undefined);

    return {
      type: "Builtin",
      builtin: {
        name: component.provider,
        label: component.label || undefined,
        description: descriptionToStore,
        config: component.config || undefined
      }
    };
  }
};

export const findComponentForAgentTool = (
  agentTool: Tool,
  components: Component<ToolConfig>[]
): Component<ToolConfig> | undefined => {
  const agentToolId = getToolIdentifier(agentTool);
  if (agentToolId === "unknown") {
    console.warn("Could not get identifier for agent tool:", agentTool);
    return undefined;
  }

  return components.find((c) => getToolIdentifier(c) === agentToolId);
};

export const SSE_MCP_TOOL_PROVIDER_NAME = "autogen_ext.tools.mcp.SseMcpToolAdapter";
export const STDIO_MCP_TOOL_PROVIDER_NAME = "autogen_ext.tools.mcp.StdioMcpToolAdapter";
export const STREAMABLE_HTTP_MCP_TOOL_PROVIDER_NAME = "autogen_ext.tools.mcp.StreamableHttpMcpToolAdapter";

export function isMcpProvider(provider: string): boolean {
  return provider === SSE_MCP_TOOL_PROVIDER_NAME || provider === STDIO_MCP_TOOL_PROVIDER_NAME || provider === STREAMABLE_HTTP_MCP_TOOL_PROVIDER_NAME;
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