// Types for the Agent Client Protocol (ACP) harness chat transport. Shared by
// the useAcpHarnessChat hook, its pure helpers (lib/acp.ts), and the chat UI.

/** Connection / ACP-handshake lifecycle state for an AcpHarnessChat session. */
export type ConnState =
  | "idle"
  | "connecting"
  | "initializing"
  | "creating-session"
  | "loading-session"
  | "ready"
  | "running"
  | "disconnected";

/** A JSON-RPC 2.0 envelope as exchanged over the ACP WebSocket. */
export type JsonRpcMessage = {
  jsonrpc?: string;
  id?: number | string;
  method?: string;
  params?: Record<string, unknown>;
  result?: Record<string, unknown>;
  error?: { code: number; message: string; data?: unknown };
};

/** Payload of an ACP `session/update` notification. */
export type SessionUpdate = {
  sessionUpdate?: string;
  content?: unknown;
  toolCallId?: string;
  title?: string;
  kind?: string;
  status?: string;
  rawInput?: Record<string, unknown>;
  rawOutput?: unknown;
  entries?: { content?: string; status?: string }[];
};

/** One option offered by an agent `session/request_permission` request. */
export type PermissionOption = { optionId?: string; name?: string; kind?: string };

/** SessionInfo entries returned by ACP `session/list`. */
export type AcpSessionInfo = { sessionId: string; title?: string; updatedAt?: string };
