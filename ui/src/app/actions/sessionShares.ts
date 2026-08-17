"use server";

import { BaseResponse } from "@/types";
import { getSessionGrpcGateway } from "@/lib/grpc/client";
import { createErrorResponse } from "./utils";

export interface SessionShare {
  token: string;
  session_id: string;
  read_only: boolean;
  created_at: string;
}

/** Creates a share link for the given session (caller must own the session). */
export async function createSessionShare(sessionId: string, readOnly: boolean = true): Promise<BaseResponse<SessionShare>> {
  try {
    const gateway = await getSessionGrpcGateway();
    const share = await gateway.createSessionShare(sessionId, readOnly);
    return { message: "Share created", data: share };
  } catch (error) {
    return createErrorResponse<SessionShare>(error, "Error creating session share");
  }
}

/** Lists all share links for the given session (caller must own the session). */
export async function listSessionShares(sessionId: string): Promise<BaseResponse<SessionShare[]>> {
  try {
    const gateway = await getSessionGrpcGateway();
    const shares = await gateway.listSessionShares(sessionId);
    return { message: "Shares listed", data: shares };
  } catch (error) {
    return createErrorResponse<SessionShare[]>(error, "Error listing session shares");
  }
}

/** Deletes a share link (caller must own the session). */
export async function deleteSessionShare(sessionId: string, token: string): Promise<BaseResponse<void>> {
  try {
    const gateway = await getSessionGrpcGateway();
    await gateway.deleteSessionShare(sessionId, token);
    return { message: "Share deleted" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting session share");
  }
}
