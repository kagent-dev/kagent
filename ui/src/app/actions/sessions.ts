"use server";

import { BaseResponse, CreateSessionRequest } from "@/lib/types";
import { Run, Session } from "@/types/datamodel";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";

/**
 * Gets all runs for a session
 * @param sessionId The session ID
 * @returns A promise with the session runs
 */
export async function getSessionRuns(sessionId: string): Promise<BaseResponse<Run[]>> {
  try {
    const data = await fetchApi<Run[]>(`/sessions/${sessionId}/runs`);
    return { success: true, data };
  } catch (error) {
    return createErrorResponse<Run[]>(error, "Error getting session runs");
  }
}

/**
 * Deletes a session
 * @param sessionId The session ID
 * @returns A promise with the delete result
 */
export async function deleteSession(sessionId: number): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/sessions/${sessionId}`, {
      method: "DELETE",
      headers: {
        "Content-Type": "application/json",
      },
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
export async function getSessions(): Promise<BaseResponse<Session[]>> {
  try {
    const data = await fetchApi<Session[]>(`/sessions`);
    return { success: true, data };
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
        team_id: Number(session.team_id),
      }),
    });

    if (!response) {
      throw new Error("Failed to create session");
    }

    revalidatePath(`/agents/${response.team_id}/chat`);
    return { success: true, data: response };
  } catch (error) {
    return createErrorResponse<Session>(error, "Error creating session");
  }
}
