"use server";

import { AgentMemory } from "@/types";
import { getMemoryGrpcGateway } from "@/lib/grpc/client";

const DEFAULT_USER_ID = "admin@kagent.dev";

export async function clearAgentMemory(agentName: string, namespace?: string, userId?: string) {
  try {
    const fullName = namespace ? `${namespace}__NS__${agentName}` : agentName;
    const gateway = await getMemoryGrpcGateway();
    await gateway.clearAgentMemory(fullName, userId ?? DEFAULT_USER_ID);
    return { data: { status: "deleted" }, error: null };
  } catch (error) {
    return { data: null, error };
  }
}

export async function listAgentMemories(agentName: string, namespace?: string, userId?: string) {
  try {
    const fullName = namespace ? `${namespace}__NS__${agentName}` : agentName;
    const gateway = await getMemoryGrpcGateway();
    const data: AgentMemory[] = await gateway.listAgentMemories(fullName, userId ?? DEFAULT_USER_ID);
    return { data, error: null };
  } catch (error) {
    return { data: null, error };
  }
}
