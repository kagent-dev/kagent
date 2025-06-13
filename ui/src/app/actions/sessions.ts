"use server";

import { BaseResponse, CreateSessionRequest } from "@/lib/types";
import {  Session, AgentMessageConfig } from "@/types/datamodel";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";

/**
 * Deletes a session
 * @param sessionId The session ID
 * @returns A promise with the delete result
 */
export async function deleteSession(sessionId: number): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/sessions/${sessionId}`, {
      method: "DELETE",
    });

    revalidatePath("/");
    return { success: true };
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
    return { success: true, data };
  } catch (error) {
    return createErrorResponse<Session>(error, "Error getting session");
  }
}

/**
 * Gets all sessions
 * @returns A promise with all sessions
 */
export async function getSessions(agentId: number): Promise<BaseResponse<Session[]>> {
  try {
    const data = await fetchApi<Session[]>(`/sessions`);
    const filteredSessions = data.filter((session) => Number(session.team_id) === Number(agentId));
    return { success: true, data: filteredSessions };
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
    const response = await fetchApi<Session>(`/sessions`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        user_id: session.user_id,
        team_id: session.team_id ? Number(session.team_id) : undefined,
        name: session.name,
      }),
    });

    if (!response) {
      throw new Error("Failed to create session");
    }

    return { success: true, data: response };
  } catch (error) {
    return createErrorResponse<Session>(error, "Error creating session");
  }
}

/**
 * Gets all messages for a session
 * @param sessionId The session ID
 * @returns A promise with the session messages
 */
export async function getSessionMessages(sessionId: string): Promise<BaseResponse<AgentMessageConfig[]>> {
  try {
    const data = await fetchApi<AgentMessageConfig[]>(`/sessions/${sessionId}/messages`);
    return { success: true, data };
  } catch (error) {
    return createErrorResponse<AgentMessageConfig[]>(error, "Error getting session messages");
  }
}

/**
 * Check if a session exists
 * @param sessionId The session ID to check
 * @returns A promise with boolean indicating if session exists
 */
export async function checkSessionExists(sessionId: string): Promise<BaseResponse<boolean>> {
  try {
    const response = await fetchApi<Session>(`/sessions/${sessionId}`);
    return { success: true, data: !!response };
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } catch (error: any) {
    // If we get a 404, return success: true but data: false
    if (error?.status === 404) {
      return { success: true, data: false };
    }
    return createErrorResponse<boolean>(error, "Error checking session");
  }
}

/**
 * Updates a session
 * @param session The session to update
 * @returns A promise with the updated session
 */
export async function updateSession(session: Session): Promise<BaseResponse<Session>> {
  try {
    const sessionToUpdate = {
      ...session,
      team_id: Number(session.team_id)
    };

    const response = await fetchApi<Session>(`/sessions/${session.id}`, {
      method: "PUT",
      body: JSON.stringify(sessionToUpdate),
    });

    if (!response) {
      throw new Error("Failed to update session");
    }

    revalidatePath("/");
    return { success: true, data: response };
  } catch (error) {
    return createErrorResponse<Session>(error, "Error updating session");
  }
}
