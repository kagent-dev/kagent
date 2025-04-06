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
        toolServer: component.provider,
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
  return availableTools.find((tool) =>  getToolIdentifier(tool) === getToolIdentifier(agentTool));
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

/**
 * Extracts all tools from any agent within the society_of_mind_agent in the provided JSON
 * @param agentResponse - The agent response data
 * @param societyOfMindAgentLabel - Optional label for the society of mind agent (defaults to "society_of_mind_agent")
 * @returns Array of AgentTool objects or undefined if not found
 */
export function extractSocietyOfMindAgentTools(
  agentResponse: AgentResponse, 
  societyOfMindAgentLabel: string = "society_of_mind_agent"
): AgentTool[] | undefined {
  try {
    // Get the component from the response
    const component = agentResponse.component;
    
    // Check if the component config is a RoundRobinGroupChatConfig or SelectorGroupChatConfig
    if (!isRoundRobinGroupChatConfig(component.config) && !isSelectorGroupChatConfig(component.config)) {
      console.error("Component config does not have participants");
      return undefined;
    }
    
    // Find the society_of_mind_agent participant
    const societyOfMindAgent = component.config.participants.find(
      (participant) => participant.label === societyOfMindAgentLabel
    );
    
    if (!societyOfMindAgent || !societyOfMindAgent.config) {
      console.error(`Could not find agent with label: ${societyOfMindAgentLabel}`);
      return undefined;
    }
    
    // Ensure the agent has a team property with a valid config
    if (!('team' in societyOfMindAgent.config)) {
      console.error(`Agent with label ${societyOfMindAgentLabel} does not have a team configuration`);
      return undefined;
    }
    
    const team = societyOfMindAgent.config.team;
    
    // Check if the team config is a RoundRobinGroupChatConfig or SelectorGroupChatConfig
    if (!isRoundRobinGroupChatConfig(team.config) && !isSelectorGroupChatConfig(team.config)) {
      console.error("Team config does not have participants");
      return undefined;
    }
    
    // Find all agents in the team that have tools
    const agentsWithTools = team.config.participants.filter(
      (participant) => {
        if (participant.component_type !== "agent" || !participant.config) {
          return false;
        }
        
        return isAssistantAgentConfig(participant.config) && participant.config.tools && participant.config.tools.length > 0;
      }
    );
    
    if (agentsWithTools.length === 0) {
      console.error("No agents with tools found in the team configuration");
      return undefined;
    }
    
    // If we're looking for tools from all agents, we can combine them
    const allTools: AgentTool[] = [];
    
    agentsWithTools.forEach(agent => {
      if (isAssistantAgentConfig(agent.config) && agent.config.tools) {
        // Convert Component<ToolConfig> to AgentTool
        const agentTools = agent.config.tools.map(tool => {
          const toolType = getToolProvider(tool)
          return {
            type: toolType === "autogen_ext.tools.mcp.SseMcpToolAdapter" ? "McpServer" : "Inline",
            provider: tool.provider,
            description: tool.description || "",
            config: tool.config ? JSON.parse(JSON.stringify(tool.config)) : {},
          } as AgentTool
        });
        
        allTools.push(...agentTools);
      }
    });
    
    return allTools.length > 0 ? allTools : undefined;
  } catch (error) {
    console.error("Error extracting tools:", error);
    return undefined;
  }
}