import { describe, expect, it, jest, beforeEach, afterEach } from '@jest/globals';
import { 
  isMcpTool, 
  isAgentTool,
  groupMcpToolsByServer,
  getToolIdentifier,
  getToolDisplayName,
  getToolDescription,
  getToolResponseDisplayName,
  getToolResponseDescription,
  getToolResponseCategory,
  getToolResponseIdentifier,
  toolResponseToAgentTool,
  parseGroupKind,
  getDiscoveredToolDisplayName,
  getDiscoveredToolDescription,
  getDiscoveredToolCategory,
  getDiscoveredToolIdentifier,
} from '../toolUtils';
import { k8sRefUtils } from '../k8sUtils';
import { Tool, ToolsResponse, DiscoveredTool } from "@/types";

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
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          name: "test-server",
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
      expect(isMcpTool({ type: "McpServer", mcpServer: { name: "test" } })).toBe(false);
      expect(isMcpTool({ type: "Inline" })).toBe(false);
    });
  });

  describe('isAgentTool', () => {
    it('should identify valid Agent tools', () => {
      const validAgentTool: Tool = {
        type: "Agent",
        agent: {
          name: "test-agent",
        }
      };
      expect(isAgentTool(validAgentTool)).toBe(true);
    });

    it('should reject invalid Agent tools', () => {
      expect(isAgentTool(null)).toBe(false);
      expect(isAgentTool(undefined)).toBe(false);
      expect(isAgentTool({})).toBe(false);
      expect(isAgentTool({ type: "Agent" })).toBe(undefined);
      expect(isAgentTool({ type: "Agent", agent: {} })).toBe(false);
    });
  });

  describe('groupMcpToolsByServer', () => {
    it('should group multiple MCP tools from same server into single entry and preserve namespace', () => {
      const githubServerRef = k8sRefUtils.toRef("default", "github-server");
      const tools: Tool[] = [
        {
          type: "McpServer",
          mcpServer: {
            name: githubServerRef,
            namespace: "kagent",
            apiGroup: "kagent.dev",
            kind: "MCPServer",
            toolNames: ["create_pull_request"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            name: githubServerRef,
            namespace: "kagent",
            apiGroup: "kagent.dev",
            kind: "MCPServer",
            toolNames: ["create_repository"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            name: githubServerRef,
            namespace: "kagent",
            apiGroup: "kagent.dev",
            kind: "MCPServer",
            toolNames: ["fork_repository"]
          }
        }
      ];

      const result = groupMcpToolsByServer(tools);

      expect(result.errors).toEqual([]);
      expect(result.groupedTools).toHaveLength(1);
      expect(result.groupedTools[0].type).toBe("McpServer");
      expect(result.groupedTools[0].mcpServer?.name).toBe(githubServerRef);
      expect(result.groupedTools[0].mcpServer?.namespace).toBe("kagent");
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
            name: githubServerRef,
            apiGroup: "kagent.dev",
            kind: "MCPServer",
            toolNames: ["create_pull_request"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            name: gitlabServerRef,
            apiGroup: "kagent.dev",
            kind: "MCPServer",
            toolNames: ["create_pull_request"]
          }
        }
      ];

      const result = groupMcpToolsByServer(tools);

      expect(result.errors).toEqual([]);
      expect(result.groupedTools).toHaveLength(2);

      // Find and verify github server tool
      const githubServerTool = result.groupedTools.find(t => t.mcpServer?.name === githubServerRef);
      expect(githubServerTool).toBeDefined();
      expect(githubServerTool?.mcpServer?.toolNames).toEqual(["create_pull_request"]);

      // Find and verify gitlab server tool
      const gitlabServerTool = result.groupedTools.find(t => t.mcpServer?.name === gitlabServerRef);
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
            name: defaultServerRef,
            apiGroup: "kagent.dev",
            kind: "MCPServer",
            toolNames: ["git_clone"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            name: toolsServerRef,
            apiGroup: "kagent.dev",
            kind: "MCPServer",
            toolNames: ["git_commit"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            name: defaultServerRef,
            apiGroup: "kagent.dev",
            kind: "MCPServer",
            toolNames: ["git_push"]
          }
        }
      ];

      const result = groupMcpToolsByServer(tools);

      expect(result.errors).toEqual([]);
      expect(result.groupedTools).toHaveLength(2);

      // Find the tool for default/git-server
      const defaultServerTool = result.groupedTools.find(t => t.mcpServer?.name === defaultServerRef);
      expect(defaultServerTool).toBeDefined();
      expect(defaultServerTool?.mcpServer?.toolNames).toEqual(["git_clone", "git_push"]);

      // Find the tool for tools/git-server
      const toolsServerTool = result.groupedTools.find(t => t.mcpServer?.name === toolsServerRef);
      expect(toolsServerTool).toBeDefined();
      expect(toolsServerTool?.mcpServer?.toolNames).toEqual(["git_commit"]);
    });

    it('should preserve non-MCP tools unchanged', () => {
      const githubServerRef = k8sRefUtils.toRef("default", "github-server");
      const agentTool: Tool = {
        type: "Agent",
        agent: {
          name: "test-agent",
        }
      };
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          name: githubServerRef,
          apiGroup: "kagent.dev",
          kind: "MCPServer",
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
            name: githubServerRef,
            apiGroup: "kagent.dev",
            kind: "MCPServer",
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
            name: githubServerRef,
            apiGroup: "kagent.dev",
            kind: "MCPServer",
            toolNames: ["create_pull_request", "get_pull_request"]
          }
        },
        {
          type: "McpServer",
          mcpServer: {
            name: githubServerRef,
            apiGroup: "kagent.dev",
            kind: "MCPServer",
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
            name: githubServerRef,
            apiGroup: "kagent.dev",
            kind: "MCPServer",
            toolNames: ["create_pull_request"]
          }
        },
        undefined,
        {
          type: "Agent",
          agent: {
            name: "test-agent",
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
              name: "",
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
      expect(result.errors).toEqual(["Invalid tool of type 'McpServer' was skipped"]);
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

  describe('getToolIdentifier', () => {
    it('should return correct identifier for Agent tools', () => {
      const agentTool: Tool = {
        type: "Agent",
        agent: {
          name: "test-agent"
        }
      };
      expect(getToolIdentifier(agentTool)).toBe("agent-test-agent");
    });

    it('should return correct identifier for MCP tools', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          name: "default/github-server",
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          toolNames: ["create_pull_request"]
        }
      };
      expect(getToolIdentifier(mcpTool)).toBe("mcp-default/github-server");
    });

    it('should handle MCP tools with missing name', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          name: "",
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          toolNames: ["create_pull_request"]
        }
      };
      expect(getToolIdentifier(mcpTool)).toBe("mcp-No name");
    });

    it('should return random identifier for unknown tool types', () => {
      const unknownTool = { type: "Unknown" as any } as Tool;
      const result = getToolIdentifier(unknownTool);
      expect(result).toMatch(/^unknown-tool-[a-z0-9]+$/);
    });

    it('should handle null/undefined agent ref', () => {
      const agentTool: Tool = {
        type: "Agent",
        agent: {
          name: ""
        }
      };
      expect(getToolIdentifier(agentTool)).toBe("agent-");
    });
  });

  describe('getToolDisplayName', () => {
    it('should return namespaced ref for Agent tools with explicit namespace', () => {
      const agentTool: Tool = {
        type: "Agent",
        agent: {
          name: "test-agent",
          namespace: "kagent"
        }
      };
      expect(getToolDisplayName(agentTool, "default")).toBe("kagent/test-agent");
    });

    it('should return namespaced ref for Agent tools using default namespace', () => {
      const agentTool: Tool = {
        type: "Agent",
        agent: {
          name: "test-agent"
        }
      };
      expect(getToolDisplayName(agentTool, "default")).toBe("default/test-agent");
    });

    it('should return namespaced ref for MCP tools with explicit namespace', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          name: "github-server",
          namespace: "tools",
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          toolNames: ["create_pull_request"]
        }
      };
      expect(getToolDisplayName(mcpTool, "default")).toBe("tools/github-server");
    });

    it('should return namespaced ref for MCP tools using default namespace', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          name: "github-server",
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          toolNames: ["create_pull_request"]
        }
      };
      expect(getToolDisplayName(mcpTool, "default")).toBe("default/github-server");
    });

    it('should return "Unknown Tool" for MCP tools with missing name', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          name: "",
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          toolNames: ["create_pull_request"]
        }
      };
      expect(getToolDisplayName(mcpTool, "default")).toBe("Unknown Tool");
    });

    it('should return "Unknown Tool" for unknown tool types', () => {
      const unknownTool = { type: "Unknown" as any } as Tool;
      expect(getToolDisplayName(unknownTool, "default")).toBe("Unknown Tool");
    });
  });

  describe('getToolDescription', () => {
    it('should return "Agent Tool" for Agent tools', () => {
      const agentTool: Tool = {
        type: "Agent",
        agent: {
          name: "test-agent"
        }
      };
      expect(getToolDescription(agentTool, [])).toBe("Agent");
    });

    it('should return description from availableTools for MCP tools', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          name: "default/github-server",
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          toolNames: ["create_pull_request"]
        }
      };
      const availableTools: ToolsResponse[] = [
        {
          id: "create_pull_request",
          server_name: "default/github-server",
          description: "Creates a new pull request",
          created_at: "2023-01-01T00:00:00Z",
          updated_at: "2023-01-01T00:00:00Z",
          deleted_at: "",
          group_kind: "MCPServer.kagent.dev"
        }
      ];
      expect(getToolDescription(mcpTool, availableTools)).toBe("Creates a new pull request");
    });

    it('should return fallback description for MCP tools not found in availableTools', () => {
      const mcpTool: Tool = {
        type: "McpServer",
        mcpServer: {
          name: "default/github-server",
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          toolNames: ["create_pull_request"]
        }
      };
      expect(getToolDescription(mcpTool, [])).toBe("MCP tool description not available");
    });

    it('should return "No description available" for unknown tool types', () => {
      const unknownTool = { type: "Unknown" as any } as Tool;
      expect(getToolDescription(unknownTool, [])).toBe("No description available");
    });
  });

  describe('getToolResponseDisplayName', () => {
    it('should return tool id when available', () => {
      const tool: ToolsResponse = {
        id: "create_pull_request",
        server_name: "default/github-server",
        description: "Creates a new pull request",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer.kagent.dev"
      };
      expect(getToolResponseDisplayName(tool)).toBe("create_pull_request");
    });

    it('should return "Unknown Tool" when id is missing', () => {
      const tool: ToolsResponse = {
        id: "",
        server_name: "default/github-server",
        description: "Creates a new pull request",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer.kagent.dev"
      };
      expect(getToolResponseDisplayName(tool)).toBe("Unknown Tool");
    });
  });

  describe('getToolResponseDescription', () => {
    it('should return tool description when available', () => {
      const tool: ToolsResponse = {
        id: "create_pull_request",
        server_name: "default/github-server",
        description: "Creates a new pull request",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer.kagent.dev"
      };
      expect(getToolResponseDescription(tool)).toBe("Creates a new pull request");
    });

    it('should return "No description available" when description is missing', () => {
      const tool: ToolsResponse = {
        id: "create_pull_request",
        server_name: "default/github-server",
        description: "",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer.kagent.dev"
      };
      expect(getToolResponseDescription(tool)).toBe("No description available");
    });
  });

  describe('getToolResponseCategory', () => {
    it('should extract category from kagent tool server tools', () => {
      const tool: ToolsResponse = {
        id: "git_clone",
        server_name: "kagent/kagent-tool-server",
        description: "Clone a git repository",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer.kagent.dev"
      };
      expect(getToolResponseCategory(tool)).toBe("git");
    });

    it('should return full tool id when no underscore in kagent tool', () => {
      const tool: ToolsResponse = {
        id: "gitclone",
        server_name: "kagent/kagent-tool-server",
        description: "Clone a git repository",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer.kagent.dev"
      };
      expect(getToolResponseCategory(tool)).toBe("gitclone");
    });

    it('should return server_name for non-kagent tools', () => {
      const tool: ToolsResponse = {
        id: "create_pull_request",
        server_name: "default/github-server",
        description: "Creates a new pull request",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer.kagent.dev"
      };
      expect(getToolResponseCategory(tool)).toBe("default/github-server");
    });
  });

  describe('getToolResponseIdentifier', () => {
    it('should return combined server name and tool id', () => {
      const tool: ToolsResponse = {
        id: "create_pull_request",
        server_name: "default/github-server",
        description: "Creates a new pull request",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer.kagent.dev"
      };
      expect(getToolResponseIdentifier(tool)).toBe("default/github-server-create_pull_request");
    });
  });

  describe('parseGroupKind', () => {
    it('should parse groupKind with both kind and apiGroup', () => {
      expect(parseGroupKind('RemoteMCPServer.kagent.dev')).toEqual({
        apiGroup: 'kagent.dev',
        kind: 'RemoteMCPServer'
      });
    });

    it('should parse groupKind with multi-part apiGroup', () => {
      expect(parseGroupKind('Service.core.v1')).toEqual({
        apiGroup: 'core.v1',
        kind: 'Service'
      });
    });

    it('should parse groupKind with only kind', () => {
      expect(parseGroupKind('MCPServer')).toEqual({
        apiGroup: 'kagent.dev',
        kind: 'MCPServer'
      });
    });

    it('should handle empty groupKind', () => {
      expect(parseGroupKind('')).toEqual({
        apiGroup: 'kagent.dev',
        kind: 'MCPServer'
      });
    });

    it('should handle Service without apiGroup', () => {
      expect(parseGroupKind('Service')).toEqual({
        apiGroup: 'kagent.dev',
        kind: 'Service'
      });
    });

    it('should handle whitespace-only input', () => {
      expect(parseGroupKind('   ')).toEqual({
        apiGroup: 'kagent.dev',
        kind: 'MCPServer'
      });
    });

    it('should trim whitespace from input', () => {
      expect(parseGroupKind('  RemoteMCPServer.kagent.dev  ')).toEqual({
        apiGroup: 'kagent.dev',
        kind: 'RemoteMCPServer'
      });
    });
  });

  describe('toolResponseToAgentTool', () => {
    it('should use groupKind to set correct apiGroup and kind, and extract namespace from serverRef', () => {
      const tool: ToolsResponse = {
        id: "create_pull_request",
        server_name: "default/ragflow-mcp-server",
        description: "Creates a new pull request",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "RemoteMCPServer.kagent.dev"
      };

      const result = toolResponseToAgentTool(tool, tool.server_name);

      expect(result).toEqual({
        type: "McpServer",
        mcpServer: {
          name: "ragflow-mcp-server",
          namespace: "default",
          apiGroup: "kagent.dev",
          kind: "RemoteMCPServer",
          toolNames: ["create_pull_request"]
        }
      });
    });

    it('should handle special case for kagent-querydoc with empty apiGroup and extract namespace', () => {
      const tool: ToolsResponse = {
        id: "query_documentation",
        server_name: "kagent/kagent-querydoc",
        description: "Query documentation",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "Service"
      };

      const result = toolResponseToAgentTool(tool, tool.server_name);

      expect(result).toEqual({
        type: "McpServer",
        mcpServer: {
          name: "kagent-querydoc",
          namespace: "kagent",
          apiGroup: "",
          kind: "Service",
          toolNames: ["query_documentation"]
        }
      });
    });

    it('should handle fallback when groupKind is empty and extract namespace', () => {
      const tool: ToolsResponse = {
        id: "some_tool",
        server_name: "default/some-server",
        description: "Some tool",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: ""
      };

      const result = toolResponseToAgentTool(tool, tool.server_name);

      expect(result).toEqual({
        type: "McpServer",
        mcpServer: {
          name: "some-server",
          namespace: "default",
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          toolNames: ["some_tool"]
        }
      });
    });

    it('should handle groupKind with only kind and extract namespace', () => {
      const tool: ToolsResponse = {
        id: "some_tool",
        server_name: "default/some-server",
        description: "Some tool",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer"
      };

      const result = toolResponseToAgentTool(tool, tool.server_name);

      expect(result).toEqual({
        type: "McpServer",
        mcpServer: {
          name: "some-server",
          namespace: "default",
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          toolNames: ["some_tool"]
        }
      });
    });

    it('should handle serverRef without namespace (no slash)', () => {
      const tool: ToolsResponse = {
        id: "some_tool",
        server_name: "some-server",
        description: "Some tool",
        created_at: "2023-01-01T00:00:00Z",
        updated_at: "2023-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer"
      };

      const result = toolResponseToAgentTool(tool, tool.server_name);

      expect(result).toEqual({
        type: "McpServer",
        mcpServer: {
          name: "some-server",
          namespace: undefined,
          apiGroup: "kagent.dev",
          kind: "MCPServer",
          toolNames: ["some_tool"]
        }
      });
    });
  });

  describe('getDiscoveredToolDisplayName', () => {
    it('should return tool name when available', () => {
      const tool: DiscoveredTool = {
        name: "create_pull_request",
        description: "Creates a new pull request"
      };
      expect(getDiscoveredToolDisplayName(tool)).toBe("create_pull_request");
    });

    it('should return "Unknown Tool" when name is missing', () => {
      const tool: DiscoveredTool = {
        name: "",
        description: "Creates a new pull request"
      };
      expect(getDiscoveredToolDisplayName(tool)).toBe("Unknown Tool");
    });
  });

  describe('getDiscoveredToolDescription', () => {
    it('should return tool description when available', () => {
      const tool: DiscoveredTool = {
        name: "create_pull_request",
        description: "Creates a new pull request"
      };
      expect(getDiscoveredToolDescription(tool)).toBe("Creates a new pull request");
    });

    it('should return "No description available" when description is missing', () => {
      const tool: DiscoveredTool = {
        name: "create_pull_request",
        description: ""
      };
      expect(getDiscoveredToolDescription(tool)).toBe("No description available");
    });
  });

  describe('getDiscoveredToolCategory', () => {
    it('should extract category from kagent tool server tools', () => {
      const tool: DiscoveredTool = {
        name: "git_clone",
        description: "Clone a git repository"
      };
      const serverRef = "kagent/kagent-tool-server";
      expect(getDiscoveredToolCategory(tool, serverRef)).toBe("git");
    });

    it('should return full tool name when no underscore in kagent tool', () => {
      const tool: DiscoveredTool = {
        name: "gitclone",
        description: "Clone a git repository"
      };
      const serverRef = "kagent/kagent-tool-server";
      expect(getDiscoveredToolCategory(tool, serverRef)).toBe("gitclone");
    });

    it('should return serverRef for non-kagent tools', () => {
      const tool: DiscoveredTool = {
        name: "create_pull_request",
        description: "Creates a new pull request"
      };
      const serverRef = "default/github-server";
      expect(getDiscoveredToolCategory(tool, serverRef)).toBe("default/github-server");
    });

    it('should handle serverRef that contains kagent-tool-server but not exact match', () => {
      const tool: DiscoveredTool = {
        name: "git_clone",
        description: "Clone a git repository"
      };
      const serverRef = "custom/kagent-tool-server-v2";
      expect(getDiscoveredToolCategory(tool, serverRef)).toBe("git");
    });
  });

  describe('getDiscoveredToolIdentifier', () => {
    it('should return combined server ref and tool name', () => {
      const tool: DiscoveredTool = {
        name: "create_pull_request",
        description: "Creates a new pull request"
      };
      const serverRef = "default/github-server";
      expect(getDiscoveredToolIdentifier(tool, serverRef)).toBe("default/github-server-create_pull_request");
    });

    it('should handle empty tool name', () => {
      const tool: DiscoveredTool = {
        name: "",
        description: "Creates a new pull request"
      };
      const serverRef = "default/github-server";
      expect(getDiscoveredToolIdentifier(tool, serverRef)).toBe("default/github-server-");
    });
  });

  describe('Edge cases and error handling', () => {
    it('should handle null/undefined inputs for getToolIdentifier', () => {
      expect(getToolIdentifier(null as any)).toMatch(/^unknown-tool-[a-z0-9]+$/);
      expect(getToolIdentifier(undefined as any)).toMatch(/^unknown-tool-[a-z0-9]+$/);
    });

    it('should handle null/undefined inputs for getToolDisplayName', () => {
      expect(getToolDisplayName(null as any, "default")).toBe("Unknown Tool");
      expect(getToolDisplayName(undefined as any, "default")).toBe("Unknown Tool");
    });

    it('should handle null/undefined inputs for getToolDescription', () => {
      expect(getToolDescription(null as any, [])).toBe("No description available");
      expect(getToolDescription(undefined as any, [])).toBe("No description available");
    });

    it('should handle malformed tool objects', () => {
      const malformedTool = { type: "Agent" } as Tool; // Missing agent property
      expect(getToolIdentifier(malformedTool)).toMatch(/^unknown-tool-[a-z0-9]+$/);
      expect(getToolDisplayName(malformedTool, "default")).toBe("Unknown Tool");
      expect(getToolDescription(malformedTool, [])).toBe("No description available");
    });

    it('should handle MCP tools with null/undefined mcpServer', () => {
      const malformedMcpTool = { type: "McpServer" } as Tool; // Missing mcpServer property
      expect(getToolIdentifier(malformedMcpTool)).toMatch(/^unknown-tool-[a-z0-9]+$/);
      expect(getToolDisplayName(malformedMcpTool, "default")).toBe("Unknown Tool");
      expect(getToolDescription(malformedMcpTool, [])).toBe("No description available");
    });
  });
});
