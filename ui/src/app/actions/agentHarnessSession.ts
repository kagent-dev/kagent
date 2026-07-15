"use server";

import { BaseResponse } from "@/types";
import { fetchApi, createErrorResponse } from "./utils";

export interface AgentHarnessSessionActor {
  namespace: string;
  name: string;
  sessionId: string;
  actorId?: string;
  state?: AgentHarnessSessionState;
}

/** Lifecycle state of a chat session's substrate actor. */
export type AgentHarnessSessionState = "running" | "suspended" | "missing";

/**
 * Provisions (creates + resumes) the substrate actor for a single AgentHarness
 * chat session. Called when a new chat is started so the actor is warm before
 * the /acp WebSocket connects.
 */
export async function ensureAgentHarnessSession(
  namespace: string,
  name: string,
  sessionId: string
): Promise<BaseResponse<AgentHarnessSessionActor>> {
  try {
    const response = await fetchApi<BaseResponse<AgentHarnessSessionActor>>(
      `/agentharnesses/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/sessions/${encodeURIComponent(
        sessionId
      )}/ensure`,
      { method: "POST" }
    );
    return { message: "Session actor ready", data: response.data };
  } catch (error) {
    return createErrorResponse<AgentHarnessSessionActor>(error, "Error provisioning session actor");
  }
}

/**
 * Checkpoints and frees the substrate actor for a single AgentHarness chat
 * session. The actor is resumed automatically on the next /acp connection.
 */
export async function suspendAgentHarnessSession(
  namespace: string,
  name: string,
  sessionId: string
): Promise<BaseResponse<AgentHarnessSessionActor>> {
  try {
    const response = await fetchApi<BaseResponse<AgentHarnessSessionActor>>(
      `/agentharnesses/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/sessions/${encodeURIComponent(
        sessionId
      )}/suspend`,
      { method: "POST" }
    );
    return { message: "Session actor suspended", data: response.data };
  } catch (error) {
    return createErrorResponse<AgentHarnessSessionActor>(error, "Error suspending session actor");
  }
}

/**
 * Returns the lifecycle state ("running", "suspended", "missing") of a single
 * AgentHarness chat session's substrate actor.
 */
export async function getAgentHarnessSessionStatus(
  namespace: string,
  name: string,
  sessionId: string
): Promise<BaseResponse<AgentHarnessSessionActor>> {
  try {
    const response = await fetchApi<BaseResponse<AgentHarnessSessionActor>>(
      `/agentharnesses/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/sessions/${encodeURIComponent(
        sessionId
      )}/status`
    );
    return { message: "Session actor state", data: response.data };
  } catch (error) {
    return createErrorResponse<AgentHarnessSessionActor>(error, "Error reading session actor state");
  }
}
