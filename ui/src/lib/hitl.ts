import { Role, type Message } from "@a2a-js/sdk";
import {
  HITL_EXTENSION_URI,
  type AskUserRequestPayload,
  type AskUserResponsePayload,
  type HitlExtensionPayload,
  type HitlRequestPayload,
  type HitlResponsePayload,
  type HitlTool,
  type ToolApprovalRequestPayload,
  type ToolApprovalResponsePayload,
  type ToolDecision,
} from "@/types";

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null;

const normalizeNullableToolArgs = (payload: Record<string, unknown>): Record<string, unknown> => {
  const normalizeTools = (value: unknown): unknown =>
    Array.isArray(value)
      ? value.map(tool => isRecord(tool) && tool.args == null ? { ...tool, args: {} } : tool)
      : value;

  const nested = isRecord(payload.nested)
    ? { ...payload.nested, tools: normalizeTools(payload.nested.tools) }
    : payload.nested;

  return {
    ...payload,
    ...(payload.tools !== undefined && { tools: normalizeTools(payload.tools) }),
    ...(payload.nested !== undefined && { nested }),
  };
};

const isHitlTool = (value: unknown): value is HitlTool =>
  isRecord(value) &&
  typeof value.id === "string" &&
  typeof value.call_id === "string" &&
  typeof value.name === "string" &&
  isRecord(value.args);

const hasValidNestedTools = (value: unknown): boolean => {
  if (value == null) return true;
  return isRecord(value) && Array.isArray(value.tools) && value.tools.length > 0 && value.tools.every(isHitlTool);
};

const isToolApprovalRequest = (payload: unknown): payload is ToolApprovalRequestPayload =>
  isRecord(payload) &&
  payload.type === "tool_approval_request" &&
  Array.isArray(payload.tools) &&
  payload.tools.length > 0 &&
  payload.tools.every(isHitlTool) &&
  hasValidNestedTools(payload.nested);

const isAskUserRequest = (payload: unknown): payload is AskUserRequestPayload =>
  isRecord(payload) &&
  payload.type === "ask_user_request" &&
  typeof payload.id === "string" &&
  Array.isArray(payload.questions) &&
  hasValidNestedTools(payload.nested);

const isToolApprovalResponse = (payload: unknown): payload is ToolApprovalResponsePayload =>
  isRecord(payload) &&
  payload.type === "tool_approval_response" &&
  Array.isArray(payload.approvals) &&
  payload.approvals.length > 0 &&
  payload.approvals.every(approval =>
    isRecord(approval) && typeof approval.id === "string" && typeof approval.approved === "boolean"
  );

const isAskUserResponse = (payload: unknown): payload is AskUserResponsePayload =>
  isRecord(payload) &&
  payload.type === "ask_user_response" &&
  typeof payload.id === "string" &&
  (payload.answers == null || Array.isArray(payload.answers));

/** Parse the only HITL wire format understood by the UI: the A2A extension. */
export function getHitlPayload(message: Message): HitlExtensionPayload | undefined {
  if (!message.extensions?.includes(HITL_EXTENSION_URI)) return undefined;
  const payload = (message.metadata as Record<string, unknown> | undefined)?.[HITL_EXTENSION_URI];
  if (!isRecord(payload)) return undefined;
  const normalized = normalizeNullableToolArgs(payload);
  if (isToolApprovalRequest(normalized) || isAskUserRequest(normalized)) return normalized;
  if (isToolApprovalResponse(normalized) || isAskUserResponse(normalized)) return normalized;
  return undefined;
}

export const isHitlRequest = (payload: HitlExtensionPayload): payload is HitlRequestPayload =>
  payload.type === "tool_approval_request" || payload.type === "ask_user_request";

export const isHitlResponse = (payload: HitlExtensionPayload): payload is HitlResponsePayload =>
  payload.type === "tool_approval_response" || payload.type === "ask_user_response";

/** Nested tools are the operations displayed to and decided by the human. */
export function visibleHitlTools(request: ToolApprovalRequestPayload): HitlTool[] {
  return request.nested?.tools ?? request.tools;
}

/** All tool calls represented by a request, including a remote-agent parent call. */
export function relatedHitlCallIds(request: HitlRequestPayload): Set<string> {
  const tools = request.type === "tool_approval_request" ? request.tools : [];
  return new Set([...tools, ...(request.nested?.tools ?? [])].map(tool => tool.call_id));
}

export function responseMatchesRequest(request: HitlRequestPayload, response: HitlResponsePayload): boolean {
  if (request.type === "ask_user_request") {
    return response.type === "ask_user_response" && response.id === request.id;
  }
  if (response.type !== "tool_approval_response") return false;
  const expectedIds = new Set(visibleHitlTools(request).map(tool => tool.id));
  const responseIds = new Set(response.approvals.map(approval => approval.id));
  return response.approvals.length === expectedIds.size &&
    responseIds.size === expectedIds.size &&
    [...expectedIds].every(id => responseIds.has(id));
}

/** Convert resolved extension results to the call-id keyed state used by tool rendering. */
export function decisionsByCallId(
  request: ToolApprovalRequestPayload,
  response: HitlResponsePayload | undefined,
): Record<string, ToolDecision> {
  if (response?.type !== "tool_approval_response") return {};
  const results = new Map(response.approvals.map(approval => [approval.id, approval.approved]));
  return Object.fromEntries(
    visibleHitlTools(request).flatMap(tool => {
      const approved = results.get(tool.id);
      return approved === undefined ? [] : [[tool.call_id, approved ? "approve" : "reject"]];
    }),
  );
}

export function createHitlResponseMessage(
  payload: HitlResponsePayload,
  options: { messageId: string; contextId?: string; taskId?: string; text: string },
): Message {
  return {
    messageId: options.messageId,
    role: Role.ROLE_USER,
    parts: [{
      content: { $case: "text", value: options.text },
      metadata: undefined,
      filename: "",
      mediaType: "text/plain",
    }],
    contextId: options.contextId ?? "",
    taskId: options.taskId ?? "",
    metadata: { timestamp: Date.now(), [HITL_EXTENSION_URI]: payload },
    extensions: [HITL_EXTENSION_URI],
    referenceTaskIds: [],
  };
}
