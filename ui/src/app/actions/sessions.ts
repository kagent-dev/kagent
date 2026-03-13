"use server";

import { BaseResponse, CreateSessionRequest } from "@/types";
import { Session } from "@/types";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { Task } from "@a2a-js/sdk";

/**
 * Deletes a session
 * @param sessionId The session ID
 * @returns A promise with the delete result
 */
export async function deleteSession(sessionId: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/sessions/${sessionId}`, {
      method: "DELETE",
    });

    revalidatePath("/");
    return { message: "Session deleted successfully" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting session");
  }
}

/**
 * Gets a session by ID
 * @param sessionId The session ID
 * @returns A promise with the session data
 */
export async function getSession(sessionId: string): Promise<BaseResponse<Session>> {
  try {
    const data = await fetchApi<Session>(`/sessions/${sessionId}`);
    return { message: "Session fetched successfully", data };
  } catch (error) {
    return createErrorResponse<Session>(error, "Error getting session");
  }
}

/**
 * Gets all sessions
 * @returns A promise with all sessions
 */
export async function getSessionsForAgent(namespace: string, agentName: string): Promise<BaseResponse<Session[]>> {
  try {
    const data = await fetchApi<BaseResponse<Session[]>> (`/sessions/agent/${namespace}/${agentName}`);
    return { message: "Sessions fetched successfully", data: data.data || [] };
  } catch (error) {
    return createErrorResponse<Session[]>(error, "Error getting sessions");
  }
}

/**
 * Creates a new session
 * @param session The session creation request
 * @returns A promise with the created session
 */
export async function createSession(session: CreateSessionRequest): Promise<BaseResponse<Session>> {
  try {
    const response = await fetchApi<BaseResponse<Session>>(`/sessions`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(session),
    });

    if (!response) {
      throw new Error("Failed to create session");
    }

    return { message: "Session created successfully", data: response.data };
  } catch (error) {
    return createErrorResponse<Session>(error, "Error creating session");
  }
}

/**
 * Gets all messages for a session
 * @param sessionId The session ID
 * @returns A promise with the session messages
 */
export async function getSessionTasks(sessionId: string): Promise<BaseResponse<Task[]>> {
  try {
    const data = await fetchApi<BaseResponse<Task[]>>(`/sessions/${sessionId}/tasks`);
    return data;
  } catch (error) {
    return createErrorResponse<Task[]>(error, "Error getting session tasks");
  }
}

/**
 * Gets a session together with its tasks (events) in a single call.
 * Used by the subagent activity viewer to display a read-only history of what
 * the subagent did without navigating away from the parent chat.
 * @param sessionId The subagent session ID
 * @returns A promise with { session, tasks }
 */
export async function getSubagentSessionWithEvents(
  sessionId: string
): Promise<BaseResponse<{ session: Session; tasks: Task[] }>> {
  try {
    // fetchApi appends user_id=admin@kagent.dev automatically.
    // With the x-user-id passthrough fix, subagent sessions are now
    // created under the same user as the parent, so a single fetch works.
    const [sessionResp, tasksResp] = await Promise.all([
      fetchApi<BaseResponse<{ session: Session; events: unknown[] }>>(`/sessions/${sessionId}`),
      fetchApi<BaseResponse<Task[]>>(`/sessions/${sessionId}/tasks`),
    ]);

    const session = sessionResp.data?.session;
    if (!session) {
      return { message: "Subagent session not found" };
    }
    return {
      message: "Session with events fetched successfully",
      data: { session, tasks: tasksResp.data ?? [] },
    };
  } catch (error) {
    return createErrorResponse<{ session: Session; tasks: Task[] }>(error, "Error fetching session with events");
  }
}

/**
 * Check if a session exists
 * @param sessionId The session ID to check
 * @returns A promise with boolean indicating if session exists
 */
export async function checkSessionExists(sessionId: string): Promise<BaseResponse<boolean>> {
  try {
    const response = await fetchApi<BaseResponse<Session>>(`/sessions/${sessionId}`);
    return { message: "Session exists successfully", data: !!response.data };
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } catch (error: any) {
    // If we get a 404, return success: true but data: false
    if (error?.status === 404) {
      return { message: "Session does not exist", data: false };
    }
    return createErrorResponse<boolean>(error, "Error checking session");
  }
}
