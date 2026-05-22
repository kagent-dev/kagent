import { describe, test, expect } from "@jest/globals";
import type { Message, StreamResponse, Task } from "@a2a-js/sdk";
import {
  createMessage,
  createMessageHandlers,
  extractMessagesFromTasks,
  extractTokenStatsFromTasks,
  getMetadataValue,
  normalizeToolResultToText,
  type ToolResponseData,
} from "@/lib/messageHandlers";

function textPart(text: string) {
  return {
    content: { $case: "text" as const, value: text },
    metadata: {},
    filename: "",
    mediaType: "text/plain",
  };
}

function dataPart(value: unknown, metadata: Record<string, unknown>) {
  return {
    content: { $case: "data" as const, value },
    metadata,
    filename: "",
    mediaType: "application/json",
  };
}

describe("messageHandlers (v1 native types)", () => {
  test("createMessage builds a v1 message", () => {
    const msg = createMessage("hi", "assistant", { contextId: "ctx", taskId: "task" });
    expect(msg.messageId).toBeDefined();
    expect(msg.parts[0].content?.$case).toBe("text");
    expect(msg.parts[0].content?.$case === "text" ? msg.parts[0].content.value : "").toBe("hi");
    expect(msg.contextId).toBe("ctx");
    expect(msg.taskId).toBe("task");
  });

  test("normalizeToolResultToText handles string response", () => {
    const data: ToolResponseData = { id: "1", name: "tool", response: { result: "hello" } };
    expect(normalizeToolResultToText(data)).toBe("hello");
  });

  test("extractTokenStatsFromTasks sums adk/kagent usage metadata", () => {
    const task = {
      id: "t1",
      contextId: "ctx",
      status: undefined,
      artifacts: [],
      history: [
        {
          messageId: "m1",
          contextId: "ctx",
          taskId: "t1",
          role: 2,
          parts: [textPart("a")],
          metadata: { kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 3, candidatesTokenCount: 7 } },
          extensions: [],
          referenceTaskIds: [],
        },
        {
          messageId: "m2",
          contextId: "ctx",
          taskId: "t1",
          role: 2,
          parts: [textPart("b")],
          metadata: { adk_usage_metadata: { totalTokenCount: 5, promptTokenCount: 2, candidatesTokenCount: 3 } },
          extensions: [],
          referenceTaskIds: [],
        },
      ],
      metadata: undefined,
    } as unknown as Task;

    expect(extractTokenStatsFromTasks([task])).toEqual({ total: 15, prompt: 5, completion: 10 });
  });

  test("extractMessagesFromTasks converts function_call and function_response", () => {
    const task = {
      id: "t1",
      contextId: "ctx",
      status: undefined,
      artifacts: [],
      history: [
        {
          messageId: "m1",
          contextId: "ctx",
          taskId: "t1",
          role: 2,
          parts: [dataPart({ id: "call1", name: "my_tool", args: { a: 1 } }, { adk_type: "function_call" })],
          metadata: {},
          extensions: [],
          referenceTaskIds: [],
        },
        {
          messageId: "m2",
          contextId: "ctx",
          taskId: "t1",
          role: 2,
          parts: [dataPart({ id: "call1", name: "my_tool", response: { result: "ok" } }, { adk_type: "function_response" })],
          metadata: {},
          extensions: [],
          referenceTaskIds: [],
        },
      ],
      metadata: undefined,
    } as unknown as Task;

    const out = extractMessagesFromTasks([task]);
    expect((out[0].metadata as Record<string, unknown>)?.originalType).toBe("ToolCallRequestEvent");
    expect((out[1].metadata as Record<string, unknown>)?.originalType).toBe("ToolCallExecutionEvent");
  });

  test("createMessageHandlers processes v1 stream status updates", () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater) => {
        const next = updater(emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setChatStatus: () => {},
      agentContext: { namespace: "kagent", agentName: "agent" },
    });

    const statusUpdate = {
      payload: {
        $case: "statusUpdate",
        value: {
          contextId: "ctx",
          taskId: "t1",
          status: {
            state: 2,
            message: {
              messageId: "m1",
              contextId: "ctx",
              taskId: "t1",
              role: 2,
              parts: [dataPart({ id: "call1", name: "tool", args: {} }, { adk_type: "function_call" })],
              metadata: {},
              extensions: [],
              referenceTaskIds: [],
            },
            timestamp: undefined,
          },
          metadata: {},
        },
      },
    } as unknown as StreamResponse;

    handlers.handleMessageEvent(statusUpdate);
    expect((emitted[0].metadata as Record<string, unknown>)?.originalType).toBe("ToolCallRequestEvent");
  });

  test("getMetadataValue checks adk_ first then kagent_", () => {
    expect(getMetadataValue({ adk_type: "a", kagent_type: "k" }, "type")).toBe("a");
  });
});
