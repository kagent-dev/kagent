"use server";

import { BaseResponse, CreateSessionRequest } from "@/lib/types";
import { Run, Session } from "@/types/datamodel";
import { revalidatePath } from "next/cache";
import { fetchApi } from "./utils";

export async function getSessionRuns(sessionId: string): Promise<BaseResponse<Run[]>> {
  try {
    const data = await fetchApi<Run[]>(`/sessions/${sessionId}/runs`);
    return { success: true, data };
  } catch (error) {
    console.error("Error getting session runs:", error);
    return { success: false, error: "Failed to get session runs. Please try again." };
  }
}
export async function deleteSession(sessionId: number) {
  try {
    await fetchApi(`/sessions/${sessionId}`, {
      method: "DELETE",
      headers: {
        "Content-Type": "application/json",
      },
    });

    return { success: true };
  } catch (error) {
    console.error("Error deleting session:", error);
    return { success: false, error: "Failed to delete session. Please try again." };
  }
}

export async function getSession(sessionId: string): Promise<BaseResponse<Session>> {
  try {
    const data = await fetchApi<Session>(`/sessions/${sessionId}`);
    return { success: true, data };
  } catch (error) {
    console.error("Error getting session:", error);
    return { success: false, error: "Failed to get session. Please try again." };
  }
}

export async function getSessions(): Promise<BaseResponse<Session[]>> {
  try {
    const data = await fetchApi<Session[]>(`/sessions`);
    return { success: true, data };
  } catch (error) {
    console.error("Error getting sessions:", error);
    return { success: false, error: "Failed to get sessions. Please try again." };
  }
}

export async function createSession(session: CreateSessionRequest): Promise<BaseResponse<Session>> {
  try {
    const response = await fetchApi<Session>(`/sessions`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        user_id: session.userId,
        team_id: session.teamId,
      }),
    });

    if (!response) {
      throw new Error("Failed to create session");
    }

    revalidatePath(`/agents/${response.id}/chat`);
    return { success: true, data: response };
  } catch (error) {
    console.error("Error creating team:", error);
    return { success: false, error: "Failed to create team. Please try again." };
  }
}
