import React from "react";
import { render, screen, fireEvent } from "@testing-library/react";
import type { Message } from "@a2a-js/sdk";
import ToolCallGroup, { groupToolCallMessages, isGroupableToolMessage, buildToolCallResultsIndex } from "@/components/chat/ToolCallGroup";

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

const approvalMessage = (id: string): Message => ({
  kind: "message",
  messageId: `approval-${id}`,
  role: "agent",
  metadata: { originalType: "ToolApprovalRequest" },
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

  it("never groups approval or ask-user messages", () => {
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

  it("breaks a run on approval messages so they stay visible", () => {
    const messages = [
      requestMessage("c1", "tool_a"),
      responseMessage("c1", "tool_a"),
      approvalMessage("c2"),
      requestMessage("c3", "tool_c"),
    ];

    const items = groupToolCallMessages(messages);
    expect(items.map(i => i.kind)).toEqual(["group", "single", "group"]);
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
