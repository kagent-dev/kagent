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

/**
 * Updates a session
 * @param session The session to update
 * @returns A promise with the updated session
 */
export async function updateSession(session: Session): Promise<BaseResponse<Session>> {
  try {
    const sessionToUpdate = {
      ...session,
      agent_id: Number(session.agent_id)
    };

    const response = await fetchApi<BaseResponse<Session>>(`/sessions/${session.id}`, {
      method: "PUT",
      body: JSON.stringify(sessionToUpdate),
    });

    if (!response) {
      throw new Error("Failed to update session");
    }

    revalidatePath("/");
    return { message: "Session updated successfully", data: response.data };
  } catch (error) {
    return createErrorResponse<Session>(error, "Error updating session");
  }
}
