import { fn } from "storybook/test";

import type { AgentFormData } from "@/lib/agentFormDomain";
import type { Agent, AgentResponse, BaseResponse } from "@/types";

export const getAgent = fn<
  (
    agentName: string,
    namespace: string,
    kubernetesKind?: string,
  ) => Promise<BaseResponse<AgentResponse>>
>(async () => ({ message: "Agent not found" }));

export const getAgentWithResolvedKind = fn<
  (agentName: string, namespace: string) => Promise<BaseResponse<AgentResponse>>
>(async () => ({ message: "Agent not found" }));

export const waitForSandboxAgentReady = fn<
  (
    agentName: string,
    namespace: string,
    options?: { timeoutMs?: number; intervalMs?: number },
  ) => Promise<{ ok: boolean; error?: string }>
>(async () => ({ ok: true }));

export const deleteAgent = fn<
  (
    agentName: string,
    namespace: string,
    kubernetesKind?: string,
  ) => Promise<BaseResponse<void>>
>(async () => ({ message: "Agent deleted" }));

export const createAgent = fn<
  (agentConfig: AgentFormData, update?: boolean) => Promise<BaseResponse<Agent>>
>(async () => ({ message: "Agent saved" }));

export const getAgents = fn<
  (options?: { namespace?: string }) => Promise<BaseResponse<AgentResponse[]>>
>(async () => ({ message: "Agents fetched", data: [] }));
