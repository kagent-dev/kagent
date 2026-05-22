import { http, HttpResponse, delay } from "msw";
import type { Session } from "@/types";
import type { Task } from "@a2a-js/sdk";
import type { TaskState } from "@a2a-js/sdk";

/**
 * The backend URL that fetchApi constructs requests against.
 * In development / Storybook this resolves to localhost.
 */
const BACKEND_URL = "http://localhost:8083/api";

// ---------------------------------------------------------------------------
// Mock data factories
// ---------------------------------------------------------------------------

export function createMockSession(overrides: Partial<Session> = {}): Session {
  return {
    id: "session-123",
    name: "Test conversation",
    agent_id: 'kagent__NS__k8s',
    user_id: "admin@kagent.dev",
    created_at: "2026-03-07T10:00:00Z",
    updated_at: "2026-03-07T10:05:00Z",
    deleted_at: "",
    ...overrides,
  };
}

/**
 * Creates a minimal A2A Task object whose `history` array contains plain
 * user/agent Message entries.  This is the shape returned by
 * `GET /sessions/:id/tasks` and consumed by `extractMessagesFromTasks`.
 */
export function createMockTask(
  taskId: string,
  contextId: string,
  history: Array<{
    role: "user" | "agent";
    text: string;
    messageId?: string;
    metadata?: Record<string, unknown>;
  }>,
  status: { state: TaskState } = { state: 3 },
): Task {
  return {
    id: taskId,
    contextId,
    status,
    history: history.map((h, i) => ({
      messageId: h.messageId ?? `${taskId}-msg-${i}`,
      role: h.role === "user" ? 1 : 2,
      parts: [{
        content: { $case: "text", value: h.text },
        metadata: {},
        filename: "",
        mediaType: "text/plain",
      }],
      contextId,
      taskId,
      metadata: {
        displaySource: h.role === "agent" ? "assistant" : undefined,
        timestamp: Date.now() - (history.length - i) * 60_000,
        ...h.metadata,
      },
      extensions: [],
      referenceTaskIds: [],
    })),
    artifacts: [],
    metadata: undefined,
  } as unknown as Task;
}

/**
 * Creates a mock task containing a tool-call request and its execution
 * result, matching the ADK metadata shape that `extractMessagesFromTasks`
 * and `ChatMessage` understand.
 */
export function createMockToolCallTask(
  taskId: string,
  contextId: string,
  toolName: string,
  toolArgs: Record<string, unknown>,
  toolResult: string,
): Task {
  return {
    id: taskId,
    contextId,
    status: { state: 3 },
    history: [
      // User message that triggered the tool call
      {
        messageId: `${taskId}-user`,
        role: 1,
        parts: [{
          content: { $case: "text", value: "Run the tool" },
          metadata: {},
          filename: "",
          mediaType: "text/plain",
        }],
        contextId,
        taskId,
        metadata: { timestamp: Date.now() - 120_000 },
        extensions: [],
        referenceTaskIds: [],
      },
      // Agent message with tool call request (DataPart)
      {
        messageId: `${taskId}-tool-call`,
        role: 2,
        parts: [
          {
            content: { $case: "data", value: { id: `call-${taskId}`, name: toolName, args: toolArgs } },
            metadata: { adk_type: "function_call" },
            filename: "",
            mediaType: "application/json",
          },
        ],
        contextId,
        taskId,
        metadata: {
          displaySource: "assistant",
          timestamp: Date.now() - 90_000,
        },
        extensions: [],
        referenceTaskIds: [],
      },
      // Agent message with tool execution result (DataPart)
      {
        messageId: `${taskId}-tool-result`,
        role: 2,
        parts: [
          {
            content: { $case: "data", value: {
              id: `call-${taskId}`,
              name: toolName,
              response: { result: toolResult, isError: false },
            } },
            metadata: { adk_type: "function_response" },
            filename: "",
            mediaType: "application/json",
          },
        ],
        contextId,
        taskId,
        metadata: {
          displaySource: "assistant",
          timestamp: Date.now() - 60_000,
        },
        extensions: [],
        referenceTaskIds: [],
      },
      // Final text response after tool execution
      {
        messageId: `${taskId}-final`,
        role: 2,
        parts: [
          {
            content: { $case: "text", value: `I used the **${toolName}** tool and here are the results:\n\n${toolResult}` },
            metadata: {},
            filename: "",
            mediaType: "text/plain",
          },
        ],
        contextId,
        taskId,
        metadata: {
          displaySource: "assistant",
          timestamp: Date.now() - 30_000,
        },
        extensions: [],
        referenceTaskIds: [],
      },
    ],
    artifacts: [],
    metadata: undefined,
  } as unknown as Task;
}

// ---------------------------------------------------------------------------
// Handler factories – compose these in per-story `beforeEach` calls
// ---------------------------------------------------------------------------

/** GET /sessions/:sessionId – returns a session (used by checkSessionExists & getSession) */
export function sessionExistsHandler(session: Session) {
  return http.get(`${BACKEND_URL}/sessions/:sessionId`, () => {
    return HttpResponse.json({ data: session });
  });
}

/** GET /sessions/:sessionId – returns 404 */
export function sessionNotFoundHandler() {
  return http.get(`${BACKEND_URL}/sessions/:sessionId`, () => {
    return HttpResponse.json(
      { error: "Session not found" },
      { status: 404, headers: { "Content-Type": "application/json" } },
    );
  });
}

/** GET /sessions/:sessionId/tasks – returns task history */
export function sessionTasksHandler(tasks: unknown[]) {
  return http.get(`${BACKEND_URL}/sessions/:sessionId/tasks`, () => {
    return HttpResponse.json({ message: "Tasks fetched successfully", data: tasks });
  });
}

/** GET /sessions/:sessionId/tasks – returns empty task list */
export function emptySessionTasksHandler() {
  return http.get(`${BACKEND_URL}/sessions/:sessionId/tasks`, () => {
    return HttpResponse.json({ message: "Tasks fetched successfully", data: [] });
  });
}

/** POST /sessions – creates a new session */
export function createSessionHandler(session: Session) {
  return http.post(`${BACKEND_URL}/sessions`, () => {
    return HttpResponse.json({ data: session });
  });
}

/** Adds an artificial delay to the session exists check (for loading-state stories) */
export function slowSessionExistsHandler(session: Session, ms = 2000) {
  return http.get(`${BACKEND_URL}/sessions/:sessionId`, async () => {
    await delay(ms);
    return HttpResponse.json({ data: session });
  });
}

/** Adds an artificial delay to the tasks fetch */
export function slowSessionTasksHandler(tasks: unknown[], ms = 2000) {
  return http.get(`${BACKEND_URL}/sessions/:sessionId/tasks`, async () => {
    await delay(ms);
    return HttpResponse.json({ message: "Tasks fetched successfully", data: tasks });
  });
}
