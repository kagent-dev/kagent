"use server";

import { BaseResponse } from "@/lib/types";
import { Agent, AgentResponse, Tool, Component } from "@/types/datamodel";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { AgentFormData } from "@/components/AgentsProvider";
import { isMcpTool, isAgentTool } from "@/lib/toolUtils";
import { k8sRefUtils } from "@/lib/k8sUtils";

/**
 * Converts a tool to AgentTool format
 * @param tool The tool to convert
 * @param allAgents List of all available agents to look up descriptions
 * @returns An AgentTool object, potentially augmented with description
 */
function convertToolRepresentation(tool: unknown, allAgents: AgentResponse[]): Tool {
  const typedTool = tool as Partial<Tool>;
  if (isMcpTool(typedTool)) {
    return tool as Tool;
  } else if (isAgentTool(typedTool)) {
    const agentRef = typedTool.agent.ref;
    const foundAgent = allAgents.find(a => {
      const aRef = k8sRefUtils.toRef(
        a.agent.metadata.namespace || "",
        a.agent.metadata.name,
      )
      return aRef === agentRef
    });
    const description = foundAgent?.agent.spec.description;
    return {
      ...typedTool,
      type: "Agent",
      agent: {
        ...typedTool.agent,
        ref: agentRef,
        description: description
      }
    } as Tool;
  }

  throw new Error(`Unknown tool type: ${tool}`);
}

/**
 * Extracts tools from an AgentResponse, augmenting AgentTool references with descriptions.
 * @param data The AgentResponse to extract tools from
 * @param allAgents List of all available agents to look up descriptions
 * @returns An array of Tool objects
 */
function extractToolsFromResponse(data: AgentResponse, allAgents: AgentResponse[]): Tool[] {
  if (data.agent?.spec?.tools) {
    return data.agent.spec.tools.map(tool => convertToolRepresentation(tool, allAgents));
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
      namespace: agentFormData.namespace || "",
    },
    spec: {
      description: agentFormData.description,
      systemMessage: agentFormData.systemPrompt,
      modelConfig: agentFormData.model.ref || "",
      memory: agentFormData.memory,
      tools: agentFormData.tools.map((tool) => {
        if (isMcpTool(tool) && tool.mcpServer) {
          return {
            type: "McpServer",
            mcpServer: {
              toolServer: tool.mcpServer.toolServer,
              toolNames: tool.mcpServer.toolNames,
            },
          } as Tool;
        }

        if (tool.agent) {
          return {
            type: "Agent",
            agent: {
              ref: tool.agent.ref
            },
          } as Tool;
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
    const teamData = await fetchApi<AgentResponse>(`/teams/${teamLabel}`);

    // Fetch all teams to get descriptions for agent tools
    // We use fetchApi directly to avoid circular dependency/logic issues with calling getTeams() here
    const allTeamsData = await fetchApi<AgentResponse[]>(`/teams`);
    
    // Extract and augment tools using the list of all teams
    const tools = extractToolsFromResponse(teamData, allTeamsData);

    const response: AgentResponse = {
      ...teamData,
      agent: {
        ...teamData.agent,
        spec: {
          ...teamData.agent.spec,
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
    const agentPayload = fromAgentFormDataToAgent(agentConfig);
    const response = await fetchApi<Agent>(`/teams`, {
      method: update ? "PUT" : "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(agentPayload),
    });

    if (!response) {
      throw new Error("Failed to create team");
    }

    const agentRef = k8sRefUtils.toRef(
      response.metadata.namespace || "",
      response.metadata.name,
    )

    revalidatePath(`/agents/${agentRef}/chat`);
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
    
    const validTeams = data.filter(team => !!team.agent);
    const agentMap = new Map(validTeams.map(agentResp => [agentResp.agent.metadata.name, agentResp]));

    const convertedData: AgentResponse[] = validTeams.map(team => {
      const augmentedTools = team.tools?.map(tool => {
        // Check if it's an Agent tool reference needing description
        if (isAgentTool(tool)) {
          const agentRef = tool.agent.ref;
          const foundAgent = agentMap.get(agentRef);
          return {
            ...tool,
            type: "Agent",
            agent: {
              ...tool.agent,
              ref: agentRef,
              description: foundAgent?.agent.spec.description
            }
          } as Tool;
        }
        return tool as Tool;
      }) || [];

      return {
        ...team,
        agent: { 
          ...team.agent,
          spec: { 
            ...team.agent.spec,
            tools: augmentedTools
          }
        },
      };
    });

    const sortedData = convertedData.sort((a, b) => {
      const aRef = k8sRefUtils.toRef(a.agent.metadata.namespace || "", a.agent.metadata.name)
      const bRef = k8sRefUtils.toRef(b.agent.metadata.namespace || "", b.agent.metadata.name)

      return aRef.localeCompare(bRef)
    });
    
    return { success: true, data: sortedData };
  } catch (error) {
    return createErrorResponse<AgentResponse[]>(error, "Error getting teams");
  }
}
