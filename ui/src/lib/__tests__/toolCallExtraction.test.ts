import { Role, type Message, type Part } from "@a2a-js/sdk";
import {
  isToolCallRequestMessage,
  isToolCallExecutionMessage,
  isToolCallSummaryMessage,
  extractToolCallRequests,
  extractToolCallResults,
} from "@/lib/toolCallExtraction";
import { createDataPart, createMockMessage, createTextPart } from "@/mocks/factories";

const message = (overrides: Partial<Message> = {}): Message =>
  createMockMessage({
    messageId: "m1",
    role: Role.ROLE_AGENT,
    contextId: "ctx-1",
    taskId: "task-1",
    ...overrides,
  });

const requestPart = (id: string, name: string, prefix: "adk" | "kagent" = "adk"): Part =>
  createDataPart({ id, name, args: { foo: "bar" } }, { [`${prefix}_type`]: "function_call" });

const responsePart = (id: string, name: string, response: Record<string, unknown>, prefix: "adk" | "kagent" = "adk"): Part =>
  createDataPart({ id, name, response }, { [`${prefix}_type`]: "function_response" });

describe("message type predicates", () => {
  it("detects request/execution messages from data parts", () => {
    expect(isToolCallRequestMessage(message({ parts: [requestPart("c1", "t")] }))).toBe(true);
    expect(isToolCallExecutionMessage(message({ parts: [responsePart("c1", "t", { result: "ok" })] }))).toBe(true);
  });

  it("supports the kagent_ metadata prefix", () => {
    expect(isToolCallRequestMessage(message({ parts: [requestPart("c1", "t", "kagent")] }))).toBe(true);
    expect(isToolCallExecutionMessage(message({ parts: [responsePart("c1", "t", { result: "ok" }, "kagent")] }))).toBe(true);
  });

  it("falls back to originalType for streaming messages", () => {
    expect(isToolCallRequestMessage(message({ metadata: { originalType: "ToolCallRequestEvent" } }))).toBe(true);
    expect(isToolCallRequestMessage(message({ metadata: { originalType: "ToolApprovalRequest" } }))).toBe(true);
    expect(isToolCallExecutionMessage(message({ metadata: { originalType: "ToolCallExecutionEvent" } }))).toBe(true);
    expect(isToolCallSummaryMessage(message({ metadata: { originalType: "ToolCallSummaryMessage" } }))).toBe(true);
  });

  it("rejects plain text messages", () => {
    const text = message({ parts: [createTextPart("hello")] });
    expect(isToolCallRequestMessage(text)).toBe(false);
    expect(isToolCallExecutionMessage(text)).toBe(false);
    expect(isToolCallSummaryMessage(text)).toBe(false);
  });
});

describe("extractToolCallRequests", () => {
  it("extracts calls from data parts", () => {
    const msg = message({ parts: [requestPart("c1", "tool_a"), requestPart("c2", "tool_b")] });
    expect(extractToolCallRequests(msg)).toEqual([
      { id: "c1", name: "tool_a", args: { foo: "bar" } },
      { id: "c2", name: "tool_b", args: { foo: "bar" } },
    ]);
  });

  it("filters authentication and ask_user calls with dedicated displays", () => {
    const msg = message({
      parts: [
        requestPart("c1", "adk_request_credential"),
        requestPart("c2", "ask_user"),
        requestPart("c3", "real_tool"),
      ],
    });
    expect(extractToolCallRequests(msg).map(c => c.name)).toEqual(["real_tool"]);
  });

  it("falls back to metadata.toolCallData for streaming messages", () => {
    const msg = message({
      metadata: {
        originalType: "ToolCallRequestEvent",
        toolCallData: [
          { id: "c1", name: "tool_a", args: {} },
          { id: "c2", name: "ask_user", args: {} },
        ],
      },
    });
    expect(extractToolCallRequests(msg).map(c => c.name)).toEqual(["tool_a"]);
  });

  it("returns [] for non-request messages and malformed JSON content", () => {
    expect(extractToolCallRequests(message({ parts: [createTextPart("hi")] }))).toEqual([]);
    expect(
      extractToolCallRequests(
        message({
          metadata: { originalType: "ToolCallRequestEvent" },
          parts: [createTextPart("not json")],
        }),
      ),
    ).toEqual([]);
  });
});

describe("extractToolCallResults", () => {
  it("extracts results with error flags", () => {
    const msg = message({
      parts: [
        responsePart("c1", "tool_a", { result: "ok", isError: false }),
        responsePart("c2", "tool_b", { result: "boom", isError: true }),
      ],
    });
    const results = extractToolCallResults(msg);
    expect(results).toHaveLength(2);
    expect(results[0]).toMatchObject({ call_id: "c1", name: "tool_a", is_error: false, raw_result: "ok" });
    expect(results[1]).toMatchObject({ call_id: "c2", name: "tool_b", is_error: true, raw_result: "boom" });
  });

  it("extracts subagent_session_id for agent tool responses", () => {
    const msg = message({
      parts: [
        responsePart("c1", "ns__NS__helper", { result: "done", subagent_session_id: "sess-42" }),
      ],
    });
    expect(extractToolCallResults(msg)[0]).toMatchObject({
      call_id: "c1",
      subagent_session_id: "sess-42",
    });
  });

  it("falls back to metadata.toolResultData for streaming messages", () => {
    const msg = message({
      metadata: {
        originalType: "ToolCallExecutionEvent",
        toolResultData: [{ call_id: "c1", name: "tool_a", content: "ok", is_error: false }],
      },
    });
    expect(extractToolCallResults(msg)).toEqual([{ call_id: "c1", name: "tool_a", content: "ok", is_error: false }]);
  });

  it("returns [] for non-execution messages", () => {
    expect(extractToolCallResults(message({ parts: [requestPart("c1", "t")] }))).toEqual([]);
  });
});
