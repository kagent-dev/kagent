"use server";

import { BaseResponse } from "@/lib/types";
import { Team } from "@/types/datamodel";
import { revalidatePath } from "next/cache";
import { fetchApi } from "./utils";
import { AgentFormData } from "@/components/AgentsProvider";

export async function getTeam(teamLabel: string | number): Promise<BaseResponse<Team>> {
  try {
    const data = await fetchApi<Team>(`/teams/${teamLabel}`);
    return { success: true, data };
  } catch (error) {
    console.error("Error getting team:", error);
    return { success: false, error: "Failed to get team. Please try again." };
  }
}

export async function deleteTeam(teamLabel: string) {
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
    console.error("Error deleting team:", error);
    return { success: false, error: "Failed to delete team. Please try again." };
  }
}

interface ResourceMetadata {
  name: string;
  namespace?: string;
}

interface AgentToolList {
  provider: string;
  description: string;
  config: {
    [key: string]: string;
  };
}
interface AgentResourceSpec {
  description: string;
  systemMessage: string;
  tools: AgentToolList[];
}
interface Agent {
  metadata: ResourceMetadata;
  spec: AgentResourceSpec;
}

function fromAgentFormDataToAgent(agentFormData: AgentFormData): Agent {
  // TODO: Fill out the model field once the backend supports it
  return {
    metadata: {
      name: agentFormData.name,
    },
    spec: {
      description: agentFormData.description,
      systemMessage: agentFormData.systemPrompt,
      tools: agentFormData.tools.map((tool) => ({
        provider: tool.provider,
        description: tool.description ?? "No description provided",
        config: Object.entries(tool.config).reduce((acc, [key, value]) => {
          acc[key] = String(value);
          return acc;
        }, {} as { [key: string]: string }),
      })),
    },
  };
}

export async function createAgent(agentConfig: AgentFormData, update: boolean = false): Promise<BaseResponse<Team>> {
  let agentSpec;

  try {
    agentSpec = fromAgentFormDataToAgent(agentConfig);
  } catch (ex) {
    console.error("Error converting agent data:", ex);
    return { success: false, error: "Failed to convert agent data. Please try again." };
  }

  try {
    const response = await fetchApi<Team>(`/teams`, {
      method:  update ? "PUT" : "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(agentSpec),
    });

    if (!response) {
      throw new Error("Failed to create team");
    }

    revalidatePath(`/agents/${response.id}/chat`);
    return { success: true, data: response };
  } catch (error) {
    console.error("Error creating team:", error);
    return { success: false, error: "Failed to create team. Please try again." };
  }
}

export async function getTeams(): Promise<BaseResponse<Team[]>> {
  try {
    const data = await fetchApi<Team[]>(`/teams`);
    const sortedData = data.sort((a, b) => a.component.label?.localeCompare(b.component.label || "") || 0);

    return { success: true, data: sortedData };
  } catch (error) {
    console.error("Error getting teams:", error);
    return { success: false, error: `Failed to get teams. Please try again. ${error}` };
  }
}
