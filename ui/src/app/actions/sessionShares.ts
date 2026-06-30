"use server";

import { BaseResponse } from "@/types";
import { fetchApi, createErrorResponse } from "./utils";

export interface SessionShare {
  token: string;
  session_id: string;
  read_only: boolean;
  created_at: string;
}

/** Creates a share link for the given session (caller must own the session). */
export async function createSessionShare(sessionId: string, readOnly: boolean = true): Promise<BaseResponse<SessionShare>> {
  try {
    const data = await fetchApi<BaseResponse<SessionShare>>(`/sessions/${sessionId}/shares`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ read_only: readOnly }),
    });
    return data;
  } catch (error) {
    return createErrorResponse<SessionShare>(error, "Error creating session share");
  }
}

/** Lists all share links for the given session (caller must own the session). */
export async function listSessionShares(sessionId: string): Promise<BaseResponse<SessionShare[]>> {
  try {
    const data = await fetchApi<BaseResponse<SessionShare[]>>(`/sessions/${sessionId}/shares`);
    return data;
  } catch (error) {
    return createErrorResponse<SessionShare[]>(error, "Error listing session shares");
  }
}

/** Deletes a share link (caller must own the session). */
export async function deleteSessionShare(sessionId: string, token: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/sessions/${sessionId}/shares/${token}`, { method: "DELETE" });
    return { message: "Share deleted" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting session share");
  }
}
