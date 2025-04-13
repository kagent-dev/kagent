import { 
  isMcpTool, 
  isInlineTool, 
  getToolDisplayName, 
  getToolDescription, 
  getToolIdentifier, 
  getToolProvider, 
  isSameTool 
} from '../toolUtils';
import { AgentTool, Component, MCPToolConfig, ToolConfig } from "@/types/datamodel";

describe('Tool Utility Functions', () => {
  describe('isMcpTool', () => {
    it('should identify valid MCP tools', () => {
      const validMcpTool: AgentTool = {
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

  describe('isInlineTool', () => {
    it('should identify valid inline tools', () => {
      const validInlineTool: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test-provider",
          label: "Test Tool",
          description: "Test Description"
        }
      };
      expect(isInlineTool(validInlineTool)).toBe(true);
    });

    it('should reject invalid inline tools', () => {
      expect(isInlineTool(null)).toBe(false);
      expect(isInlineTool(undefined)).toBe(false);
      expect(isInlineTool({})).toBe(false);
      expect(isInlineTool({ type: "Inline" })).toBe(false);
      expect(isInlineTool({ type: "Inline", inline: {} })).toBe(false);
      expect(isInlineTool({ type: "McpServer" })).toBe(false);
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
      const mcpTool: AgentTool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(getToolDisplayName(mcpTool)).toBe("tool1");
    });

    it('should handle inline tools', () => {
      const inlineTool: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test.provider.ToolName",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(getToolDisplayName(inlineTool)).toBe("Inline Label");
    });

    it('should fall back to provider name for inline tools without label', () => {
      const inlineTool: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test.provider.ToolName",
          description: "Inline Description"
        }
      };
      expect(getToolDisplayName(inlineTool)).toBe("ToolName");
    });

    it('should handle unknown tool types', () => {
      const unknownTool = { someProperty: "value" };
      expect(getToolDisplayName(unknownTool as any)).toBe("Unknown Tool");
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

    it('should handle inline tools', () => {
      const inlineTool: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test.provider",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(getToolDescription(inlineTool)).toBe("Inline Description");
    });

    it('should handle MCP server tools', () => {
      const mcpTool: AgentTool = {
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
      expect(getToolIdentifier(mcpAdapterTool)).toBe("mcptool-MCP Tool Name");
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
      const mcpTool: AgentTool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(getToolIdentifier(mcpTool)).toBe("mcptool-test-server-tool1");
    });

    it('should handle inline tools', () => {
      const inlineTool: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test.provider",
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
      const mcpTool: AgentTool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(getToolProvider(mcpTool)).toBe("test-server");
    });

    it('should handle inline tools', () => {
      const inlineTool: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test.provider",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(getToolProvider(inlineTool)).toBe("test.provider");
    });

    it('should handle unknown tool types', () => {
      const unknownTool = { someProperty: "value" };
      expect(getToolProvider(unknownTool as any)).toBe("unknown");
    });
  });

  describe('isSameTool', () => {
    it('should return false for undefined tools', () => {
      expect(isSameTool(undefined, undefined)).toBe(false);
      expect(isSameTool(undefined, {} as AgentTool)).toBe(false);
      expect(isSameTool({} as AgentTool, undefined)).toBe(false);
    });

    it('should identify same MCP tools', () => {
      const tool1: AgentTool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      const tool2: AgentTool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool3"]
        }
      };
      expect(isSameTool(tool1, tool2)).toBe(true);
    });

    it('should identify same inline tools', () => {
      const tool1: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test.provider",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      const tool2: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test.provider",
          label: "Different Label",
          description: "Different Description"
        }
      };
      expect(isSameTool(tool1, tool2)).toBe(true);
    });

    it('should identify different tools', () => {
      const mcpTool: AgentTool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      const inlineTool: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test.provider",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(isSameTool(mcpTool, inlineTool)).toBe(false);
    });

    it('should identify different MCP tools', () => {
      const tool1: AgentTool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server-1",
          toolNames: ["tool1", "tool2"]
        }
      };
      const tool2: AgentTool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server-2",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(isSameTool(tool1, tool2)).toBe(false);
    });

    it('should identify different inline tools', () => {
      const tool1: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test.provider1",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      const tool2: AgentTool = {
        type: "Inline",
        inline: {
          provider: "test.provider2",
          label: "Inline Label",
          description: "Inline Description"
        }
      };
      expect(isSameTool(tool1, tool2)).toBe(false);
    });
  });
}); 