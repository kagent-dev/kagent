import type { Task } from "@a2a-js/sdk";
import { fn } from "storybook/test";

import type { BaseResponse, CreateSessionRequest, Session } from "@/types";

export interface SessionWithEvents {
  session: Session;
  events: unknown[];
  read_only?: boolean | null;
}

export const deleteSession = fn<
  (sessionId: string) => Promise<BaseResponse<void>>
>();
export const getSession = fn<
  (sessionId: string, shareToken?: string) => Promise<BaseResponse<Session>>
>();
export const getSessionsForAgent = fn<
  (namespace: string, agentName: string) => Promise<BaseResponse<Session[]>>
>();
export const createSession = fn<
  (session: CreateSessionRequest) => Promise<BaseResponse<Session>>
>();
export const renameSession = fn<
  (sessionId: string, name: string) => Promise<BaseResponse<Session>>
>();
export const getSessionTasks = fn<
  (sessionId: string, shareToken?: string) => Promise<BaseResponse<Task[]>>
>();
export const getSubagentSessionWithEvents = fn<
  (sessionId: string) => Promise<BaseResponse<{ session: Session; tasks: Task[] }>>
>();
export const getSessionWithEvents = fn<
  (sessionId: string, shareToken?: string) => Promise<BaseResponse<SessionWithEvents>>
>();
export const checkSessionExists = fn<
  (sessionId: string) => Promise<BaseResponse<boolean>>
>();
