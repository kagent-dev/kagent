"use server";

import { AgentMemory } from "@/types";
import { fetchApi } from "./utils";

export async function clearAgentMemory(agentName: string, namespace?: string, userId?: string) {
  try {
    const fullName = namespace ? `${namespace}__NS__${agentName}` : agentName;
    const params = new URLSearchParams({ agent_name: fullName });
    if (userId) params.set("user_id", userId);
    const data = await fetchApi<unknown>(
      `/memories?${params.toString()}`,
      { method: "DELETE" },
    );
    return { data, error: null };
  } catch (error) {
    return { data: null, error };
  }
}

export async function listAgentMemories(agentName: string, namespace?: string, userId?: string) {
  try {
    const fullName = namespace ? `${namespace}__NS__${agentName}` : agentName;
    const params = new URLSearchParams({ agent_name: fullName });
    if (userId) params.set("user_id", userId);
    const data = await fetchApi<AgentMemory[]>(
      `/memories?${params.toString()}`,
      { method: "GET" },
    );
    return { data, error: null };
  } catch (error) {
    return { data: null, error };
  }
}
