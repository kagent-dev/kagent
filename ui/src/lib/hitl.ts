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

export type HitlCard =
  | {
      kind: "ask_user";
      request: AskUserRequestPayload;
      response?: AskUserResponsePayload;
      subagentName?: string;
    }
  | {
      kind: "tool_approval";
      request: ToolApprovalRequestPayload;
      response?: ToolApprovalResponsePayload;
      calls: Array<{ id: string; name: string; args: Record<string, unknown> }>;
      subagentName?: string;
    };

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

export function buildHitlCard(request: HitlRequestPayload, response?: HitlResponsePayload): HitlCard {
  if (request.type === "ask_user_request") {
    return {
      kind: "ask_user",
      request,
      ...(response?.type === "ask_user_response" && { response }),
      ...(request.nested?.subagent_name && { subagentName: request.nested.subagent_name }),
    };
  }
  return {
    kind: "tool_approval",
    request,
    ...(response?.type === "tool_approval_response" && { response }),
    calls: visibleHitlTools(request).map(tool => ({ id: tool.call_id, name: tool.name, args: tool.args })),
    ...(request.nested?.subagent_name && { subagentName: request.nested.subagent_name }),
  };
}

export function getHitlCard(message: { metadata?: unknown }): HitlCard | undefined {
  const card = (message.metadata as { hitlCard?: unknown } | undefined)?.hitlCard;
  return isRecord(card) && (card.kind === "ask_user" || card.kind === "tool_approval") ? card as HitlCard : undefined;
}

export const isHitlCardResolved = (card: HitlCard): boolean => card.response !== undefined;

/**
 * Opaque id the client must return on ask_user_response.
 * Nested pauses use the child id from nested.tools; direct pauses use request.id.
 */
export function askUserResponseId(request: AskUserRequestPayload): string {
  const nestedAsk = request.nested?.tools?.find(tool => tool.name === "ask_user");
  return nestedAsk?.id ?? request.nested?.tools?.[0]?.id ?? request.id;
}

/** All tool calls represented by a request, including a remote-agent parent call. */
export function relatedHitlCallIds(request: HitlRequestPayload): Set<string> {
  const tools = request.type === "tool_approval_request" ? request.tools : [];
  return new Set([...tools, ...(request.nested?.tools ?? [])].map(tool => tool.call_id));
}

export function responseMatchesRequest(request: HitlRequestPayload, response: HitlResponsePayload): boolean {
  if (request.type === "ask_user_request") {
    return response.type === "ask_user_response" && response.id === askUserResponseId(request);
  }
  if (response.type !== "tool_approval_response") return false;
  const expectedIds = new Set(visibleHitlTools(request).map(tool => tool.id));
  const responseIds = new Set(response.approvals.map(approval => approval.id));
  return response.approvals.length === expectedIds.size &&
    responseIds.size === expectedIds.size &&
    [...expectedIds].every(id => responseIds.has(id));
}

export type PendingHitl = {
  request: HitlRequestPayload;
  taskId?: string;
  contextId?: string;
};

/** First unresolved HITL card in chat message metadata (live or reloaded). */
export function findPendingHitl(
  messages: Array<{ metadata?: unknown; taskId?: string; contextId?: string }>,
): PendingHitl | undefined {
  for (const msg of messages) {
    const card = getHitlCard(msg);
    if (!card || isHitlCardResolved(card)) continue;
    return { request: card.request, taskId: msg.taskId, contextId: msg.contextId };
  }
  return undefined;
}

export function buildAskUserResponse(
  request: AskUserRequestPayload,
  answers: Array<{ answer: string[] }>,
): AskUserResponsePayload {
  return { type: "ask_user_response", id: askUserResponseId(request), answers };
}

export function buildToolApprovalResponse(
  request: ToolApprovalRequestPayload,
  decisions: Record<string, ToolDecision>,
  reasons: Record<string, string> = {},
): ToolApprovalResponsePayload {
  return {
    type: "tool_approval_response",
    approvals: visibleHitlTools(request).map(tool => {
      const approved = decisions[tool.call_id] === "approve";
      return {
        id: tool.id,
        approved,
        ...(!approved && reasons[tool.call_id] ? { rejection_reason: reasons[tool.call_id] } : {}),
      };
    }),
  };
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
