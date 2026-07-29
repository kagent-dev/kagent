import type { Session } from "@/types";
import type { Task, TaskState } from "@a2a-js/sdk";

export function createMockSession(overrides: Partial<Session> = {}): Session {
  return {
    id: "session-123",
    name: "Test conversation",
    agent_id: "kagent__NS__k8s",
    user_id: "admin@kagent.dev",
    created_at: "2026-03-07T10:00:00Z",
    updated_at: "2026-03-07T10:05:00Z",
    deleted_at: "",
    ...overrides,
  };
}

export function createMockTask(
  taskId: string,
  contextId: string,
  history: Array<{
    role: "user" | "agent";
    text: string;
    messageId?: string;
    metadata?: Record<string, unknown>;
  }>,
  status: { state: TaskState } = { state: "completed" },
): Task {
  return {
    id: taskId,
    contextId,
    kind: "task",
    status,
    history: history.map((item, index) => ({
      kind: "message" as const,
      messageId: item.messageId ?? `${taskId}-msg-${index}`,
      role: item.role,
      parts: [{ kind: "text" as const, text: item.text }],
      metadata: {
        displaySource: item.role === "agent" ? "assistant" : undefined,
        timestamp: Date.now() - (history.length - index) * 60_000,
        ...item.metadata,
      },
    })),
  };
}

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
    kind: "task",
    status: { state: "completed" },
    history: [
      {
        kind: "message" as const,
        messageId: `${taskId}-user`,
        role: "user" as const,
        parts: [{ kind: "text" as const, text: "Run the tool" }],
        metadata: { timestamp: Date.now() - 120_000 },
      },
      {
        kind: "message" as const,
        messageId: `${taskId}-tool-call`,
        role: "agent" as const,
        parts: [
          {
            kind: "data" as const,
            data: { id: `call-${taskId}`, name: toolName, args: toolArgs },
            metadata: { adk_type: "function_call" },
          },
        ],
        metadata: {
          displaySource: "assistant",
          timestamp: Date.now() - 90_000,
        },
      },
      {
        kind: "message" as const,
        messageId: `${taskId}-tool-result`,
        role: "agent" as const,
        parts: [
          {
            kind: "data" as const,
            data: {
              id: `call-${taskId}`,
              name: toolName,
              response: { result: toolResult, isError: false },
            },
            metadata: { adk_type: "function_response" },
          },
        ],
        metadata: {
          displaySource: "assistant",
          timestamp: Date.now() - 60_000,
        },
      },
      {
        kind: "message" as const,
        messageId: `${taskId}-final`,
        role: "agent" as const,
        parts: [
          {
            kind: "text" as const,
            text: `I used the **${toolName}** tool and here are the results:\n\n${toolResult}`,
          },
        ],
        metadata: {
          displaySource: "assistant",
          timestamp: Date.now() - 30_000,
        },
      },
    ],
  };
}
