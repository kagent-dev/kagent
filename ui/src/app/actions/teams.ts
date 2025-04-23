"use server";

import { BaseResponse } from "@/lib/types";
import { Agent, AgentResponse, AgentTool, Component } from "@/types/datamodel";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { AgentFormData } from "@/components/AgentsProvider";
import { isInlineTool, isMcpTool } from "@/lib/toolUtils";

/**
 * Converts a tool to AgentTool format
 * @param tool The tool to convert
 * @returns An AgentTool object
 */
function convertToAgentTool(tool: unknown): AgentTool {
  // Check if the tool is already in AgentTool format
  if (tool && typeof tool === 'object' && 'type' in tool) {
    const typedTool = tool as Partial<AgentTool>;
    if (typedTool.type === "Inline" && typedTool.inline) {
      return tool as AgentTool;
    }
    if (typedTool.type === "McpServer" && typedTool.mcpServer) {
      return tool as AgentTool;
    }
  }

  // Check if it's a Component<ToolConfig>
  if (tool && typeof tool === 'object' && 'provider' in tool) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const componentTool = tool as Component<any>;
    return {
      type: "Inline",
      inline: {
        provider: componentTool.provider,
        description: componentTool.description || "",
        config: componentTool.config || {},
        label: componentTool.label,
      }
    } as AgentTool;
  }

  // Default case - shouldn't happen with proper type checking
  console.warn("Unknown tool format:", tool);
  return {
    type: "Inline",
    inline: {
      provider: "unknown",
      description: "Unknown tool",
      config: {},
    }
  } as AgentTool;
}

/**
 * Extracts tools from an AgentResponse
 * @param data The AgentResponse to extract tools from
 * @returns An array of AgentTool objects
 */
function extractToolsFromResponse(data: AgentResponse): AgentTool[] {
  // First try to get tools from the agent property
  if (data.agent?.spec?.tools) {
    return data.agent.spec.tools.map(convertToAgentTool);
  }
  return [];
}

/**
 * Processes a config object, converting all values to strings
 * @param config The config object to process
 * @returns A new object with all values as strings
 */
function processConfigObject(config: Record<string, unknown>): Record<string, string> {
  return Object.entries(config).reduce((acc, [key, value]) => {
    // If value is an object and not null, process it recursively
    if (typeof value === "object" && value !== null) {
      acc[key] = JSON.stringify(processConfigObject(value as Record<string, unknown>));
    } else {
      // For primitive values, convert to string
      acc[key] = String(value);
    }
    return acc;
  }, {} as Record<string, string>);
}

/**
 * Converts AgentFormData to Agent format
 * @param agentFormData The form data to convert
 * @returns An Agent object
 */
function fromAgentFormDataToAgent(agentFormData: AgentFormData): Agent {
  return {
    metadata: {
      name: agentFormData.name,
    },
    spec: {
      description: agentFormData.description,
      systemMessage: agentFormData.systemPrompt,
      modelConfigRef: agentFormData.model.name || "",
      tools: agentFormData.tools.map((tool) => {
        // Convert to the proper Tool structure based on the tool type
        if (isInlineTool(tool) && tool.inline) {
          return {
            type: "Inline",
            inline: {
              provider: tool.inline.provider,
              config: tool.inline.config ? processConfigObject(tool.inline.config) : {},
              label: tool.inline.label,
            },
          } as AgentTool;
        }
        
        if (isMcpTool(tool) && tool.mcpServer) {
          return {
            type: "McpServer",
            mcpServer: {
              toolServer: tool.mcpServer.toolServer,
              toolNames: tool.mcpServer.toolNames,
            },
          } as AgentTool;
        }
        
        // Default case - shouldn't happen with proper type checking
        console.warn("Unknown tool type:", tool);
        return tool;
      }),
    },
  };
}

/**
 * Gets a team by label or ID
 * @param teamLabel The team label or ID
 * @returns A promise with the team data
 */
export async function getTeam(teamLabel: string | number): Promise<BaseResponse<AgentResponse>> {
  try {
    const data = await fetchApi<AgentResponse>(`/teams/${teamLabel}`);
    const tools = extractToolsFromResponse(data);

    const response: AgentResponse = {
      ...data,
      agent: {
        ...data.agent,
        spec: {
          ...data.agent.spec,
          tools,
        },
      },
    };

    return { success: true, data: response };
  } catch (error) {
    return createErrorResponse<AgentResponse>(error, "Error getting team");
  }
}

/**
 * Deletes a team
 * @param teamLabel The team label
 * @returns A promise with the delete result
 */
export async function deleteTeam(teamLabel: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/teams/${teamLabel}`, {
      method: "DELETE",
      headers: {
        "Content-Type": "application/json",
      },
    });

    revalidatePath("/");
    return { success: true };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting team");
  }
}

/**
 * Creates or updates an agent
 * @param agentConfig The agent configuration
 * @param update Whether to update an existing agent
 * @returns A promise with the created/updated agent
 */
export async function createAgent(agentConfig: AgentFormData, update: boolean = false): Promise<BaseResponse<Agent>> {
  try {
    const agentSpec = fromAgentFormDataToAgent(agentConfig);
    const response = await fetchApi<Agent>(`/teams`, {
      method: update ? "PUT" : "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(agentSpec),
    });

    if (!response) {
      throw new Error("Failed to create team");
    }

    revalidatePath(`/agents/${response.metadata.name}/chat`);
    return { success: true, data: response };
  } catch (error) {
    return createErrorResponse<Agent>(error, "Error creating team");
  }
}

/**
 * Gets all teams
 * @returns A promise with all teams
 */
export async function getTeams(): Promise<BaseResponse<AgentResponse[]>> {
  try {
    const data = await fetchApi<AgentResponse[]>(`/teams`);
    
    // Convert each team's tools to AgentTool format
    const convertedData: AgentResponse[] = data.map(team => ({
      ...team,
      tools: extractToolsFromResponse(team),
    }));

    const sortedData = convertedData.sort((a, b) => 
      a.agent.metadata.name.localeCompare(b.agent.metadata.name)
    );
    
    return { success: true, data: sortedData };
  } catch (error) {
    return createErrorResponse<AgentResponse[]>(error, "Error getting teams");
  }
}
