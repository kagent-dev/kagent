import { Tool, Component, MCPToolConfig, ToolConfig, McpServerTool, AgentTool } from "@/types/datamodel";

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

  if (isMcpTool(tool)) {
    return "MCP Server Tool";
  } else if (isAgentTool(tool) && tool.agent) {
    return tool.agent.description || "Agent Tool (No description provided)";
  } else {
    console.warn("Unknown tool type:", tool);
    return "No description";
  }
};

export const isBuiltInTool = (tool?: Tool | Component<ToolConfig>): boolean => {
  if (!tool) return false;

  // Check if the toolServer name ends with 'kagent-tool-server'
  // This is a bit fragile, since we're relying on the name of the tool server.
  if (typeof tool === "object" && "provider" in tool) {
    const component = tool as Component<ToolConfig>;
    if (isMcpProvider(component.provider)) {
      const toolServer = component.label || (component.config as MCPToolConfig)?.tool?.name || "unknown";
      return toolServer.endsWith("kagent-tool-server");
    }
  }
  return false;
};

export const getToolIdentifier = (tool?: Tool | Component<ToolConfig>): string => {
  if (!tool) return "unknown";

  // Handle Component<ToolConfig> type
  if (typeof tool === "object" && "provider" in tool && isMcpProvider(tool.provider)) {
    // For MCP adapter components, use toolServer (from label) and tool name
    const mcpConfig = tool.config as MCPToolConfig;
    const toolServer = tool.label || mcpConfig.tool.name || "unknown"; // Prefer label as toolServer
    const toolName = mcpConfig.tool.name || "unknown";
    return `${toolServer}-${toolName}`;
  }

  // Handle AgentTool types
  if (isMcpTool(tool) && tool.mcpServer) {
    // For MCP agent tools, use toolServer and first tool name
    const toolName = tool.mcpServer.toolNames[0] || "unknown";
    // Ensure mcpServer and toolServer exist before accessing
    const toolServer = tool.mcpServer?.toolServer || "unknown";
    return `${toolServer}-${toolName}`;
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
  if (isMcpTool(tool) && tool.mcpServer) {
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
  }

  throw new Error(`Unknown tool type: ${component.provider}`);
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
  if (isBuiltInTool(tool)) {
    // Get the tool name, and get the first portion of the tool name (split at '_'). The tools are named like this: 'k8s_some_tool_name' and "istio_another_toolname".
    // We want the first portion of the tool name.
    const toolName = (tool.config as MCPToolConfig)?.tool?.name;
    const parts = toolName.split("_");
    if (parts.length > 0) {
      return parts[0];
    }
    return "other";
  }
  return tool.label || "MCP Server";
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
