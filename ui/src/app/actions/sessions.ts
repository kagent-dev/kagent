"use server";

import { BaseResponse, CreateSessionRequest } from "@/types";
import { Session } from "@/types";
import { getSessionGrpcGateway } from "@/lib/grpc/client";
import { revalidatePath } from "next/cache";
import { createErrorResponse } from "./utils";
import { Task } from "@a2a-js/sdk";

export interface SessionWithEvents {
  session: Session;
  events: unknown[];
  read_only?: boolean | null;
}

/**
 * Deletes a session
 * @param sessionId The session ID
 * @returns A promise with the delete result
 */
export async function deleteSession(sessionId: string): Promise<BaseResponse<void>> {
  try {
    const gateway = await getSessionGrpcGateway();
    await gateway.deleteSession(sessionId);

    revalidatePath("/");
    return { message: "Session deleted successfully" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting session");
  }
}

/**
 * Gets a session by ID
 * @param sessionId The session ID
 * @param shareToken Optional X-Share-Token for accessing another user's shared session
 * @returns A promise with the session data
 */
export async function getSession(sessionId: string, shareToken?: string): Promise<BaseResponse<Session>> {
  try {
    const gateway = await getSessionGrpcGateway();
    const session = await gateway.getSession(sessionId, shareToken);
    return { message: "Session fetched successfully", data: session };
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
    const gateway = await getSessionGrpcGateway();
    const sessions = await gateway.listSessionsByAgent(namespace, agentName);
    return { message: "Sessions fetched successfully", data: sessions };
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
    const gateway = await getSessionGrpcGateway();
    const created = await gateway.createSession(session);
    return { message: "Session created successfully", data: created };
  } catch (error) {
    return createErrorResponse<Session>(error, "Error creating session");
  }
}

/**
 * Renames a session (sets its display name).
 * @param sessionId The session ID
 * @param name The new display name
 * @returns A promise with the updated session
 */
export async function renameSession(sessionId: string, name: string): Promise<BaseResponse<Session>> {
  try {
    const gateway = await getSessionGrpcGateway();
    const session = await gateway.renameSession(sessionId, name);
    return { message: "Session renamed successfully", data: session };
  } catch (error) {
    return createErrorResponse<Session>(error, "Error renaming session");
  }
}

/**
 * Gets all messages for a session
 * @param sessionId The session ID
 * @param shareToken Optional X-Share-Token for accessing another user's shared session
 * @returns A promise with the session messages
 */
export async function getSessionTasks(sessionId: string, shareToken?: string): Promise<BaseResponse<Task[]>> {
  try {
    const gateway = await getSessionGrpcGateway();
    const tasks = await gateway.listTasks(sessionId, shareToken);
    return { message: "Session tasks fetched successfully", data: tasks };
  } catch (error) {
    return createErrorResponse<Task[]>(error, "Error getting session tasks");
  }
}

/**
 * Gets a session together with its tasks (events) in a single call.
 * @param sessionId The subagent session ID
 * @returns A promise with { session, tasks }
 */
export async function getSubagentSessionWithEvents(
  sessionId: string
): Promise<BaseResponse<{ session: Session; tasks: Task[] }>> {
  try {
    const gateway = await getSessionGrpcGateway();
    const [session, tasks] = await Promise.all([
      gateway.getSession(sessionId),
      gateway.listTasks(sessionId),
    ]);
    return {
      message: "Session with events fetched successfully",
      data: { session, tasks },
    };
  } catch (error) {
    return createErrorResponse<{ session: Session; tasks: Task[] }>(error, "Error fetching session with events");
  }
}

/**
 * Gets a session with its events, optionally using a share token.
 * @param sessionId The session ID
 * @param shareToken Optional X-Share-Token for accessing a shared session
 */
export async function getSessionWithEvents(sessionId: string, shareToken?: string): Promise<BaseResponse<SessionWithEvents>> {
  try {
    const gateway = await getSessionGrpcGateway();
    const result = await gateway.getSessionWithEvents(sessionId, shareToken);
    return { message: "Session fetched successfully", data: result };
  } catch (error) {
    return createErrorResponse<SessionWithEvents>(error, "Error getting session");
  }
}

/**
 * Check if a session exists
 * @param sessionId The session ID to check
 * @returns A promise with boolean indicating if session exists
 */
export async function checkSessionExists(sessionId: string): Promise<BaseResponse<boolean>> {
  try {
    const gateway = await getSessionGrpcGateway();
    await gateway.getSession(sessionId);
    return { message: "Session exists successfully", data: true };
  } catch (error: unknown) {
    // If we get a 404, return success: true but data: false
    if (typeof error === "object" && error !== null && "status" in error && (error as { status: unknown }).status === 404) {
      return { message: "Session does not exist", data: false };
    }
    return createErrorResponse<boolean>(error, "Error checking session");
  }
}
