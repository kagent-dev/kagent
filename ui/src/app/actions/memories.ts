"use server";

import { fetchApi } from "./utils";

export async function clearAgentMemory(agentName: string, namespace?: string) {
  try {
    const fullName = namespace ? `${namespace}__NS__${agentName}` : agentName;
    const data = await fetchApi<unknown>(
      `/memories?agent_name=${encodeURIComponent(fullName)}`,
      { method: "DELETE" },
    );
    return { data, error: null };
  } catch (error) {
    return { data: null, error };
  }
}
