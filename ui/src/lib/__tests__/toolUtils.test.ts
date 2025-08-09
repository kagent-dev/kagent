import { describe, expect, it, jest, beforeEach, afterEach } from '@jest/globals';
import { 
  isMcpTool, 
  isAgentTool,
  groupMcpToolsByServer,
} from '../toolUtils';
import { k8sRefUtils } from '../k8sUtils';
import { Tool, MCPToolConfig, AgentTool } from "@/types/datamodel";

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
