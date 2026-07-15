import React from "react";
import { render, screen, fireEvent } from "@testing-library/react";
import type { Message } from "@a2a-js/sdk";
import ToolCallGroup, { groupToolCallMessages, isGroupableToolMessage, buildToolCallResultsIndex, collectPendingApprovalIds } from "@/components/chat/ToolCallGroup";
import { isAgentToolName } from "@/lib/utils";

const textMessage = (text: string, role: "user" | "agent" = "agent"): Message => ({
  kind: "message",
  messageId: `text-${text}`,
  role,
  parts: [{ kind: "text", text }],
});

const requestMessage = (id: string, name: string): Message => ({
  kind: "message",
  messageId: `req-${id}`,
  role: "agent",
  parts: [
    {
      kind: "data",
      data: { id, name, args: {} },
      metadata: { adk_type: "function_call" },
    },
  ],
});

const responseMessage = (id: string, name: string, isError = false): Message => ({
  kind: "message",
  messageId: `res-${id}`,
  role: "agent",
  parts: [
    {
      kind: "data",
      data: { id, name, response: { result: isError ? "boom" : "ok", isError } },
      metadata: { adk_type: "function_response" },
    },
  ],
});

const approvalMessage = (id: string, approvalDecision?: unknown): Message => ({
  kind: "message",
  messageId: `approval-${id}`,
  role: "agent",
  metadata: { originalType: "ToolApprovalRequest", ...(approvalDecision !== undefined ? { approvalDecision } : {}) },
  parts: [
    {
      kind: "data",
      data: { id, name: "dangerous_tool", args: {} },
      metadata: { adk_type: "function_call" },
    },
  ],
});

describe("isGroupableToolMessage", () => {
  it("accepts function_call and function_response messages", () => {
    expect(isGroupableToolMessage(requestMessage("c1", "t"))).toBe(true);
    expect(isGroupableToolMessage(responseMessage("c1", "t"))).toBe(true);
  });

  it("rejects user and plain text messages", () => {
    expect(isGroupableToolMessage(textMessage("hi", "user"))).toBe(false);
    expect(isGroupableToolMessage(textMessage("hello"))).toBe(false);
  });

  it("never groups undecided approval or ask-user messages", () => {
    expect(isGroupableToolMessage(approvalMessage("c1"))).toBe(false);
    expect(
      isGroupableToolMessage({
        kind: "message",
        messageId: "ask-1",
        role: "agent",
        metadata: { originalType: "AskUserRequest" },
        parts: [],
      }),
    ).toBe(false);
  });

  it("groups approval messages once decided", () => {
    // Persisted decision (uniform string)
    expect(isGroupableToolMessage(approvalMessage("c1", "approve"))).toBe(true);
    // Persisted decision (per-tool map)
    expect(isGroupableToolMessage(approvalMessage("c1", { c1: "reject" }))).toBe(true);
    // Local optimistic decision
    expect(isGroupableToolMessage(approvalMessage("c1"), { pendingDecisions: { c1: "approve" } })).toBe(true);
    // Map decision for a different call id does not count
    expect(isGroupableToolMessage(approvalMessage("c1", { other: "approve" }))).toBe(false);
  });
});

describe("groupToolCallMessages", () => {
  it("folds consecutive tool messages into one group and keeps text standalone", () => {
    const messages = [
      textMessage("question", "user"),
      requestMessage("c1", "tool_a"),
      responseMessage("c1", "tool_a"),
      requestMessage("c2", "tool_b"),
      responseMessage("c2", "tool_b"),
      textMessage("answer"),
    ];

    const items = groupToolCallMessages(messages);
    expect(items).toHaveLength(3);
    expect(items[0]).toMatchObject({ kind: "single", startIndex: 0 });
    expect(items[1]).toMatchObject({ kind: "group", startIndex: 1 });
    expect((items[1] as { messages: Message[] }).messages).toHaveLength(4);
    expect(items[2]).toMatchObject({ kind: "single", startIndex: 5 });
  });

  it("floats undecided approvals outside without breaking the run; decided ones fold in", () => {
    const messages = [
      requestMessage("c1", "tool_a"),
      responseMessage("c1", "tool_a"),
      approvalMessage("c2"),
      requestMessage("c3", "tool_c"),
    ];

    const items = groupToolCallMessages(messages);
    // The pending approval floats out as a single, but the run stays open so
    // c1 and c3 share one group.
    expect(items.map(i => i.kind)).toEqual(["group", "single"]);
    expect((items[0] as { messages: Message[] }).messages).toHaveLength(3);

    // Same transcript after the user decides: one contiguous group.
    const decided = groupToolCallMessages(messages, { pendingDecisions: { c2: "approve" } });
    expect(decided.map(i => i.kind)).toEqual(["group"]);
    expect((decided[0] as { messages: Message[] }).messages).toHaveLength(4);
  });

  it("keeps the request message that carries a pending approval card outside the group", () => {
    // The approve/reject buttons render under the FIRST message introducing
    // the call id — the plain function_call request — not the
    // ToolApprovalRequest message itself.
    const messages = [
      requestMessage("c1", "tool_a"),
      responseMessage("c1", "tool_a"),
      requestMessage("c2", "dangerous_tool"),
      approvalMessage("c2"),
      requestMessage("c3", "tool_c"),
    ];

    const pending = groupToolCallMessages(messages, {
      pendingApprovalIds: collectPendingApprovalIds(messages),
    });
    // Both the request (owning the approval card) and the approval message
    // float out as singles; the run stays open so c1 and c3 share one group.
    expect(pending.map(i => i.kind)).toEqual(["group", "single", "single"]);
    expect((pending[0] as { messages: Message[] }).messages).toHaveLength(3);

    // After the user decides, everything folds into one group.
    const pendingDecisions = { c2: "approve" as const };
    const decided = groupToolCallMessages(messages, {
      pendingDecisions,
      pendingApprovalIds: collectPendingApprovalIds(messages, pendingDecisions),
    });
    expect(decided.map(i => i.kind)).toEqual(["group"]);
  });

  it("floats standalone tools (e.g. MCP apps) outside without breaking the run", () => {
    const isMcpApp = (name: string) => name === "mcp_app_tool";
    const messages = [
      requestMessage("c1", "tool_a"),
      responseMessage("c1", "tool_a"),
      requestMessage("c2", "mcp_app_tool"),
      responseMessage("c2", "mcp_app_tool"),
      requestMessage("c3", "tool_c"),
    ];

    const items = groupToolCallMessages(messages, { isStandaloneToolName: isMcpApp });
    expect(items.map(i => i.kind)).toEqual(["group", "single", "single"]);
    expect(items[0]).toMatchObject({ kind: "group", startIndex: 0 });
    // The run stays open across the MCP app call: c1 and c3 share one group.
    expect((items[0] as { messages: Message[] }).messages).toHaveLength(3);
    expect(items[1]).toMatchObject({ kind: "single", startIndex: 2 });
    expect(items[2]).toMatchObject({ kind: "single", startIndex: 3 });
  });

  it("floats subagent (agent-tool) calls outside without breaking the run", () => {
    // Mirrors ChatInterface's predicate: agent tools (namespaced names) and
    // MCP app tools both render standalone.
    const isStandalone = (name: string) => isAgentToolName(name) || name === "mcp_app_tool";
    const messages = [
      requestMessage("c1", "tool_a"),
      requestMessage("c2", "kagent__NS__researcher"),
      responseMessage("c1", "tool_a"),
      responseMessage("c2", "kagent__NS__researcher"),
      requestMessage("c3", "tool_c"),
      responseMessage("c3", "tool_c"),
    ];

    const items = groupToolCallMessages(messages, { isStandaloneToolName: isStandalone });
    expect(items.map(i => i.kind)).toEqual(["group", "single", "single"]);
    // The subagent request/response float out; the rest share one group.
    expect((items[0] as { messages: Message[] }).messages).toHaveLength(4);
    expect((items[1] as { message: Message }).message.messageId).toBe("req-c2");
    expect((items[2] as { message: Message }).message.messageId).toBe("res-c2");
  });

  it("keeps an interleaved parallel batch as one group (MCP app + approval mid-batch)", () => {
    // Models issue parallel batches: [events, logs, weather(MCP), datetime(approval)].
    // The MCP app and the decided approval must not shatter the batch.
    const isMcpApp = (name: string) => name === "show-weather-dashboard";
    const messages = [
      requestMessage("c1", "k8s_get_events"),
      requestMessage("c2", "k8s_get_pod_logs"),
      requestMessage("c3", "show-weather-dashboard"),
      requestMessage("c4", "datetime_get_current_time"),
      responseMessage("c1", "k8s_get_events"),
      responseMessage("c2", "k8s_get_pod_logs"),
      responseMessage("c3", "show-weather-dashboard"),
      approvalMessage("c4", "approve"),
      responseMessage("c4", "datetime_get_current_time"),
      requestMessage("c5", "k8s_get_pod_logs"),
      responseMessage("c5", "k8s_get_pod_logs"),
      textMessage("answer"),
    ];

    const items = groupToolCallMessages(messages, { isStandaloneToolName: isMcpApp });
    // One group for the whole batch + the two MCP app singles + final text.
    expect(items.map(i => i.kind)).toEqual(["group", "single", "single", "single"]);
    const group = items[0] as { messages: Message[] };
    expect(group.messages).toHaveLength(9);

    // The approval card repeats c4's request — the summary must dedupe by id.
    render(
      <ToolCallGroup messages={group.messages} resultsByCallId={buildToolCallResultsIndex(messages)}>
        <div />
      </ToolCallGroup>,
    );
    expect(screen.getByRole("button")).toHaveTextContent("4 tool calls");
  });
});

describe("ToolCallGroup", () => {
  const messages = [
    requestMessage("c1", "tool_a"),
    responseMessage("c1", "tool_a"),
    requestMessage("c2", "tool_b"),
    responseMessage("c2", "tool_b", true),
    requestMessage("c3", "tool_c"),
    responseMessage("c3", "tool_c"),
  ];

  const renderGroup = () =>
    render(
      <ToolCallGroup messages={messages} resultsByCallId={buildToolCallResultsIndex(messages)}>
        <div data-testid="group-child">tool call details</div>
      </ToolCallGroup>,
    );

  it("shows total and pass/fail counts collapsed by default", () => {
    renderGroup();
    const toggle = screen.getByRole("button", { expanded: false });
    expect(toggle).toHaveTextContent("3 tool calls");
    expect(toggle).toHaveTextContent("2");
    expect(toggle).toHaveTextContent("1");
    expect(screen.getByText("succeeded")).toBeInTheDocument();
    expect(screen.getByText("failed")).toBeInTheDocument();
  });

  it("expands and collapses on toggle", () => {
    renderGroup();
    const toggle = screen.getByRole("button");
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "false");
  });

  it("shows running progress while calls are in flight", () => {
    const inFlight = [requestMessage("c1", "tool_a"), responseMessage("c1", "tool_a"), requestMessage("c2", "tool_b")];
    render(
      <ToolCallGroup messages={inFlight} resultsByCallId={buildToolCallResultsIndex(inFlight)}>
        <div />
      </ToolCallGroup>,
    );
    expect(screen.getByRole("button")).toHaveTextContent("Running tools 1/2");
  });

  it("renders children without chrome when there are no visible calls", () => {
    render(
      <ToolCallGroup messages={[]} resultsByCallId={new Map()}>
        <div data-testid="bare-child" />
      </ToolCallGroup>,
    );
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByTestId("bare-child")).toBeInTheDocument();
  });
});
