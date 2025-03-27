/**
 * This utility file provides functions to convert between different tool types
 */

import { AgentTool, Component, ToolConfig } from "@/types/datamodel";

/**
 * Converts a Component<ToolConfig> to an AgentTool
 * @param tool The Component<ToolConfig> to convert
 * @returns An AgentTool based on the provided Component
 */
export function componentToAgentTool(tool: Component<ToolConfig>): AgentTool {
  return {
    provider: tool.provider,
    description: tool.description || "",
    config: Object.entries(tool.config || {}).reduce((acc, [key, value]) => {
      acc[key] = String(value); // Ensure all values are strings
      return acc;
    }, {} as Record<string, string>),
  };
}

/**
 * Converts an array of Component<ToolConfig> to an array of AgentTools
 * @param tools Array of Component<ToolConfig> to convert
 * @returns Array of AgentTools
 */
export function componentsToAgentTools(tools: Component<ToolConfig>[]): AgentTool[] {
  return tools.map(componentToAgentTool);
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
  return availableTools.find((tool) => tool.provider === agentTool.provider);
}

/**
 * Checks if an AgentTool is represented in an array of Component<ToolConfig>
 * @param agentTool The AgentTool to check
 * @param components Array of Component<ToolConfig> to search in
 * @returns True if the AgentTool is found, false otherwise
 */
export function isAgentToolInComponents(
  agentTool: AgentTool,
  components: Component<ToolConfig>[]
): boolean {
  return components.some((component) => component.provider === agentTool.provider);
}

/**
 * Updates an AgentTool with new configuration values
 * @param agentTool The AgentTool to update
 * @param newConfig The new configuration to apply
 * @returns A new AgentTool with updated configuration
 */
export function updateAgentToolConfig(
  agentTool: AgentTool,
  newConfig: Record<string, string>
): AgentTool {
  return {
    ...agentTool,
    config: {
      ...agentTool.config,
      ...newConfig,
    },
  };
}