import { describe, expect, test } from "@jest/globals";
import { Role, type Message } from "@a2a-js/sdk";
import {
  createHitlResponseMessage,
  getHitlPayload,
  relatedHitlCallIds,
  responseMatchesRequest,
  visibleHitlTools,
} from "@/lib/hitl";
import { HITL_EXTENSION_URI, type ToolApprovalRequestPayload } from "@/types";

const messageWithPayload = (payload: unknown): Message => ({
  messageId: "message-1",
  role: Role.ROLE_AGENT,
  parts: [],
  contextId: "context-1",
  taskId: "task-1",
  metadata: { [HITL_EXTENSION_URI]: payload },
  extensions: [HITL_EXTENSION_URI],
  referenceTaskIds: [],
});

describe("A2A HITL extension helpers", () => {
  test("parses only a declared, valid extension payload", () => {
    const request = getHitlPayload(messageWithPayload({
      type: "tool_approval_request",
      tools: [{ id: "approval-1", call_id: "call-1", name: "delete", args: {} }],
    }));
    expect(request?.type).toBe("tool_approval_request");

    const legacy = messageWithPayload({ decision_type: "approve" });
    expect(getHitlPayload(legacy)).toBeUndefined();
  });

  test("normalizes null tool arguments from no-argument calls", () => {
    const request = getHitlPayload(messageWithPayload({
      type: "tool_approval_request",
      tools: [{ id: "approval-1", call_id: "call-1", name: "get_cluster", args: null }],
    }));

    expect(request).toEqual({
      type: "tool_approval_request",
      tools: [{ id: "approval-1", call_id: "call-1", name: "get_cluster", args: {} }],
    });
  });

  test("uses nested tools as the human-visible operations", () => {
    const request: ToolApprovalRequestPayload = {
      type: "tool_approval_request",
      tools: [{ id: "parent-approval", call_id: "parent-call", name: "subagent", args: {} }],
      nested: {
        subagent_name: "cluster-agent",
        tools: [{ id: "child-approval", call_id: "child-call", name: "delete", args: {} }],
      },
    };

    expect(visibleHitlTools(request)[0].id).toBe("child-approval");
    expect([...relatedHitlCallIds(request)]).toEqual(["parent-call", "child-call"]);
    expect(responseMatchesRequest(request, {
      type: "tool_approval_response",
      approvals: [{ id: "child-approval", approved: true }],
    })).toBe(true);
    expect(responseMatchesRequest(request, {
      type: "tool_approval_response",
      approvals: [
        { id: "child-approval", approved: true },
        { id: "unknown-approval", approved: true },
      ],
    })).toBe(false);
  });

  test("builds a response message containing only the public extension", () => {
    const message = createHitlResponseMessage(
      { type: "ask_user_response", id: "question-1", answers: [{ answer: ["prod"] }] },
      { messageId: "response-1", contextId: "context-1", taskId: "task-1", text: "Answered questions" },
    );

    expect(message.extensions).toEqual([HITL_EXTENSION_URI]);
    expect(getHitlPayload(message)).toEqual({
      type: "ask_user_response",
      id: "question-1",
      answers: [{ answer: ["prod"] }],
    });
  });
});
