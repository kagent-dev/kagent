/**
 * This utility file provides functions to convert between different tool types
 */

import { AgentConfig, AgentResponse, AgentTool, AssistantAgentConfig, Component, MCPToolConfig, RoundRobinGroupChatConfig, SelectorGroupChatConfig, TeamConfig, ToolConfig } from "@/types/datamodel";
import { getToolIdentifier, getToolProvider } from "./data";

export function isMCPToolConfig(config: ToolConfig): config is MCPToolConfig {
  return (
    config &&
    typeof config === "object" &&
    "server_params" in config &&
    "tool" in config &&
    typeof config.tool === "object" &&
    "name" in config.tool
  );
}

/**
 * Converts a Component<ToolConfig> to an AgentTool
 */
export function componentToAgentTool(component: Component<ToolConfig>): AgentTool {
  // Check if it's an MCP tool first
  if (isMCPToolConfig(component.config)) {
    const mcpConfig = component.config;
    
    return {
      type: "McpServer",
      mcpServer: {
        toolServer: component.label || "",
        toolNames: [mcpConfig.tool.name]
      }
    };
  } else {
    const r ={ 
      type: "Inline",
      inline: {
        provider: component.provider,
        description: component.description || "",
        config: component.config
      }
    } as AgentTool;
    return r;
  }
}

/**
 * Finds a Component<ToolConfig> matching an AgentTool from a list of available tools
 * @param agentTool The AgentTool to find
 * @param availableTools List of available Component<ToolConfig>
 * @returns The matching Component<ToolConfig> or undefined if not found
 */
export function findComponentForAgentTool(
  agentTool: AgentTool,
  availableTools: Component<ToolConfig>[]
): Component<ToolConfig> | undefined {
    const result = availableTools.find((tool) => getToolIdentifier(tool) === getToolIdentifier(agentTool));
  return result;
}

/**
 * Type guard to check if config is RoundRobinGroupChatConfig
 */
function isRoundRobinGroupChatConfig(config: TeamConfig): config is RoundRobinGroupChatConfig {
  return (config as RoundRobinGroupChatConfig).participants !== undefined;
}

/**
 * Type guard to check if config is SelectorGroupChatConfig
 */
function isSelectorGroupChatConfig(config: TeamConfig): config is SelectorGroupChatConfig {
  return (config as SelectorGroupChatConfig).participants !== undefined;
}

/**
 * Type guard to check if config is AssistantAgentConfig
 */
function isAssistantAgentConfig(config: AgentConfig): config is AssistantAgentConfig {
  return (config as AssistantAgentConfig).tools !== undefined;
}