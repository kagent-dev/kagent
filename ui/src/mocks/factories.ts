import type { Session } from "@/types";
import { Message, Part, Role, Task, TaskState } from "@a2a-js/sdk";

// ---------------------------------------------------------------------------
// Mock data factories (MSW-free so Jest can import them)
// ---------------------------------------------------------------------------

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

/**
 * Creates a minimal A2A Task object whose `history` array contains plain
 * user/agent Message entries.  This is the shape returned by
 * `GET /sessions/:id/tasks` and consumed by `extractMessagesFromTasks`.
 */
export function createTextPart(text: string, metadata?: Record<string, unknown>): Part {
  return {
    content: { $case: "text", value: text },
    metadata,
    filename: "",
    mediaType: "text/plain",
  };
}

export function createDataPart(data: unknown, metadata?: Record<string, unknown>): Part {
  return {
    content: { $case: "data", value: data },
    metadata,
    filename: "",
    mediaType: "application/json",
  };
}

export function createMockMessage(
  overrides: Partial<Message> & Pick<Message, "messageId" | "role">,
): Message {
  return {
    messageId: overrides.messageId,
    role: overrides.role,
    parts: overrides.parts ?? [],
    contextId: overrides.contextId ?? "",
    taskId: overrides.taskId ?? "",
    metadata: overrides.metadata,
    extensions: overrides.extensions ?? [],
    referenceTaskIds: overrides.referenceTaskIds ?? [],
  };
}

export function createMockTextMessage(
  messageId: string,
  role: Role,
  text: string,
  overrides: Partial<Message> = {},
): Message {
  return createMockMessage({
    messageId,
    role,
    parts: [createTextPart(text)],
    ...overrides,
  });
}

export function createMockTask(
  taskId: string,
  contextId: string,
  history: Message[],
  status: Pick<NonNullable<Task["status"]>, "state"> & Partial<NonNullable<Task["status"]>> = {
    state: TaskState.TASK_STATE_COMPLETED,
  },
): Task {
  return {
    id: taskId,
    contextId,
    status: {
      message: undefined,
      timestamp: new Date().toISOString(),
      ...status,
    },
    artifacts: [],
    history: history.map((message, i) => ({
      messageId: message.messageId || `${taskId}-msg-${i}`,
      role: message.role,
      parts: message.parts ?? [],
      contextId: message.contextId || contextId,
      taskId: message.taskId || taskId,
      metadata: {
        displaySource: message.role === Role.ROLE_AGENT ? "assistant" : undefined,
        timestamp: Date.now() - (history.length - i) * 60_000,
        ...(message.metadata ?? {}),
      },
      extensions: message.extensions ?? [],
      referenceTaskIds: message.referenceTaskIds ?? [],
    })),
    metadata: undefined,
  };
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
    status: {
      state: TaskState.TASK_STATE_COMPLETED,
      message: undefined,
      timestamp: new Date().toISOString(),
    },
    artifacts: [],
    history: [
      createMockTextMessage(`${taskId}-user`, Role.ROLE_USER, "Run the tool", {
        contextId,
        taskId,
        metadata: { timestamp: Date.now() - 120_000 },
      }),
      createMockMessage({
        messageId: `${taskId}-tool-call`,
        role: Role.ROLE_AGENT,
        contextId,
        taskId,
        parts: [
          createDataPart(
            { id: `call-${taskId}`, name: toolName, args: toolArgs },
            { kagent_type: "function_call" },
          ),
        ],
        metadata: {
          displaySource: "assistant",
          timestamp: Date.now() - 90_000,
        },
      }),
      createMockMessage({
        messageId: `${taskId}-tool-result`,
        role: Role.ROLE_AGENT,
        contextId,
        taskId,
        parts: [
          createDataPart(
            {
              id: `call-${taskId}`,
              name: toolName,
              response: { result: toolResult, isError: false },
            },
            { kagent_type: "function_response" },
          ),
        ],
        metadata: {
          displaySource: "assistant",
          timestamp: Date.now() - 60_000,
        },
      }),
      createMockTextMessage(
        `${taskId}-final`,
        Role.ROLE_AGENT,
        `I used the **${toolName}** tool and here are the results:\n\n${toolResult}`,
        {
          contextId,
          taskId,
          metadata: {
            displaySource: "assistant",
            timestamp: Date.now() - 30_000,
          },
        },
      ),
    ],
    metadata: undefined,
  };
}
