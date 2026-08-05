import { describe, expect, test } from "@jest/globals";
import { Role, type Message } from "@a2a-js/sdk";
import {
  askUserResponseId,
  buildAskUserResponse,
  buildHitlCard,
  createHitlResponseMessage,
  findPendingHitl,
  getHitlPayload,
  relatedHitlCallIds,
  responseMatchesRequest,
  visibleHitlTools,
} from "@/lib/hitl";
import { HITL_EXTENSION_URI, type AskUserRequestPayload, type ToolApprovalRequestPayload } from "@/types";

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

  test("buildHitlCard is the only UI HITL display model", () => {
    const request: ToolApprovalRequestPayload = {
      type: "tool_approval_request",
      tools: [{ id: "approval-1", call_id: "call-1", name: "delete", args: {} }],
    };
    const card = buildHitlCard(request);
    expect(card).toEqual({
      kind: "tool_approval",
      request,
      calls: [{ id: "call-1", name: "delete", args: {} }],
    });
    expect(findPendingHitl([{ metadata: { hitlCard: card }, taskId: "task-1" }])).toEqual({
      request,
      taskId: "task-1",
      contextId: undefined,
    });
    expect(findPendingHitl([{
      metadata: { hitlCard: buildHitlCard(request, { type: "tool_approval_response", approvals: [{ id: "approval-1", approved: true }] }) },
    }])).toBeUndefined();
  });

  test("nested ask_user responses use the child correlation id", () => {
    const request: AskUserRequestPayload = {
      type: "ask_user_request",
      id: "parent-confirm",
      questions: [{ question: "Which namespace?" }],
      nested: {
        subagent_name: "child_agent",
        task_id: "child-task",
        context_id: "child-context",
        tools: [{ id: "child-confirm", call_id: "child-confirm", name: "ask_user", args: { questions: [] } }],
      },
    };

    expect(askUserResponseId(request)).toBe("child-confirm");
    const response = buildAskUserResponse(request, [{ answer: ["default"] }]);
    expect(response.id).toBe("child-confirm");
    expect(responseMatchesRequest(request, response)).toBe(true);
    expect(responseMatchesRequest(request, {
      type: "ask_user_response",
      id: "parent-confirm",
      answers: [{ answer: ["default"] }],
    })).toBe(false);
  });
});
