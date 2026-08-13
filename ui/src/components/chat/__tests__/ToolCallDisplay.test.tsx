import React from "react";
import { render, screen } from "@testing-library/react";
import { expect, test } from "@jest/globals";
import { Role, type Message } from "@a2a-js/sdk";
import ToolCallDisplay from "@/components/chat/ToolCallDisplay";

test("passes a pending nested HITL session to the AgentCall activity card", () => {
  const message: Message = {
    messageId: "agent-call-message",
    role: Role.ROLE_AGENT,
    parts: [],
    contextId: "parent-session",
    taskId: "parent-task",
    metadata: {
      originalType: "ToolCallRequestEvent",
      toolCallData: [{
        id: "parent-call",
        name: "child-agent",
        args: { request: "delete pod" },
        subagent_session_id: "child-session",
      }],
    },
    extensions: [],
    referenceTaskIds: [],
  };

  render(<ToolCallDisplay currentMessage={message} allMessages={[message]} />);

  expect(screen.getByText("child-agent")).toBeTruthy();
  expect(screen.getByText("Activity")).toBeTruthy();
});
