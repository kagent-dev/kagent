import { Message, Task, TaskStatusUpdateEvent, TaskArtifactUpdateEvent, TextPart, Part, DataPart } from "@a2a-js/sdk";
import { v4 as uuidv4 } from "uuid";
import { convertToUserFriendlyName, isAgentToolName, messageUtils } from "@/lib/utils";
import { ApprovalDecision, AdkRequestConfirmationData, HitlPartInfo, ToolDecision, TokenStats, ChatStatus } from "@/types";
import { mapA2AStateToStatus } from "@/lib/statusUtils";

// Helper functions for extracting data from stored tasks
export function extractMessagesFromTasks(tasks: Task[]): Message[] {
  const messages: Message[] = [];
  const seenMessageIds = new Set<string>();

  for (const task of tasks) {
    if (!task.history) continue;

    // Track the most recent LLM usage seen so far within this task so we can
    // attach it to HITL confirmation cards (which share the same invocation as
    // the preceding function_call but don't carry usage_metadata themselves).
    let lastSeenStats: TokenStats | undefined;

    for (let i = 0; i < task.history.length; i++) {
      const historyItem = task.history[i];
      if (historyItem.kind !== "message") continue;

      // Deduplicate by messageId to avoid showing the same message twice
      if (seenMessageIds.has(historyItem.messageId)) continue;
      seenMessageIds.add(historyItem.messageId);

      // If this history message IS an adk_request_confirmation, replace
      // it with a ToolApprovalRequest card carrying the decision status.
      const confirmationParts = findConfirmationParts(historyItem);
      if (confirmationParts.length > 0) {
        // Find the decision that applies to THIS confirmation (first decision AFTER this message)
        const decision = findDecisionAfterIndex(
          task.history as Array<{ kind?: string; role?: string; parts?: Part[] }>,
          i
        );

        // Skip unresolved confirmations — extractApprovalMessagesFromTasks
        // handles pending ones via task.status.message to avoid duplicates.
        if (!decision) continue;

        for (const confPart of confirmationParts) {
          // Use lastSeenStats: the confirmation message shares an invocation with
          // the preceding function_call message that carries the usage.
          messages.push(buildApprovalMessage(confPart, task.contextId, task.id, decision, lastSeenStats));
        }
        continue;
      }

      // Skip user decision messages — the decision is shown on the
      // approval card itself, not as a separate chat bubble.
      if (isUserDecisionMessage(historyItem)) continue;

      // User messages: push as-is (no tokenStats needed).
      if (historyItem.role === "user") {
        messages.push(historyItem);
        continue;
      }

      // Agent messages: convert function_call / function_response DataParts to
      // the same ToolCallRequestEvent / ToolCallExecutionEvent format produced
      // by the live-stream handlers so the rendering component can display them.
      const msgContextId = historyItem.contextId ?? task.contextId;
      const msgTaskId = historyItem.taskId ?? task.id;
      const source = getSourceFromMetadata(historyItem.metadata as ADKMetadata | undefined, "assistant");
      const msgStats = getMessageTokenStats(historyItem.metadata as Record<string, unknown>);

      if (msgStats) lastSeenStats = msgStats;

      let hasConvertedParts = false;
      for (const part of historyItem.parts ?? []) {
        if (part.kind !== "data") continue;
        const dp = part as DataPart;
        const partMeta = dp.metadata as Record<string, unknown> | undefined;
        const partType = getMetadataValue<string>(partMeta, "type");

        if (partType === "function_call") {
          const fcName = (dp.data as Record<string, unknown>)?.name as string | undefined;
          // Skip ADK internal calls — confirmations are handled above.
          if (fcName === "adk_request_confirmation" || fcName === "adk_request_credential") continue;

          const toolData = dp.data as unknown as ToolCallData;
          // Agent calls get no initial tokenStats; child stats arrive later via
          // the function_response and are stamped on this card below.
          // Regular tool calls use the message's own invocation stats.
          const toolStats = isAgentToolName(toolData.name) ? undefined : msgStats;
          messages.push(createMessage("", source, {
            originalType: "ToolCallRequestEvent",
            contextId: msgContextId,
            taskId: msgTaskId,
            additionalMetadata: {
              toolCallData: [{ id: toolData.id, name: toolData.name, args: (toolData.args as Record<string, unknown>) || {} }],
              ...(toolStats && { tokenStats: toolStats }),
            },
          }));
          hasConvertedParts = true;

        } else if (partType === "function_response") {
          const toolData = dp.data as unknown as ToolResponseData;
          messages.push(createMessage("", source, {
            originalType: "ToolCallExecutionEvent",
            contextId: msgContextId,
            taskId: msgTaskId,
            additionalMetadata: {
              toolResultData: [{
                call_id: toolData.id,
                name: toolData.name,
                content: normalizeToolResultToText(toolData),
                is_error: toolData.response?.isError || false,
              }],
            },
          }));
          hasConvertedParts = true;

          // For agent tools, extract child usage from the response dict and
          // stamp it on the matching ToolCallRequestEvent card.
          if (isAgentToolName(toolData.name)) {
            const responseUsage = (toolData.response as Record<string, unknown> | undefined)?.kagent_usage_metadata;
            if (responseUsage) {
              const agentCallStats = getMessageTokenStats({ kagent_usage_metadata: responseUsage } as Record<string, unknown>);
              if (agentCallStats) {
                for (let j = messages.length - 2; j >= 0; j--) {
                  const prevMeta = messages[j].metadata as ADKMetadata | undefined;
                  if (prevMeta?.originalType === "ToolCallRequestEvent" &&
                      prevMeta?.toolCallData?.some(tc => tc.id === toolData.id)) {
                    messages[j] = { ...messages[j], metadata: { ...(messages[j].metadata as object || {}), tokenStats: agentCallStats } };
                    break;
                  }
                }
              }
            }
          }
        }
      }

      // Text messages (or any message without data parts): push with tokenStats.
      if (!hasConvertedParts) {
        messages.push(msgStats
          ? { ...historyItem, metadata: { ...(historyItem.metadata as object || {}), tokenStats: msgStats } }
          : historyItem
        );
      }
    }
  }

  return messages;
}

/** Returns true if the message is a user HITL decision (approve/reject) or ask-user answer. */
function isUserDecisionMessage(message: Message): boolean {
  if (message.role !== "user" || !message.parts) return false;
  return message.parts.some((p: Part) => {
    if (p.kind !== "data") return false;
    const data = (p as DataPart).data as Record<string, unknown> | undefined;
    return data?.decision_type != null;
  });
}

/**
 * Check tasks for pending ADK confirmation requests (task still in
 * input-required state) and create ToolApprovalRequest messages with
 * Approve/Reject buttons.
 *
 * Resolved approvals are handled inline by extractMessagesFromTasks
 * (inserted at the correct history position with an approved/rejected badge).
 */
export function extractApprovalMessagesFromTasks(tasks: Task[]): { messages: Message[]; hasPendingApproval: boolean } {
  const approvalMessages: Message[] = [];
  let hasPending = false;

  for (const task of tasks) {
    const status = task.status;
    if (status?.state !== "input-required" || !status?.message) continue;

    const confirmationParts = findConfirmationParts(status.message as Message);
    if (confirmationParts.length === 0) continue;

    for (const confPart of confirmationParts) {
      approvalMessages.push(buildApprovalMessage(confPart, task.contextId, task.id));
    }
    hasPending = true;
  }

  return { messages: approvalMessages, hasPendingApproval: hasPending };
}

/** Find adk_request_confirmation DataParts in a message's parts. */
function findConfirmationParts(message: Message): DataPart[] {
  if (!message.parts) return [];
  return message.parts.filter((part: Part) => {
    if (part.kind !== "data") return false;
    const dp = part as DataPart;
    const meta = dp.metadata as Record<string, unknown> | undefined;
    return (
      getMetadataValue<string>(meta, "type") === "function_call" &&
      getMetadataValue<boolean>(meta, "is_long_running") === true &&
      (dp.data as Record<string, unknown>)?.name === "adk_request_confirmation"
    );
  }) as DataPart[];
}

/**
 * Find the user's HITL decision data from task history, starting after a specific index.
 * This ensures we associate the correct decision payload with each specific approval cycle
 * if a task enters input-required multiple times.
 */
function findDecisionAfterIndex(
  history: Array<{ kind?: string; role?: string; parts?: Part[] }>,
  startIndex: number
): Record<string, unknown> | undefined {
  for (let i = startIndex + 1; i < history.length; i++) {
    const item = history[i];
    if (item.kind !== "message" || item.role !== "user" || !item.parts) continue;
    for (const p of item.parts) {
      if (p.kind !== "data") continue;
      const data = (p as DataPart).data as Record<string, unknown> | undefined;
      if (data?.decision_type != null) {
        return data;
      }
    }
  }
  return undefined;
}

/**
 * Resolve the decision for a specific tool from the user's decision data.
 * Handles uniform ("approve"/"reject") and batch modes.
 */
function resolveToolDecision(
  decisionData: Record<string, unknown> | undefined,
  toolId: string
): ToolDecision | undefined {
  if (!decisionData) return undefined;
  const decisionType = decisionData.decision_type as string;

  if (decisionType === "batch") {
    const decisions = decisionData.decisions as Record<string, ToolDecision> | undefined;
    return decisions?.[toolId];
  }

  // Uniform decision — applies to all tools
  return decisionType as ToolDecision;
}

/**
 * Build a confirmation message from an adk_request_confirmation DataPart.
 * Branches on the original function call name:
 *   - "ask_user" → AskUserRequest message
 *   - everything else → ToolApprovalRequest message
 */
export function buildApprovalMessage(
  confPart: DataPart,
  contextId: string | undefined,
  taskId: string | undefined,
  decisionData?: Record<string, unknown>,
  tokenStats?: TokenStats
): Message {
  const data = confPart.data as unknown as AdkRequestConfirmationData;
  const origFc = data.args.originalFunctionCall;
  const toolId = origFc.id || data.id;

  // ask_user tool uses a dedicated UI card
  if (origFc.name === "ask_user") {
    // Resolve the user's previous answers (if already resolved)
    const askUserAnswers = decisionData?.ask_user_answers as Array<{ answer: string[] }> | undefined;
    return createMessage("", "agent", {
      originalType: "AskUserRequest",
      contextId,
      taskId,
      additionalMetadata: {
        askUserData: {
          id: toolId,
          questions: (origFc.args as { questions?: unknown }).questions || [],
        },
        // If already resolved, store the answers so the card can show them read-only.
        askUserAnswers: askUserAnswers || null,
        // Track the decision type so we know it was resolved
        approvalDecision: decisionData?.decision_type ? "approve" : undefined,
        ...(tokenStats && { tokenStats }),
      },
    });
  }

  // Check for inner subagent tool details in toolConfirmation.payload.hitl_parts.
  // When a subagent's tool needs approval, KAgentRemoteA2ATool stores the
  // subagent's adk_request_confirmation DataParts in the payload so we can
  // show the actual inner tool(s) instead of the generic "call subagent" request.
  const hitlParts: HitlPartInfo[] | undefined = data.args.toolConfirmation?.payload?.hitl_parts;

  // Subagent ask_user: if the only inner tool is ask_user, render the
  // AskUserDisplay instead of a generic approval card.
  if (hitlParts && hitlParts.length === 1 && hitlParts[0].originalFunctionCall.name === "ask_user") {
    const innerFc = hitlParts[0].originalFunctionCall;
    const innerToolId = innerFc.id || hitlParts[0].id || toolId;
    const askUserAnswers = decisionData?.ask_user_answers as Array<{ answer: string[] }> | undefined;
    const subagentNameForAskUser: string | undefined = data.args.toolConfirmation?.payload?.subagent_name;
    return createMessage("", "agent", {
      originalType: "AskUserRequest",
      contextId,
      taskId,
      additionalMetadata: {
        askUserData: {
          id: innerToolId,
          questions: (innerFc.args as { questions?: unknown }).questions || [],
        },
        askUserAnswers: askUserAnswers || null,
        approvalDecision: decisionData?.decision_type ? "approve" : undefined,
        subagentName: subagentNameForAskUser,
        ...(tokenStats && { tokenStats }),
      },
    });
  }

  let toolCallContent: ProcessedToolCallData[];

  if (hitlParts && hitlParts.length > 0) {
    toolCallContent = hitlParts.map((hp: HitlPartInfo) => ({
      id: hp.originalFunctionCall.id || hp.id || toolId,
      name: hp.originalFunctionCall.name || origFc.name,
      args: hp.originalFunctionCall.args || {},
    }));
  } else {
    toolCallContent = [{
      id: toolId,
      name: origFc.name,
      args: origFc.args || {},
    }];
  }

  // Resolve the approval decision for this message.
  // For subagent HITL with batch decisions, the decision keys are inner tool
  // IDs (not the outer toolId), so return the full per-tool map.
  let approvalDecision: ApprovalDecision | undefined;
  if (hitlParts && hitlParts.length > 0 && decisionData?.decision_type === "batch") {
    approvalDecision = decisionData.decisions as Record<string, ToolDecision> | undefined;
  } else {
    approvalDecision = resolveToolDecision(decisionData, toolId);
  }

  // Extract subagent name if this is a subagent HITL request
  const subagentName: string | undefined = data.args.toolConfirmation?.payload?.subagent_name;

  return createMessage("", "agent", {
    originalType: "ToolApprovalRequest",
    contextId,
    taskId,
    additionalMetadata: {
      toolCallData: toolCallContent,
      approvalDecision,
      subagentName,
      ...(tokenStats && { tokenStats }),
    },
  });
}

/**
 * Extract token stats from a single message's own metadata (if the message
 * was generated by an LLM call and carries per-call usage).
 */
function getMessageTokenStats(metadata: Record<string, unknown> | undefined): TokenStats | undefined {
  const usage = getMetadataValue<ADKMetadata["kagent_usage_metadata"]>(metadata, "usage_metadata");
  if (!usage) return undefined;
  return {
    total: usage.totalTokenCount ?? 0,
    prompt: usage.promptTokenCount ?? 0,
    completion: usage.candidatesTokenCount ?? 0,
  };
}

export function extractTokenStatsFromTasks(tasks: Task[]): TokenStats {
  let total = 0, prompt = 0, completion = 0;
  for (const task of tasks) {
    for (const item of task.history ?? []) {
      const msg = item as unknown as { kind?: string; role?: string; metadata?: Record<string, unknown>; parts?: Part[] };
      if (msg.kind !== "message" || msg.role === "user") continue;

      // Message-level usage (most agent messages carry this).
      const stats = getMessageTokenStats(msg.metadata);
      if (stats) {
        total += stats.total;
        prompt += stats.prompt;
        completion += stats.completion;
      }

      // function_response from agent tools carries child-agent usage inside the
      // response dict rather than in message-level metadata — include it here.
      for (const part of msg.parts ?? []) {
        if (part.kind !== "data") continue;
        const dp = part as DataPart;
        const partMeta = dp.metadata as Record<string, unknown> | undefined;
        if (getMetadataValue<string>(partMeta, "type") !== "function_response") continue;
        const toolData = dp.data as unknown as ToolResponseData;
        if (!isAgentToolName(toolData.name)) continue;
        const responseUsage = (toolData.response as Record<string, unknown> | undefined)?.kagent_usage_metadata;
        if (!responseUsage) continue;
        const childStats = getMessageTokenStats({ kagent_usage_metadata: responseUsage } as Record<string, unknown>);
        if (childStats) {
          total += childStats.total;
          prompt += childStats.prompt;
          completion += childStats.completion;
        }
      }
    }
  }
  return { total, prompt, completion };
}

export type OriginalMessageType =
  | "TextMessage"
  | "ToolCallRequestEvent"
  | "ToolCallExecutionEvent"
  | "ToolCallSummaryMessage"
  | "ToolApprovalRequest"
  | "AskUserRequest";

export interface ADKMetadata {
  kagent_app_name?: string;
  kagent_session_id?: string;
  kagent_user_id?: string;
  kagent_usage_metadata?: {
    totalTokenCount?: number;
    promptTokenCount?: number;
    candidatesTokenCount?: number;
  };
  kagent_type?: "function_call" | "function_response";
  kagent_author?: string;
  kagent_invocation_id?: string;
  originalType?: OriginalMessageType;
  displaySource?: string;
  toolCallData?: ProcessedToolCallData[];
  toolResultData?: ProcessedToolResultData[];
  [key: string]: unknown; // Allow for additional metadata fields
}

/**
 * Read a metadata value checking `adk_<key>` first, then `kagent_<key>`.
 * Allows interoperability with upstream ADK (adk_ prefix) while preserving
 * backward-compatibility with kagent's own kagent_ prefix.
 */
export function getMetadataValue<T = unknown>(
  metadata: Record<string, unknown> | undefined | null,
  key: string
): T | undefined {
  if (!metadata) return undefined;
  const adkKey = `adk_${key}`;
  if (adkKey in metadata) return metadata[adkKey] as T;
  const kagentKey = `kagent_${key}`;
  if (kagentKey in metadata) return metadata[kagentKey] as T;
  return undefined;
}

export interface ToolCallData {
  id: string;
  name: string;
  args?: Record<string, unknown>;
}

export interface ToolResponseData {
  id: string;
  name: string;
  response?: {
    isError?: boolean;
    result?: unknown;
  };
}

// Types for the processed tool call data stored in metadata
export interface ProcessedToolCallData {
  id: string;
  name: string;
  args: Record<string, unknown>;
  subagent_session_id?: string;
}

export interface ProcessedToolResultData {
  call_id: string;
  name: string;
  content: string;
  is_error: boolean;
  subagent_session_id?: string;
}

// Normalize various tool response result shapes into plain text
export function normalizeToolResultToText(toolData: ToolResponseData): string {
  const result = toolData.response?.result || toolData.response;

  if (typeof result === "string") {
    return result;
  }

  if (result && typeof result === "object") {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const anyResult: any = result;
    const content = anyResult?.content;
    if (Array.isArray(content)) {
      return content.map((c: unknown) => {
        if (typeof c === "object" && c !== null && "text" in (c as Record<string, unknown>)) {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          return ((c as any).text as string) || "";
        }
        try {
          return typeof c === "string" ? c : JSON.stringify(c);
        } catch {
          return String(c);
        }
      }).join("");
    }

    if ("text" in anyResult && typeof anyResult.text === "string") {
      return anyResult.text;
    }

    try {
      return JSON.stringify(result);
    } catch {
      return String(result);
    }
  }

  return "";
}

function isTextPart(part: Part): part is TextPart {
  return part.kind === "text";
}

function isDataPart(part: Part): part is DataPart {
  return part.kind === "data";
}

function  getSourceFromMetadata(metadata: ADKMetadata | undefined, fallback: string = "assistant"): string {
  const appName = getMetadataValue<string>(metadata as Record<string, unknown>, "app_name");
  if (appName) {
    return convertToUserFriendlyName(appName);
  }
  return fallback;
}

// Helper to safely cast metadata to ADKMetadata
function getADKMetadata(obj: { metadata?: { [k: string]: unknown } }): ADKMetadata | undefined {
  return obj.metadata as ADKMetadata | undefined;
}

export function createMessage(
  content: string,
  source: string,
  options: {
    messageId?: string;
    originalType?: OriginalMessageType;
    contextId?: string;
    taskId?: string;
    additionalMetadata?: Record<string, unknown>;
  } = {}
): Message {
  const {
    messageId = uuidv4(),
    originalType,
    contextId,
    taskId,
    additionalMetadata = {},
  } = options;

  const message: Message = {
    kind: "message",
    messageId,
    role: source === "user" ? "user" : "agent",
    parts: [{
      kind: "text",
      text: content
    }],
    contextId,
    taskId,
    metadata: {
      originalType,
      displaySource: source,
      ...additionalMetadata
    }
  };
  return message;
}

export type MessageHandlers = {
  setMessages: (updater: (prev: Message[]) => Message[]) => void;
  setIsStreaming: (value: boolean) => void;
  setStreamingContent: (updater: (prev: string) => string) => void;
  setChatStatus?: (status: ChatStatus) => void;
  setSessionStats?: (updater: (prev: TokenStats) => TokenStats) => void;
  /**
   * External mutable container for pending turn stats. Pass a ref-like object
   * (`useRef<TokenStats | undefined>(undefined)`) from the component so that
   * `pendingTurnStats` survives re-renders instead of being reset to `undefined`
   * every time `createMessageHandlers` is called.
   */
  pendingTurnStats?: { current: TokenStats | undefined };
  agentContext?: {
    namespace: string;
    agentName: string;
  };
};

export const createMessageHandlers = (handlers: MessageHandlers) => {
  const appendMessage = (message: Message) => {
    handlers.setMessages(prev => [...prev, message]);
  };

  // Stores the latest usage stats from the current turn.
  // Usage arrives on intermediate status-update events (before the TextMessage
  // is created via artifact update), so we carry it forward here.
  //
  // We use an external ref-like container (if provided) so that the value
  // survives React re-renders between A2A stream events.  If no container is
  // provided we fall back to a local one (fine for tests / non-React usage).
  const pts = handlers.pendingTurnStats ?? { current: undefined as TokenStats | undefined };

  const getTokenStatsFromMetadata = (adkMetadata: ADKMetadata | undefined): TokenStats | undefined => {
    const usage = getMetadataValue<ADKMetadata["kagent_usage_metadata"]>(adkMetadata as Record<string, unknown>, "usage_metadata");
    if (!usage) return undefined;
    return {
      total: usage.totalTokenCount ?? 0,
      prompt: usage.promptTokenCount ?? 0,
      completion: usage.candidatesTokenCount ?? 0,
    };
  };

  const aggregatePartsToText = (parts: Part[]): string => {
    return parts.map((part: Part) => {
      if (isTextPart(part)) {
        return part.text || "";
      } else if (isDataPart(part)) {
        try {
          return JSON.stringify(part.data || "");
        } catch {
          return String(part.data);
        }
      } else if (part.kind === "file") {
        return `[File: ${(part as { file?: { name?: string } }).file?.name || "unknown"}]`;
      }
      return String(part);
    }).join("");
  };

  const accumulateSessionStats = (stats: TokenStats) => {
    if (handlers.setSessionStats) {
      handlers.setSessionStats(prev => ({
        total: prev.total + stats.total,
        prompt: prev.prompt + stats.prompt,
        completion: prev.completion + stats.completion,
      }));
    }
  };

  const finalizeStreaming = () => {
    handlers.setIsStreaming(false);
    handlers.setStreamingContent(() => "");
    if (pts.current) {
      accumulateSessionStats(pts.current);
    }
    pts.current = undefined;
    if (handlers.setChatStatus) {
      handlers.setChatStatus("ready");
    }
  };

  const processFunctionCallPart = (
    toolData: ToolCallData,
    contextId: string | undefined,
    taskId: string | undefined,
    source: string,
    options?: { setProcessingStatus?: boolean; tokenStats?: TokenStats; subagentSessionId?: string }
  ) => {
    if (options?.setProcessingStatus && handlers.setChatStatus) {
      handlers.setChatStatus("processing_tools");
    }
    const toolCallContent: ProcessedToolCallData[] = [{
      id: toolData.id,
      name: toolData.name,
      args: toolData.args || {},
      ...(options?.subagentSessionId ? { subagent_session_id: options.subagentSessionId } : {}),
    }];
    const convertedMessage = createMessage(
      "",
      source,
      {
        originalType: "ToolCallRequestEvent",
        contextId,
        taskId,
        additionalMetadata: { toolCallData: toolCallContent, ...(options?.tokenStats && { tokenStats: options.tokenStats }) }
      }
    );
    appendMessage(convertedMessage);
  };

  const processFunctionResponsePart = (
    toolData: ToolResponseData,
    contextId: string | undefined,
    taskId: string | undefined,
    defaultSource: string
  ) => {
    const content = normalizeToolResultToText(toolData);
    let subagentSessionId: string | undefined;

    if (isAgentToolName(toolData.name)) {
      const responseObj = toolData.response as Record<string, unknown> | undefined;
      if (responseObj && typeof responseObj.subagent_session_id === "string") {
        subagentSessionId = responseObj.subagent_session_id;
      }
    }

    const toolResultContent: ProcessedToolResultData[] = [{
      call_id: toolData.id,
      name: toolData.name,
      content,
      is_error: toolData.response?.isError || false,
      ...(subagentSessionId ? { subagent_session_id: subagentSessionId } : {}),
    }];
    const execEvent = createMessage(
      "",
      defaultSource,
      {
        originalType: "ToolCallExecutionEvent",
        contextId,
        taskId,
        additionalMetadata: { toolResultData: toolResultContent }
      }
    );
    appendMessage(execEvent);

    // If the sub-agent included its own usage metadata in the response dict,
    // tag the matching AgentCall card (ToolCallRequestEvent) with those stats.
    // We match by call ID to be precise regardless of message ordering.
    const responseUsage = (toolData.response as Record<string, unknown> | undefined)?.kagent_usage_metadata;
    if (responseUsage && isAgentToolName(toolData.name)) {
      const agentCallStats = getTokenStatsFromMetadata({ kagent_usage_metadata: responseUsage } as ADKMetadata);
      if (agentCallStats) {
        handlers.setMessages(prev => {
          const updated = [...prev];
          for (let i = updated.length - 1; i >= 0; i--) {
            const msgMeta = updated[i].metadata as ADKMetadata | undefined;
            if (msgMeta?.originalType === "ToolCallRequestEvent" &&
                msgMeta?.toolCallData?.some(tc => tc.id === toolData.id)) {
              updated[i] = { ...updated[i], metadata: { ...(updated[i].metadata as object || {}), tokenStats: agentCallStats } };
              break;
            }
          }
          return updated;
        });
        accumulateSessionStats(agentCallStats);
      }
    }
  };

  const isUserMessage = (message: Message): boolean => message.role === "user";

  // Simple fallback source when metadata is not available
  const defaultAgentSource = handlers.agentContext
    ? `${handlers.agentContext.namespace}/${handlers.agentContext.agentName.replace(/_/g, "-")}`
    : "assistant";

  const handleA2ATaskStatusUpdate = (statusUpdate: TaskStatusUpdateEvent) => {
    try {
      const adkMetadata = getADKMetadata(statusUpdate);
      const turnStats = getTokenStatsFromMetadata(adkMetadata);

      // When usage arrives, retroactively tag all agent messages from the
      // current invocation. The loop stops at an invocation boundary so that
      // messages from earlier LLM calls keep their own stats.
      if (turnStats) {
        // If a previous invocation already deposited stats that DIFFER from the
        // current event's stats, accumulate them before replacing — each LLM
        // call within a turn is independent.
        // When stats are identical the current event is a state transition from
        // the same LLM call (e.g. working→input-required both carry {470}).
        // Accumulating in that case would double-count; the input-required
        // branch handles the single accumulation instead.
        const isNewInvocation = pts.current && (
          pts.current.total !== turnStats.total ||
          pts.current.prompt !== turnStats.prompt ||
          pts.current.completion !== turnStats.completion
        );
        if (isNewInvocation) {
          accumulateSessionStats(pts.current!);
        }
        pts.current = turnStats;
        handlers.setMessages(prev => {
          const updated = [...prev];
          for (let i = updated.length - 1; i >= 0; i--) {
            if (updated[i].role === "user") break;
            // Stop at an invocation boundary — everything before belongs to an
            // earlier LLM call and must not be tagged with this turn's stats.
            // ToolApprovalRequest: HITL boundary; ToolCallExecutionEvent: the
            // tool response that separates two back-to-back LLM invocations.
            const iterMeta = updated[i].metadata as ADKMetadata | undefined;
            const type = iterMeta?.originalType;
            if (type === "ToolApprovalRequest" || type === "ToolCallExecutionEvent") break;
            // AgentCall cards get stats from the child agent's function_response — don't overwrite with parent synthesis stats
            if (type === "ToolCallRequestEvent" && iterMeta?.toolCallData?.some(tc => isAgentToolName(tc.name))) break;
            updated[i] = { ...updated[i], metadata: { ...(updated[i].metadata as object || {}), tokenStats: turnStats } };
          }
          return updated;
        });
      }

      // Check for tool approval interrupt
      if (
        statusUpdate.status.state === "input-required" &&
        statusUpdate.status.message
      ) {
        const confirmationParts = findConfirmationParts(statusUpdate.status.message as Message);

        if (confirmationParts.length > 0) {
          for (const confPart of confirmationParts) {
            // Use pts.current (accumulated turn stats) in preference to turnStats
            // (current event's stats) — the confirmation event often carries no
            // usage_metadata of its own; the stats live in the preceding event.
            appendMessage(buildApprovalMessage(confPart, statusUpdate.contextId, statusUpdate.taskId, undefined, pts.current ?? turnStats));
          }

          // Accumulate this turn's stats now — the HITL interrupt ends the
          // current invocation and the stream will pause until the user decides.
          if (pts.current) {
            accumulateSessionStats(pts.current);
          }
          pts.current = undefined;

          if (handlers.setChatStatus) {
            handlers.setChatStatus("input_required");
          }
          return;
        }
      }

      // If the status update has a message, process it
      if (statusUpdate.status.message) {
        const message = statusUpdate.status.message;

        // Skip user messages to avoid duplicates (they're already shown immediately)
        if (isUserMessage(message)) {
          return;
        }

        for (const part of message.parts) {

          if (isTextPart(part)) {
            const textContent = part.text || "";
            const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);

            if (statusUpdate.final) {
              const displayMessage = createMessage(
                textContent,
                source,
                {
                  originalType: "TextMessage",
                  contextId: statusUpdate.contextId,
                  taskId: statusUpdate.taskId,
                  additionalMetadata: { ...(turnStats && { tokenStats: turnStats }) }
                }
              );
              handlers.setMessages(prevMessages => [...prevMessages, displayMessage]);
              if (handlers.setChatStatus) {
                handlers.setChatStatus("ready");
              }
            } else {
              handlers.setIsStreaming(true);
              handlers.setStreamingContent(prevContent => prevContent + textContent);
              if (handlers.setChatStatus) {
                handlers.setChatStatus("generating_response");
              }
            }
          } else if (isDataPart(part)) {
            const data = part.data;
            const partMetadata = part.metadata as ADKMetadata | undefined;

            const partType = getMetadataValue<string>(partMetadata as Record<string, unknown>, "type");
            if (partType === "function_call") {
              // Skip ADK internal confirmation/auth function calls
              const fcName = (data as Record<string, unknown>)?.name as string | undefined;
              if (fcName === "adk_request_confirmation" || fcName === "adk_request_credential") {
                continue;
              }
              const toolData = data as unknown as ToolCallData;
              const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);

              // Extract subagent_session_id from DataPart metadata for agent tools
              let subagentSessionId: string | undefined;
              if (fcName && isAgentToolName(fcName)) {
                subagentSessionId = getMetadataValue<string>(partMetadata as Record<string, unknown>, "subagent_session_id");
              }

              // Don't stamp AgentCall cards with the parent invocation's stats —
              // those belong on the confirmation dialog. The AgentCall card gets
              // its own stats from the child agent's function_response.
              processFunctionCallPart(toolData, statusUpdate.contextId, statusUpdate.taskId, source, { setProcessingStatus: true, tokenStats: isAgentToolName(toolData.name) ? undefined : turnStats, subagentSessionId });

            } else if (partType === "function_response") {
              // Skip internal HITL markers: the before_tool_callback stub and
              // the ask_user first-invocation pending stub.
              const responseData = (data as { response?: Record<string, unknown> })?.response;
              const responseStatus = responseData?.status as string | undefined;
              if (responseStatus === "confirmation_requested" || responseStatus === "pending") {
                continue;
              }
              const toolData = data as unknown as ToolResponseData;
              const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);
              processFunctionResponsePart(toolData, statusUpdate.contextId, statusUpdate.taskId, source);
            }
          }
        }
      } else {
        if (handlers.setChatStatus) {
          const uiStatus = mapA2AStateToStatus(statusUpdate.status.state);
          handlers.setChatStatus(uiStatus);
        }
      }

      if (statusUpdate.final) {
        finalizeStreaming();
      }
    } catch (error) {
      console.error("❌ Error in handleA2ATaskStatusUpdate:", error);
    }
  };

  const handleA2ATaskArtifactUpdate = (artifactUpdate: TaskArtifactUpdateEvent) => {
    let adkMetadata = getADKMetadata(artifactUpdate);
    if (!adkMetadata && artifactUpdate.artifact) {
      adkMetadata = getADKMetadata(artifactUpdate.artifact);
    }

    // Usage metadata arrives on status-update events, not artifact events.
    const turnStats = pts.current;

    // Add artifact content and convert tool parts to messages
    let artifactText = "";
    const convertedMessages: Message[] = [];
    for (const part of artifactUpdate.artifact.parts) {
      if (isTextPart(part)) {
        artifactText += part.text || "";
        continue;
      }
      if (isDataPart(part)) {
        const partMetadata = part.metadata as ADKMetadata | undefined;
        const data = part.data;
        const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);

        const partType = getMetadataValue<string>(partMetadata as Record<string, unknown>, "type");
        if (partType === "function_call") {
          const toolData = data as unknown as ToolCallData;
          // Extract subagent_session_id from DataPart metadata for agent tools
          let artifactFcSubagentSessionId: string | undefined;
          if (isAgentToolName(toolData.name)) {
            artifactFcSubagentSessionId = getMetadataValue<string>(partMetadata as Record<string, unknown>, "subagent_session_id");
          }
          const toolCallContent: ProcessedToolCallData[] = [{
            id: toolData.id,
            name: toolData.name,
            args: toolData.args || {},
            ...(artifactFcSubagentSessionId ? { subagent_session_id: artifactFcSubagentSessionId } : {}),
          }];
          const convertedMessage = createMessage("", source, { originalType: "ToolCallRequestEvent", contextId: artifactUpdate.contextId, taskId: artifactUpdate.taskId, additionalMetadata: { toolCallData: toolCallContent, ...(turnStats && { tokenStats: turnStats }) } });
          convertedMessages.push(convertedMessage);
          continue;
        }

        if (partType === "function_response") {
          const toolData = data as unknown as ToolResponseData;
          const textContent = normalizeToolResultToText(toolData);
          let artifactSubagentSessionId: string | undefined;
          if (isAgentToolName(toolData.name)) {
            const responseObj = toolData.response as Record<string, unknown> | undefined;
            if (responseObj && typeof responseObj.subagent_session_id === "string") {
              artifactSubagentSessionId = responseObj.subagent_session_id;
            }
          }
          const toolResultContent: ProcessedToolResultData[] = [{
            call_id: toolData.id,
            name: toolData.name,
            content: textContent,
            is_error: toolData.response?.isError || false,
            ...(artifactSubagentSessionId ? { subagent_session_id: artifactSubagentSessionId } : {}),
          }];
          const convertedMessage = createMessage("", source, { originalType: "ToolCallExecutionEvent", contextId: artifactUpdate.contextId, taskId: artifactUpdate.taskId, additionalMetadata: { toolResultData: toolResultContent } });
          convertedMessages.push(convertedMessage);
          continue;
        }

        try {
          artifactText += JSON.stringify(data || "");
        } catch {
          artifactText += String(data);
        }
        continue;
      }
      if (part.kind === "file") {
        artifactText += `[File: ${(part as { file?: { name?: string } }).file?.name || "unknown"}]`;
        continue;
      }
      artifactText += String(part);
    }

    if (artifactUpdate.lastChunk) {
      handlers.setIsStreaming(false);
      handlers.setStreamingContent(() => "");
      // Do NOT accumulate pts.current here — the final status update that always
      // follows a completed artifact will call finalizeStreaming(), which does
      // the single accumulation.  Accumulating here AND in finalizeStreaming()
      // would double-count the last turn's stats.

      const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);
      if (artifactText) {
        const displayMessage = createMessage(
          artifactText,
          source,
          {
            originalType: "TextMessage",
            contextId: artifactUpdate.contextId,
            taskId: artifactUpdate.taskId,
            additionalMetadata: { ...(turnStats && { tokenStats: turnStats }) }
          }
        );
        handlers.setMessages(prevMessages => [...prevMessages, displayMessage]);
      }

      if (convertedMessages.length > 0) {
        handlers.setMessages(prevMessages => [...prevMessages, ...convertedMessages]);
      }

      // Add a tool call summary message to mark any pending tool calls as completed
      const summarySource = getSourceFromMetadata(adkMetadata, defaultAgentSource);
      const toolSummaryMessage = createMessage(
        "Tool execution completed",
        summarySource,
        {
          originalType: "ToolCallSummaryMessage",
          contextId: artifactUpdate.contextId,
          taskId: artifactUpdate.taskId
        }
      );
      handlers.setMessages(prevMessages => [...prevMessages, toolSummaryMessage]);

      if (handlers.setChatStatus) {
        handlers.setChatStatus("ready");
      }
    }
  };

  const handleA2AMessage = (message: Message) => {
    const content = aggregatePartsToText(message.parts);

    if (message.role !== "user") {
      const source = getSourceFromMetadata(message.metadata as ADKMetadata, defaultAgentSource);
      const displayMessage = createMessage(
        content,
        source,
        {
          originalType: "TextMessage",
          contextId: message.contextId,
          taskId: message.taskId
        }
      );
      handlers.setMessages(prevMessages => [...prevMessages, displayMessage]);
    }
  };

  const handleOtherMessage = (message: Message) => {
    finalizeStreaming();
    appendMessage(message);
  };

  const handleMessageEvent = (message: Message) => {
    if (messageUtils.isA2ATask(message)) {
      handlers.setIsStreaming(true);
      return;
    }

    if (messageUtils.isA2ATaskStatusUpdate(message)) {
      handleA2ATaskStatusUpdate(message);
      return;
    }

    if (messageUtils.isA2ATaskArtifactUpdate(message)) {
      handleA2ATaskArtifactUpdate(message);
      return;
    }

    if (messageUtils.isA2AMessage(message)) {
      handleA2AMessage(message);
      return;
    }

    // If we get here, it's an unknown message type from the A2A stream
    console.warn("🤔 Unknown message type from A2A stream:", message);
    handleOtherMessage(message);
  };

  return {
    handleMessageEvent
  };
};
