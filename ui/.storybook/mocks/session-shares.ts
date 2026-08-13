import { fn } from "storybook/test";

import type { BaseResponse } from "@/types";

export interface SessionShare {
  token: string;
  session_id: string;
  read_only: boolean;
  created_at: string;
}

export const createSessionShare = fn<
  (sessionId: string, readOnly?: boolean) => Promise<BaseResponse<SessionShare>>
>(async () => ({ message: "Share creation is not configured in this story" }));

export const listSessionShares = fn<
  (sessionId: string) => Promise<BaseResponse<SessionShare[]>>
>(async () => ({ message: "Shares listed", data: [] }));

export const deleteSessionShare = fn<
  (sessionId: string, token: string) => Promise<BaseResponse<void>>
>(async () => ({ message: "Share deleted" }));
