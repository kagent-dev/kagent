import { 
  isMcpTool, 
  isBuiltinTool,
  isAgentTool,
  getToolDisplayName, 
  getToolDescription, 
  getToolIdentifier, 
  getToolProvider, 
  isSameTool,
  componentToAgentTool,
  findComponentForAgentTool,
  isMcpProvider,
  getToolCategory,
  SSE_MCP_TOOL_PROVIDER_NAME,
  STDIO_MCP_TOOL_PROVIDER_NAME
} from '../toolUtils';
import { Tool, Component, MCPToolConfig, ToolConfig, AgentTool, BuiltinTool } from "@/types/datamodel";

describe('Tool Utility Functions', () => {
  let consoleWarnSpy: jest.SpyInstance;

  beforeEach(() => {
    // Suppress console.warn before each test
    consoleWarnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    // Restore console.warn after each test
    consoleWarnSpy.mockRestore();
  });

  describe('isMcpTool', () => {
    it('should identify valid MCP tools', () => {
      const validMcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(isMcpTool(validMcpTool)).toBe(true);
    });

    it('should reject invalid MCP tools', () => {
      expect(isMcpTool(null)).toBe(false);
      expect(isMcpTool(undefined)).toBe(false);
      expect(isMcpTool({})).toBe(false);
      expect(isMcpTool({ type: "McpServer" })).toBe(false);
      expect(isMcpTool({ type: "McpServer", mcpServer: {} })).toBe(false);
      expect(isMcpTool({ type: "McpServer", mcpServer: { toolServer: "test" } })).toBe(false);
      expect(isMcpTool({ type: "McpServer", mcpServer: { toolNames: [] } })).toBe(false);
      expect(isMcpTool({ type: "Inline" })).toBe(false);
    });
  });

  describe('isBuiltinTool', () => {
    it('should identify valid inline tools', () => {
      const validInlineTool: Tool = {
        type: "Builtin",
        builtin: {
          name: "test-provider",
          label: "Test Tool",
          description: "Test Description"
        }
      };
      expect(isBuiltinTool(validInlineTool)).toBe(true);
    });

    it('should reject invalid inline tools', () => {
      expect(isBuiltinTool(null)).toBe(false);
      expect(isBuiltinTool(undefined)).toBe(false);
      expect(isBuiltinTool({})).toBe(false);
      expect(isBuiltinTool({ type: "Builtin" })).toBe(false);
      expect(isBuiltinTool({ type: "Builtin", builtin: {} })).toBe(false);
      expect(isBuiltinTool({ type: "Builtin", builtin: { name: "test"} })).toBe(true);
      expect(isBuiltinTool({ type: "McpServer" })).toBe(false);
    });
  });

  describe('getToolDisplayName', () => {
    it('should return "No name" for undefined tools', () => {
      expect(getToolDisplayName(undefined)).toBe("No name");
    });

    it('should handle MCP adapter tools', () => {
      const mcpAdapterTool: Component<ToolConfig> = {
        provider: "autogen_ext.tools.mcp.SseMcpToolAdapter",
        label: "Adapter Label",
        description: "Adapter Description",
        component_type: "tool",
        config: {
          tool: {
            name: "MCP Tool Name",
            description: "MCP Tool Description"
          }
        } as MCPToolConfig
      };
      expect(getToolDisplayName(mcpAdapterTool)).toBe("MCP Tool Name");
    });

    it('should handle regular component tools', () => {
      const componentTool: Component<ToolConfig> = {
        provider: "test.provider",
        label: "Component Label",
        description: "Component Description",
        component_type: "tool",
        config: {}
      };
      expect(getToolDisplayName(componentTool)).toBe("Component Label");
    });

    it('should handle MCP server tools', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(getToolDisplayName(mcpTool)).toBe("tool1");
    });

    it('should handle builtin tools', () => {
      const inlineTool: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider.ToolName",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(getToolDisplayName(inlineTool)).toBe("Inline Label");
    });

    it('should fall back to provider name for builtin tools without label', () => {
      const inlineTool: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider.ToolName",
          description: "Inline Description"
        }
      };
      expect(getToolDisplayName(inlineTool)).toBe("ToolName");
    });

    it('should handle unknown tool types', () => {
      const unknownTool = { someProperty: "value" };
      expect(getToolDisplayName(unknownTool as any)).toBe("Unknown Tool");
      expect(consoleWarnSpy).toHaveBeenCalledWith("Unknown tool type:", unknownTool);
    });
  });

  describe('getToolDescription', () => {
    it('should return "No description" for undefined tools', () => {
      expect(getToolDescription(undefined)).toBe("No description");
    });

    it('should handle MCP adapter tools', () => {
      const mcpAdapterTool: Component<ToolConfig> = {
        provider: "autogen_ext.tools.mcp.SseMcpToolAdapter",
        label: "Adapter Label",
        description: "Adapter Description",
        component_type: "tool",
        config: {
          tool: {
            name: "MCP Tool Name",
            description: "MCP Tool Description"
          }
        } as MCPToolConfig
      };
      expect(getToolDescription(mcpAdapterTool)).toBe("MCP Tool Description");
    });

    it('should handle MCP stdio adapter tools', () => {
      const mcpAdapterTool: Component<ToolConfig> = {
        provider: "autogen_ext.tools.mcp.StdioMcpToolAdapter",
        label: "Adapter Label",
        description: "Adapter Description",
        component_type: "tool",
        config: {
          tool: {
            name: "MCP Tool Name",
            description: "MCP Tool Description"
          }
        } as MCPToolConfig
      };
      expect(getToolDescription(mcpAdapterTool)).toBe("MCP Tool Description");
    });

    it('should handle regular component tools', () => {
      const componentTool: Component<ToolConfig> = {
        provider: "test.provider",
        label: "Component Label",
        description: "Component Description",
        component_type: "tool",
        config: {}
      };
      expect(getToolDescription(componentTool)).toBe("Component Description");
    });

    it('should handle builtin tools', () => {
      const inlineTool: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(getToolDescription(inlineTool)).toBe("Inline Description");
    });

    it('should handle MCP server tools', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(getToolDescription(mcpTool)).toBe("MCP Server Tool");
    });

    it('should handle unknown tool types', () => {
      const unknownTool = { someProperty: "value" };
      expect(getToolDescription(unknownTool as any)).toBe("No description");
    });
  });

  describe('getToolIdentifier', () => {
    it('should return "unknown" for undefined tools', () => {
      expect(getToolIdentifier(undefined)).toBe("unknown");
    });

    it('should handle MCP adapter tools', () => {
      const mcpAdapterTool: Component<ToolConfig> = {
        provider: "autogen_ext.tools.mcp.SseMcpToolAdapter",
        label: "Adapter Label",
        description: "Adapter Description",
        component_type: "tool",
        config: {
          tool: {
            name: "MCP Tool Name",
            description: "MCP Tool Description"
          }
        } as MCPToolConfig
      };
      expect(getToolIdentifier(mcpAdapterTool)).toBe("mcptool-Adapter Label-MCP Tool Name");
    });

    it('should handle MCP stdio adapter tools', () => {
      const mcpAdapterTool: Component<ToolConfig> = {
        provider: "autogen_ext.tools.mcp.StdioMcpToolAdapter",
        label: "Adapter Label",
        description: "Adapter Description",
        component_type: "tool",
        config: {
          tool: {
            name: "MCP Tool Name",
            description: "MCP Tool Description"
          }
        } as MCPToolConfig
      };
      expect(getToolIdentifier(mcpAdapterTool)).toBe("mcptool-Adapter Label-MCP Tool Name");
    });

    it('should handle regular component tools', () => {
      const componentTool: Component<ToolConfig> = {
        provider: "test.provider",
        label: "Component Label",
        description: "Component Description",
        component_type: "tool",
        config: {}
      };
      expect(getToolIdentifier(componentTool)).toBe("component-test.provider");
    });

    it('should handle MCP server tools', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(getToolIdentifier(mcpTool)).toBe("mcptool-test-server-tool1");
    });

    it('should handle builtin tools', () => {
      const inlineTool: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(getToolIdentifier(inlineTool)).toBe("component-test.provider");
    });

    it('should handle unknown tool types', () => {
      const unknownTool = { someProperty: "value" };
      const result = getToolIdentifier(unknownTool as any);
      expect(result).toMatch(/^unknown-/);
      expect(consoleWarnSpy).toHaveBeenCalledWith("Unknown tool type:", unknownTool);
    });
  });

  describe('getToolProvider', () => {
    it('should return "unknown" for undefined tools', () => {
      expect(getToolProvider(undefined)).toBe("unknown");
    });

    it('should handle component tools', () => {
      const componentTool: Component<ToolConfig> = {
        provider: "test.provider",
        label: "Component Label",
        description: "Component Description",
        component_type: "tool",
        config: {}
      };
      expect(getToolProvider(componentTool)).toBe("test.provider");
    });

    it('should handle MCP server tools', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(getToolProvider(mcpTool)).toBe("test-server");
    });

    it('should handle builtin tools', () => {
      const inlineTool: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(getToolProvider(inlineTool)).toBe("test.provider");
    });

    it('should handle unknown tool types', () => {
      const unknownTool = { someProperty: "value" };
      expect(getToolProvider(unknownTool as any)).toBe("unknown");
      expect(consoleWarnSpy).toHaveBeenCalledWith("Unknown tool type:", unknownTool);
    });
  });

  describe('isSameTool', () => {
    it('should return false for undefined tools', () => {
      expect(isSameTool(undefined, undefined)).toBe(false);
      expect(isSameTool(undefined, {} as Tool)).toBe(false);
      expect(isSameTool({} as Tool, undefined)).toBe(false);
    });

    it('should identify same MCP tools', () => {
      const tool1: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      const tool2: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool3"]
        }
      };
      expect(isSameTool(tool1, tool2)).toBe(true);
    });

    it('should identify same builtin tools', () => {
      const tool1: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      const tool2: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider",
          label: "Different Label",
          description: "Different Description"
        }
      };
      expect(isSameTool(tool1, tool2)).toBe(true);
    });

    it('should identify different tools', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      const inlineTool: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(isSameTool(mcpTool, inlineTool)).toBe(false);
    });

    it('should identify different MCP tools', () => {
      const tool1: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server-1",
          toolNames: ["tool1", "tool2"]
        }
      };
      const tool2: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server-2",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(isSameTool(tool1, tool2)).toBe(false);
    });

    it('should identify different builtin tools', () => {
      const tool1: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider1",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      const tool2: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider2",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(isSameTool(tool1, tool2)).toBe(false);
    });
  });

  describe('isAgentTool', () => {
    it('should identify valid Agent tools', () => {
      const validAgentTool: Tool = {
        type: "Agent",
        agent: {
          ref: "test-agent",
          description: "Agent description"
        }
      };
      expect(isAgentTool(validAgentTool)).toBe(true);
    });

    it('should reject invalid Agent tools', () => {
      expect(isAgentTool(null)).toBe(false);
      expect(isAgentTool(undefined)).toBe(false);
      expect(isAgentTool({})).toBe(false);
      expect(isAgentTool({ type: "Agent" })).toBe(false);
      expect(isAgentTool({ type: "Agent", agent: {} })).toBe(false);
      expect(isAgentTool({ type: "Agent", agent: { description: "desc" } })).toBe(false);
      expect(isAgentTool({ type: "Agent", agent: { ref: 123 } })).toBe(false); // ref must be string
      expect(isAgentTool({ type: "Builtin" })).toBe(false);
    });
  });

  describe('componentToAgentTool', () => {
    it('should convert a Builtin component to a Builtin Tool', () => {
      const component: Component<ToolConfig> = {
        provider: "test.provider",
        label: "Test Label",
        description: "Test Component Description",
        component_type: "tool",
        config: { setting: "value" }
      };
      const expectedTool: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider",
          label: "Test Label",
          description: "Test Component Description", // Prefers component.description
          config: { setting: "value" }
        }
      };
      expect(componentToAgentTool(component)).toEqual(expectedTool);
    });

    it('should convert a Builtin component using config description', () => {
      const component: Component<ToolConfig> = {
        provider: "test.provider.configdesc",
        label: "Test Label Config Desc",
        description: "Top Level Desc",
        component_type: "tool",
        config: { description: "Config Desc" }
      };
      const expectedTool: Tool = {
        type: "Builtin",
        builtin: {
          name: "test.provider.configdesc",
          label: "Test Label Config Desc",
          description: "Config Desc", // Prefers config.description
          config: { description: "Config Desc" }
        }
      };
      expect(componentToAgentTool(component)).toEqual(expectedTool);
    });

    it('should convert an MCP component to an McpServer Tool', () => {
      const component: Component<MCPToolConfig> = {
        provider: SSE_MCP_TOOL_PROVIDER_NAME,
        label: "MyMCPAdapter", // Used as toolServer
        description: "MCP Adapter Description",
        component_type: "tool",
        config: {
          server_params: { url: "http://example.com/sse" },
          tool: {
            name: "TheActualToolName",
            description: "Actual Tool Description",
            inputSchema: {}
          }
        }
      };
      const expectedTool: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "MyMCPAdapter", // From component label
          toolNames: ["TheActualToolName"] // From config.tool.name
        }
      };
      expect(componentToAgentTool(component)).toEqual(expectedTool);
    });

     it('should fallback to tool name for MCP component toolServer if label missing', () => {
      const component: Component<MCPToolConfig> = {
        provider: STDIO_MCP_TOOL_PROVIDER_NAME,
        description: "MCP Adapter Description",
        component_type: "tool",
        config: {
          server_params: { command: "echo stdio" },
          tool: {
            name: "ToolNameAsServer",
            description: "Actual Tool Description",
            inputSchema: {}
          }
        }
      };
      const expectedTool: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "ToolNameAsServer", // Falls back to tool name
          toolNames: ["ToolNameAsServer"]
        }
      };
      expect(componentToAgentTool(component)).toEqual(expectedTool);
    });
  });

  describe('findComponentForAgentTool', () => {
    const components: Component<ToolConfig>[] = [
      {
        provider: "builtin.provider",
        label: "Builtin Component",
        component_type: "tool",
        config: {}
      },
      {
        provider: SSE_MCP_TOOL_PROVIDER_NAME,
        label: "mcp.server.name", // Matches toolServer
        component_type: "tool",
        config: { server_params: { url: "http://example.com/sse2" }, tool: { name: "mcp_tool_name", description: "desc", inputSchema: {} } } as MCPToolConfig // Matches toolName
      },
      {
        provider: "other.provider",
        label: "Other Component",
        component_type: "tool",
        config: {}
      }
    ];

    it('should find a matching Builtin component for a Builtin tool', () => {
      const agentTool: Tool = {
        type: "Builtin",
        builtin: { name: "builtin.provider", label: "Irrelevant Label" } as BuiltinTool
      };
      const expectedComponent = components[0];
      expect(findComponentForAgentTool(agentTool, components)).toBe(expectedComponent);
    });

    it('should find a matching MCP component for an McpServer tool', () => {
      const agentTool: Tool = {
        type: "McpServer",
        mcpServer: { toolServer: "mcp.server.name", toolNames: ["mcp_tool_name"] }
      };
      const expectedComponent = components[1];
      expect(findComponentForAgentTool(agentTool, components)).toBe(expectedComponent);
    });

    it('should not find a match for an Agent tool (identifier mismatch)', () => {
      const agentTool: Tool = {
        type: "Agent",
        agent: { ref: "some-agent" } as AgentTool
      };
      expect(findComponentForAgentTool(agentTool, components)).toBeUndefined();
    });

    it('should return undefined if no matching component is found', () => {
      const agentTool: Tool = {
        type: "Builtin",
        builtin: { name: "nonexistent.provider" } as BuiltinTool
      };
      expect(findComponentForAgentTool(agentTool, components)).toBeUndefined();
    });

    it('should find a component matching a tool derived from it', () => {
      const component = components[0];
      const derivedTool = componentToAgentTool(component);
      expect(findComponentForAgentTool(derivedTool, components)).toBe(component);

      const mcpComponent = components[1] as Component<MCPToolConfig>; 
      const derivedMcpTool = componentToAgentTool(mcpComponent);
      expect(findComponentForAgentTool(derivedMcpTool, components)).toBe(mcpComponent);
    });
  });

  describe('isMcpProvider', () => {
    it('should return true for known MCP provider names', () => {
      expect(isMcpProvider(SSE_MCP_TOOL_PROVIDER_NAME)).toBe(true);
      expect(isMcpProvider(STDIO_MCP_TOOL_PROVIDER_NAME)).toBe(true);
    });

    it('should return false for other provider names', () => {
      expect(isMcpProvider("autogen_ext.tools.something_else")).toBe(false);
      expect(isMcpProvider("my.custom.provider")).toBe(false);
      expect(isMcpProvider("")).toBe(false);
    });
  });

  describe('getToolCategory', () => {
    it('should return the label for MCP providers', () => {
      const component: Component<MCPToolConfig> = {
        provider: SSE_MCP_TOOL_PROVIDER_NAME,
        label: "My Custom MCP Server",
        component_type: "tool",
        config: { server_params: { url: "http://example.com/sse3" }, tool: { name: "tool", description: "desc", inputSchema: {} } }
      };
      expect(getToolCategory(component)).toBe("My Custom MCP Server");
    });

    it('should return "MCP Server" if label is missing for MCP provider', () => {
      const component: Component<MCPToolConfig> = {
        provider: STDIO_MCP_TOOL_PROVIDER_NAME,
        component_type: "tool",
        config: { server_params: { command: "echo stdio2" }, tool: { name: "tool", description: "desc", inputSchema: {} } }
      };
      expect(getToolCategory(component)).toBe("MCP Server");
    });

    it('should extract category from provider string like kagent.tools.*', () => {
      const component: Component<ToolConfig> = {
        provider: "kagent.tools.grafana",
        component_type: "tool",
        config: {}
      };
      expect(getToolCategory(component)).toBe("grafana");
    });

    it('should extract category from provider string like kagent.*', () => {
      const component: Component<ToolConfig> = {
        provider: "kagent.builtin",
        component_type: "tool",
        config: {}
      };
      expect(getToolCategory(component)).toBe("builtin");
    });

    it('should return "other" for unrecognised provider formats', () => {
      const component: Component<ToolConfig> = {
        provider: "mycompany.secrettool",
        component_type: "tool",
        config: {}
      };
      expect(getToolCategory(component)).toBe("secrettool");
    });

     it('should return "other" for simple provider names', () => {
      const component: Component<ToolConfig> = {
        provider: "simpletool",
        component_type: "tool",
        config: {}
      };
      expect(getToolCategory(component)).toBe("other");
    });

    it('should return category based on second part if only two parts exist', () => {
      const component: Component<ToolConfig> = {
        provider: "company.category",
        component_type: "tool",
        config: {}
      };
      expect(getToolCategory(component)).toBe("category");
    });
  });
}); 