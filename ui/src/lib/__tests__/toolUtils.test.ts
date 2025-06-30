import { describe, expect, it, jest, beforeEach, afterEach } from '@jest/globals';
import { 
  isMcpTool, 
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
  groupMcpToolsByServer,
  SSE_MCP_TOOL_PROVIDER_NAME,
  STDIO_MCP_TOOL_PROVIDER_NAME,
  STREAMABLE_HTTP_MCP_TOOL_PROVIDER_NAME,
  isBuiltInTool
} from '../toolUtils';
import { k8sRefUtils } from '../k8sUtils';
import { Tool, Component, MCPToolConfig, ToolConfig, AgentTool } from "@/types/datamodel";

describe('Tool Utility Functions', () => {
  let consoleWarnSpy: any;

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
        config: {} as ToolConfig
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

    it('should handle unknown tool types', () => {
      const unknownTool = { someProperty: "value" };
      expect(getToolDisplayName(unknownTool as any)).toBe("Unknown Tool");
      expect(console.warn).toHaveBeenCalledWith("Unknown tool type:", expect.objectContaining(unknownTool));
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
        config: {} as ToolConfig
      };
      expect(getToolDescription(componentTool)).toBe("Component Description");
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
      expect(getToolIdentifier(mcpAdapterTool)).toBe("Adapter Label-MCP Tool Name");
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
      expect(getToolIdentifier(mcpAdapterTool)).toBe("Adapter Label-MCP Tool Name");
    });

    it('should handle regular component tools', () => {
      const componentTool: Component<ToolConfig> = {
        provider: "test.provider",
        label: "Component Label",
        description: "Component Description",
        component_type: "tool",
        config: {} as ToolConfig
      };
      const result = getToolIdentifier(componentTool);
      expect(result).toMatch(/^unknown-/);
    });

    it('should handle MCP server tools', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: "test-server",
          toolNames: ["tool1", "tool2"]
        }
      };
      expect(getToolIdentifier(mcpTool)).toBe("test-server-tool1");
    });

    it('should handle unknown tool types', () => {
      const unknownTool = { someProperty: "value" };
      const result = getToolIdentifier(unknownTool as any);
      expect(result).toMatch(/^unknown-/);
      expect(console.warn).toHaveBeenCalledWith("Unknown tool type:", expect.objectContaining(unknownTool));
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
        config: {} as ToolConfig
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

    it('should handle unknown tool types', () => {
      const unknownTool = { someProperty: "value" };
      expect(getToolProvider(unknownTool as any)).toBe("unknown");
      expect(console.warn).toHaveBeenCalledWith("Unknown tool type:", expect.objectContaining(unknownTool));
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

    it('should identify same MCP server tools', () => {
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
          toolNames: ["tool1", "tool2"]
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
        type: "Agent",
        agent: {
          ref: "test-agent",
          description: "Agent description"
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
        provider: SSE_MCP_TOOL_PROVIDER_NAME,
        label: "mcp.server.name", // Matches toolServer
        component_type: "tool",
        config: { server_params: { url: "http://example.com/sse2" }, tool: { name: "mcp_tool_name", description: "desc", inputSchema: {} } } as MCPToolConfig // Matches toolName
      },
      {
        provider: "other.provider",
        label: "Other Component",
        component_type: "tool",
        config: {} as ToolConfig
      }
    ];

    it('should find a matching MCP component for an McpServer tool', () => {
      const agentTool: Tool = {
        type: "McpServer",
        mcpServer: { toolServer: "mcp.server.name", toolNames: ["mcp_tool_name"] }
      };
      const expectedComponent = components[0];
      expect(findComponentForAgentTool(agentTool, components)).toBe(expectedComponent);
    });

    it('should not find a match for an Agent tool (identifier mismatch)', () => {
      const agentTool: Tool = {
        type: "Agent",
        agent: { ref: "some-agent" } as AgentTool
      };
      expect(findComponentForAgentTool(agentTool, components)).toBeUndefined();
    });

    it('should find a component matching a tool derived from it', () => {
      const component = components[0];
      const derivedTool = componentToAgentTool(component);
      expect(findComponentForAgentTool(derivedTool, components)).toBe(component);
    });
  });

  describe('isMcpProvider', () => {
    it('should return true for known MCP provider names', () => {
      expect(isMcpProvider(SSE_MCP_TOOL_PROVIDER_NAME)).toBe(true);
      expect(isMcpProvider(STDIO_MCP_TOOL_PROVIDER_NAME)).toBe(true);
      expect(isMcpProvider(STREAMABLE_HTTP_MCP_TOOL_PROVIDER_NAME)).toBe(true);
    });

    it('should return false for other provider names', () => {
      expect(isMcpProvider("autogen_ext.tools.something_else")).toBe(false);
      expect(isMcpProvider("my.custom.provider")).toBe(false);
      expect(isMcpProvider("")).toBe(false);
    });
  });

  describe('isBuiltInTool', () => {
    it('should return true for built-in tools', () => {
      const component: Component<MCPToolConfig> = {
        provider: SSE_MCP_TOOL_PROVIDER_NAME,
        label: "kagent-tool-server",
        component_type: "tool",
        config: { server_params: { url: "http://example.com/sse3" }, tool: { name: "k8s_get_pods", description: "desc", inputSchema: {} } }
      };
      expect(isBuiltInTool(component)).toBe(true);
    });

    it('should return false for non-built-in tools', () => {
      const component: Component<MCPToolConfig> = {
        provider: SSE_MCP_TOOL_PROVIDER_NAME,
        label: "my-tool-server",
        component_type: "tool",
        config: { server_params: { url: "http://example.com/sse3" }, tool: { name: "k8s_get_pods", description: "desc", inputSchema: {} } }
      };
      expect(isBuiltInTool(component)).toBe(false);
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

    it('should return a category for built-in tools', () => {
      const component: Component<MCPToolConfig> = {
        provider: SSE_MCP_TOOL_PROVIDER_NAME,
        label: "kagent-tool-server",
        component_type: "tool",
        config: { server_params: { url: "http://example.com/sse3" }, tool: { name: "k8s_get_pods", description: "desc", inputSchema: {} } }
      };
      expect(getToolCategory(component)).toBe("k8s");
    });

    it('should return the label for non-MCP provider tools if available', () => {
      const component: Component<ToolConfig> = {
        provider: "kagent.tools.grafana",
        label: "Grafana",
        component_type: "tool",
        config: {} as ToolConfig
      };
      expect(getToolCategory(component)).toBe("Grafana");
    });

    it('should return "MCP Server" if a label is not available', () => {
      const component: Component<ToolConfig> = {
        provider: "kagent.tools.grafana",
        component_type: "tool",
        config: {} as ToolConfig
      };
      expect(getToolCategory(component)).toBe("MCP Server");
    });
  });

  describe('groupMcpToolsByServer', () => {
    it('should group multiple MCP tools from same server into single entry', () => {
      const githubServerRef = k8sRefUtils.toRef("default", "github-server");
      const tools: Tool[] = [
        {
          type: "McpServer",
          mcpServer: {
            toolServer: githubServerRef,
            toolNames: ["create_pull_request"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            toolServer: githubServerRef,
            toolNames: ["create_repository"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            toolServer: githubServerRef,
            toolNames: ["fork_repository"]
          }
        }
      ];

      const result = groupMcpToolsByServer(tools);

      expect(result.errors).toEqual([]);
      expect(result.groupedTools).toHaveLength(1);
      expect(result.groupedTools[0].type).toBe("McpServer");
      expect(result.groupedTools[0].mcpServer?.toolServer).toBe(githubServerRef);
      expect(result.groupedTools[0].mcpServer?.toolNames).toEqual([
        "create_pull_request",
        "create_repository",
        "fork_repository"
      ]);
    });

    it('should keep MCP tools from different servers separate', () => {
      const githubServerRef = k8sRefUtils.toRef("default", "github-server");
      const gitlabServerRef = k8sRefUtils.toRef("tools", "gitlab-server");
      const tools: Tool[] = [
        {
          type: "McpServer",
          mcpServer: {
            toolServer: githubServerRef,
            toolNames: ["create_pull_request"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            toolServer: gitlabServerRef,
            toolNames: ["create_pull_request"]
          }
        }
      ];

      const result = groupMcpToolsByServer(tools);

      expect(result.errors).toEqual([]);
      expect(result.groupedTools).toHaveLength(2);

      // Find and verify github server tool
      const githubServerTool = result.groupedTools.find(t => t.mcpServer?.toolServer === githubServerRef);
      expect(githubServerTool).toBeDefined();
      expect(githubServerTool?.mcpServer?.toolNames).toEqual(["create_pull_request"]);

      // Find and verify gitlab server tool
      const gitlabServerTool = result.groupedTools.find(t => t.mcpServer?.toolServer === gitlabServerRef);
      expect(gitlabServerTool).toBeDefined();
      expect(gitlabServerTool?.mcpServer?.toolNames).toEqual(["create_pull_request"]);
    });

    it('should keep MCP tools from servers with same names but different namespaces separate', () => {
      const defaultServerRef = k8sRefUtils.toRef("default", "git-server");
      const toolsServerRef = k8sRefUtils.toRef("tools", "git-server");
      const tools: Tool[] = [
        {
          type: "McpServer",
          mcpServer: {
            toolServer: defaultServerRef,
            toolNames: ["git_clone"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            toolServer: toolsServerRef,
            toolNames: ["git_commit"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            toolServer: defaultServerRef,
            toolNames: ["git_push"]
          }
        }
      ];

      const result = groupMcpToolsByServer(tools);

      expect(result.errors).toEqual([]);
      expect(result.groupedTools).toHaveLength(2);

      // Find the tool for default/git-server
      const defaultServerTool = result.groupedTools.find(t => t.mcpServer?.toolServer === defaultServerRef);
      expect(defaultServerTool).toBeDefined();
      expect(defaultServerTool?.mcpServer?.toolNames).toEqual(["git_clone", "git_push"]);

      // Find the tool for tools/git-server
      const toolsServerTool = result.groupedTools.find(t => t.mcpServer?.toolServer === toolsServerRef);
      expect(toolsServerTool).toBeDefined();
      expect(toolsServerTool?.mcpServer?.toolNames).toEqual(["git_commit"]);
    });

    it('should preserve non-MCP tools unchanged', () => {
      const githubServerRef = k8sRefUtils.toRef("default", "github-server");
      const agentTool: Tool = {
        type: "Agent",
        agent: {
          ref: "test-agent",
          description: "Test agent"
        }
      };
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          toolServer: githubServerRef,
          toolNames: ["create_pull_request"]
        }
      };
      const tools: Tool[] = [agentTool, mcpTool];

      const result = groupMcpToolsByServer(tools);

      expect(result.errors).toEqual([]);
      expect(result.groupedTools).toHaveLength(2);

      // Verify agent tool is unchanged
      const resultAgentTool = result.groupedTools.find(t => t.type === "Agent");
      expect(resultAgentTool).toEqual(agentTool);

      // Verify MCP tool is present (may be grouped)
      expect(result.groupedTools.find(t => t.type === "McpServer")).toBeDefined();
    });

    it('should handle empty tool names arrays', () => {
      const githubServerRef = k8sRefUtils.toRef("default", "github-server");
      const tools: Tool[] = [
        {
          type: "McpServer",
          mcpServer: {
            toolServer: githubServerRef,
            toolNames: []
          }
        }
      ];

      const result = groupMcpToolsByServer(tools);

      expect(result.errors).toEqual([]);
      expect(result.groupedTools).toHaveLength(1);
      expect(result.groupedTools[0].mcpServer?.toolNames).toEqual([]);
    });

    it('should remove duplicate tool names within same server', () => {
      const githubServerRef = k8sRefUtils.toRef("default", "github-server");
      const tools: Tool[] = [
        {
          type: "McpServer",
          mcpServer: {
            toolServer: githubServerRef,
            toolNames: ["create_pull_request", "get_pull_request"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            toolServer: githubServerRef,
            toolNames: ["create_pull_request", "list_pull_requests"] // duplicate create_pull_request
          }
        }
      ];

      const result = groupMcpToolsByServer(tools);

      expect(result.errors).toEqual([]);
      expect(result.groupedTools).toHaveLength(1);
      expect(result.groupedTools[0].mcpServer?.toolNames).toEqual([
        "create_pull_request",
        "get_pull_request",
        "list_pull_requests"
      ]);
    });

    it('should handle null/undefined inputs gracefully', () => {
      expect(groupMcpToolsByServer(null as any)).toEqual({ groupedTools: [], errors: ["Invalid input: tools must be an array"] });
      expect(groupMcpToolsByServer(undefined as any)).toEqual({ groupedTools: [], errors: ["Invalid input: tools must be an array"] });
      expect(groupMcpToolsByServer("not an array" as any)).toEqual({ groupedTools: [], errors: ["Invalid input: tools must be an array"] });
    });

    it('should skip null/undefined tools in array', () => {
      const githubServerRef = k8sRefUtils.toRef("default", "github-server");
      const tools: (Tool | null | undefined)[] = [
        null,
        {
          type: "McpServer",
          mcpServer: {
            toolServer: githubServerRef,
            toolNames: ["create_pull_request"]
          }
        },
        undefined,
        {
          type: "Agent",
          agent: {
            ref: "test-agent",
            description: "Test agent"
          }
        }
      ];

      const result = groupMcpToolsByServer(tools as Tool[]);

      expect(result.errors).toEqual(["Invalid tool of type 'null/undefined' was skipped", "Invalid tool of type 'null/undefined' was skipped"]);
      expect(result.groupedTools).toHaveLength(2);
      expect(result.groupedTools.some(t => t.type === "McpServer")).toBe(true);
      expect(result.groupedTools.some(t => t.type === "Agent")).toBe(true);
    });

    it('should handle MCP tools with missing or invalid toolServer', () => {
      const tools: Tool[] = [
        {
          type: "McpServer",
          mcpServer: {
            toolServer: "",
            toolNames: ["create_pull_request"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            toolNames: ["create_repository"]
          } as any // Missing toolServer
        },
        {
          type: "McpServer",
          mcpServer: null as any
        }
      ];

      const result = groupMcpToolsByServer(tools);

      // Should skip invalid tools and report errors
      expect(result.errors).toEqual(["Invalid tool of type 'McpServer' was skipped", "Invalid tool of type 'McpServer' was skipped"]);
      expect(result.groupedTools).toHaveLength(1);
      expect(result.groupedTools[0].type).toBe("McpServer");
    });

    it('should handle MCP tools with undefined/null toolNames', () => {
      const githubServerRef = k8sRefUtils.toRef("default", "github-server");
      const tools: Tool[] = [
        {
          type: "McpServer",
          mcpServer: {
            toolServer: githubServerRef,
            toolNames: null as any
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            toolServer: githubServerRef
            // toolNames is undefined
          } as any
        }
      ];

      const result = groupMcpToolsByServer(tools);

      // Both tools should be skipped as invalid (null/undefined toolNames)
      expect(result.errors).toEqual(["Invalid tool of type 'McpServer' was skipped", "Invalid tool of type 'McpServer' was skipped"]);
      expect(result.groupedTools).toHaveLength(0);
    });
  });
});
