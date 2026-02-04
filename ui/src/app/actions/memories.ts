"use server";

import { fetchApi } from "./utils";

export async function clearAgentMemory(agentName: string) {
  try {
    const data = await fetchApi<any>(
      `/api/memories?agent_name=${encodeURIComponent(agentName)}`,
      { method: "DELETE" },
    );
    return { data, error: null };
  } catch (error) {
    return { data: null, error };
  }
}
