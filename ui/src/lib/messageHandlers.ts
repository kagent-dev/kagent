import {
  type Artifact,
  Message,
  Role,
  TaskState,
  type Part,
  type StreamResponse,
  type Task,
  type TaskArtifactUpdateEvent,
  type TaskStatusUpdateEvent,
} from "@a2a-js/sdk";
import { v4 as uuidv4 } from "uuid";
import {
  convertToUserFriendlyName,
  isAgentToolName,
  isDataPart,
  isTextPart,
  isUserRole,
} from "@/lib/utils";
import type {
  ChatStatus,
  HitlRequestPayload,
  HitlResponsePayload,
  TokenStats,
} from "@/types";
import { mapA2AStateToStatus } from "@/lib/statusUtils";
import {
  buildHitlCard,
  getHitlPayload,
  isHitlRequest,
  isHitlResponse,
  relatedHitlCallIds,
  responseMatchesRequest,
  visibleHitlTools,
} from "@/lib/hitl";

function isInputRequiredState(state: TaskState | undefined): boolean {
  return state === TaskState.TASK_STATE_INPUT_REQUIRED;
}

// A2A v1 dropped status-update `final`; terminal TaskState is the stream end signal.
export function isTerminalTaskState(state: TaskState | undefined): boolean {
  return (
    state === TaskState.TASK_STATE_COMPLETED ||
    state === TaskState.TASK_STATE_CANCELED ||
    state === TaskState.TASK_STATE_FAILED ||
    state === TaskState.TASK_STATE_REJECTED
  );
}

/** Task IDs whose status is terminal — used to derive finished-reply chrome at render time. */
export function collectTerminalTaskIds(tasks: Task[]): Set<string> {
  const ids = new Set<string>();
  for (const task of tasks) {
    if (isTerminalTaskState(task.status?.state)) {
      ids.add(task.id);
    }
  }
  return ids;
}

/** Last usage-bearing artifact stats keyed by task id (for reply token tooltips). */
export function collectTaskTokenStats(tasks: Task[]): Map<string, TokenStats> {
  const statsByTask = new Map<string, TokenStats>();
  for (const task of tasks) {
    const stats = lastArtifactTokenStats(task);
    if (stats) statsByTask.set(task.id, stats);
  }
  return statsByTask;
}

/**
 * True when this assistant text message is the last one for a terminal task.
 * Derived from task status + transcript position — no message metadata writes.
 */
export function isFinishedAssistantReply(
  message: Message,
  allMessages: Message[],
  terminalTaskIds: ReadonlySet<string>,
): boolean {
  if (isUserRole(message.role)) return false;
  const taskId = message.taskId;
  if (!taskId || !terminalTaskIds.has(taskId)) return false;

  const originalType = (message.metadata as ADKMetadata | undefined)?.originalType;
  if (originalType && originalType !== "TextMessage") return false;

  const messageIndex = allMessages.indexOf(message);
  if (messageIndex < 0) return false;

  for (let i = allMessages.length - 1; i >= 0; i--) {
    const candidate = allMessages[i];
    if (candidate.taskId !== taskId || isUserRole(candidate.role)) continue;
    const candidateType = (candidate.metadata as ADKMetadata | undefined)?.originalType;
    if (candidateType && candidateType !== "TextMessage") continue;
    return i === messageIndex;
  }
  return false;
}

// Helper functions for extracting data from stored tasks
export function extractMessagesFromTasks(tasks: Task[]): Message[] {
  const messages: Message[] = [];
  const seenMessageIds = new Set<string>();

  for (const task of tasks) {
    const history = task.history ?? [];
    const historicalHitlByCallId = indexHistoricalHitlByCallId(history);
    const emittedHistoricalHitl = new Set<HistoricalHitl>();

    for (const historyItem of history) {

      // Deduplicate by messageId to avoid showing the same message twice
      if (seenMessageIds.has(historyItem.messageId)) continue;
      seenMessageIds.add(historyItem.messageId);

      // HITL status messages and their user decisions do not have a defined
      // position relative to persisted artifacts. Only ordinary user messages
      // belong in the reloaded transcript; the current pending interaction is
      // reconstructed separately from task.status.message.
      if (isUserDecisionMessage(historyItem)) continue;

      // User messages: push as-is (no tokenStats needed).
      if (isUserRole(historyItem.role)) {
        messages.push(historyItem);
      }
    }

    // A2A task output is persisted as artifacts, not history messages. Process
    // the server-assembled artifacts in their stored order using the same
    // text/tool representation as the live artifact-update path.
    for (const artifact of task.artifacts ?? []) {
      appendArtifactMessages(messages, task, artifact, historicalHitlByCallId, emittedHistoricalHitl);
    }

    // Status messages are control-plane explanations, never task results.
    // Preserve only the states whose status is expected to carry explanatory
    // content; INPUT_REQUIRED confirmations are handled above/separately.
    const statusState = task.status?.state;
    const statusMessage = task.status?.message;
    if (
      statusMessage &&
      (statusState === TaskState.TASK_STATE_FAILED || statusState === TaskState.TASK_STATE_AUTH_REQUIRED)
    ) {
      const content = aggregatePartsToDisplayText(statusMessage.parts);
      if (content) {
        messages.push(createMessage(content, getSourceFromMetadata(statusMessage.metadata as ADKMetadata, "assistant"), {
          originalType: "TextMessage",
          contextId: task.contextId,
          taskId: task.id,
        }));
      }
    }
  }

  return messages;
}

function appendArtifactMessages(
  messages: Message[],
  task: Task,
  artifact: Artifact,
  historicalHitlByCallId: ReadonlyMap<string, HistoricalHitl>,
  emittedHistoricalHitl: Set<HistoricalHitl>,
): void {
  const metadata = artifact.metadata as ADKMetadata | undefined;
  const source = getSourceFromMetadata(metadata, "assistant");
  const tokenStats = getMessageTokenStats(metadata as Record<string, unknown> | undefined);
  let text = "";

  const flushText = () => {
    if (!text) return;
    messages.push(createMessage(text, source, {
      originalType: "TextMessage",
      contextId: task.contextId,
      taskId: task.id,
    }));
    text = "";
  };

  const replaceFunctionCallWithHistoricalHitl = (callId: string | undefined): boolean => {
    if (!callId) return false;
    const hitl = historicalHitlByCallId.get(callId);
    if (!hitl) return false;
    if (!emittedHistoricalHitl.has(hitl)) {
      messages.push(...buildHitlMessagesFromPayload(hitl.requestPayload, task.contextId, task.id, {
        response: hitl.response,
      }));
      emittedHistoricalHitl.add(hitl);
    }
    return true;
  };

  const appendHistoricalHitlBeforeResponse = (callId: string | undefined): void => {
    if (!callId) return;
    const hitl = historicalHitlByCallId.get(callId);
    if (!hitl || emittedHistoricalHitl.has(hitl)) return;
    messages.push(...buildHitlMessagesFromPayload(hitl.requestPayload, task.contextId, task.id, {
      response: hitl.response,
    }));
    emittedHistoricalHitl.add(hitl);
  };

  for (const part of artifact.parts ?? []) {
    if (isTextPart(part)) {
      text += part.content.value || "";
      continue;
    }

    if (isDataPart(part)) {
      const partMetadata = part.metadata as Record<string, unknown> | undefined;
      const partType = getMetadataValue<string>(partMetadata, "type");
      const data = part.content.value as Record<string, unknown> | undefined;

      if (partType === "function_call" && data) {
        const toolData = data as unknown as ToolCallData;
        if (toolData.name === "adk_request_credential") {
          continue;
        }
        flushText();
        if (replaceFunctionCallWithHistoricalHitl(toolData.id)) {
          continue;
        }
        messages.push(createMessage("", source, {
          originalType: "ToolCallRequestEvent",
          contextId: task.contextId,
          taskId: task.id,
          additionalMetadata: {
            toolCallData: [{
              id: toolData.id,
              name: toolData.name,
              args: toolData.args || {},
            }],
            ...(!isAgentToolName(toolData.name) && tokenStats ? { tokenStats } : {}),
          },
        }));
        continue;
      }

      if (partType === "function_response" && data) {
        const toolData = data as unknown as ToolResponseData;
        const responseData = toolData.response as Record<string, unknown> | undefined;
        const responseStatus = responseData?.status as string | undefined;
        const isPendingAgentSession =
          responseStatus === "pending" &&
          isAgentToolName(toolData.name) &&
          typeof responseData?.subagent_session_id === "string";
        if (
          (responseStatus === "confirmation_requested" || responseStatus === "pending") &&
          !isPendingAgentSession
        ) {
          continue;
        }

        flushText();
        // ADK executors may move the long-running function call exclusively
        // into the INPUT_REQUIRED status. Once resolved, anchor that historical
        // HITL interaction immediately before the matching artifact response.
        appendHistoricalHitlBeforeResponse(toolData.id);
        const subagentSessionId = isAgentToolName(toolData.name) &&
          typeof responseData?.subagent_session_id === "string"
          ? responseData.subagent_session_id
          : undefined;
        messages.push(createMessage("", source, {
          originalType: "ToolCallExecutionEvent",
          contextId: task.contextId,
          taskId: task.id,
          additionalMetadata: {
            toolResultData: [{
              call_id: toolData.id,
              name: toolData.name,
              content: normalizeToolResultToText(toolData),
              is_error: toolData.response?.isError || false,
              raw_result: getRawToolResult(toolData),
              ...(subagentSessionId ? { subagent_session_id: subagentSessionId } : {}),
            }],
          },
        }));

        const responseUsage = responseData?.kagent_usage_metadata;
        const childStats = responseUsage
          ? getMessageTokenStats({ kagent_usage_metadata: responseUsage })
          : undefined;
        if (childStats && isAgentToolName(toolData.name)) {
          for (let i = messages.length - 2; i >= 0; i--) {
            const messageMetadata = messages[i].metadata as ADKMetadata | undefined;
            if (
              messageMetadata?.originalType === "ToolCallRequestEvent" &&
              messageMetadata.toolCallData?.some(call => call.id === toolData.id)
            ) {
              messages[i] = {
                ...messages[i],
                metadata: { ...(messages[i].metadata as object || {}), tokenStats: childStats },
              };
              break;
            }
          }
        }
        continue;
      }

      if (data && Object.keys(data).length > 0) {
        try {
          text += JSON.stringify(data);
        } catch {
          text += String(data);
        }
      }
      continue;
    }

    if (part.content?.$case === "raw" || part.content?.$case === "url") {
      text += `[File: ${part.filename || "unknown"}]`;
    }
  }

  flushText();
}

function aggregatePartsToDisplayText(parts: Part[]): string {
  return parts.map((part: Part) => {
    if (isTextPart(part)) {
      return part.content.value || "";
    }
    if (isDataPart(part)) {
      if (isModelInternalDataPart(part.content.value)) {
        return "";
      }
      try {
        return JSON.stringify(part.content.value || "");
      } catch {
        return String(part.content.value);
      }
    }
    if (part.content?.$case === "raw" || part.content?.$case === "url") {
      return `[File: ${part.filename || "unknown"}]`;
    }
    return String(part);
  }).join("");
}

/** Returns true if the message is a user HITL decision (approve/reject) or ask-user answer. */
function isUserDecisionMessage(message: Message): boolean {
  if (!isUserRole(message.role)) return false;
  const payload = getHitlPayload(message);
  return payload !== undefined && isHitlResponse(payload);
}

/**
 * Check tasks for pending HITL requests (task still in input-required state)
 * and create typed hitlCard messages with Approve/Reject buttons.
 *
 * Historical status messages are intentionally not reconstructed here because
 * A2A task snapshots do not define their order relative to artifacts.
 */
export function extractApprovalMessagesFromTasks(tasks: Task[]): { messages: Message[]; hasPendingApproval: boolean } {
  const approvalMessages: Message[] = [];
  let hasPending = false;

  for (const task of tasks) {
    const status = task.status;
    if (!isInputRequiredState(status?.state) || !status?.message) continue;

    const payload = getHitlPayload(status.message as Message);
    if (!payload || !isHitlRequest(payload)) continue;
    approvalMessages.push(...buildHitlMessagesFromPayload(payload, task.contextId, task.id));
    hasPending = true;
  }

  return { messages: approvalMessages, hasPendingApproval: hasPending };
}

function buildApprovalMessageFromPayload(payload: HitlRequestPayload, contextId: string | undefined, taskId: string | undefined, options: BuildApprovalMessageOptions = {}): Message {
  const { response, tokenStats } = options;
  return createMessage("", "agent", {
    contextId,
    taskId,
    additionalMetadata: { hitlCard: buildHitlCard(payload, response), ...(tokenStats && { tokenStats }) },
  });
}

/** Build the parent AgentCall card whose activity panel opens the paused child session. */
function buildNestedAgentCallMessage(payload: HitlRequestPayload, contextId: string | undefined, taskId: string | undefined): Message | undefined {
  const nested = payload.nested;
  if (!nested?.context_id || !nested.subagent_name) return undefined;

  // Nested ask_user leaves the parent agent call in the transcript (its call id
  // is not in relatedHitlCallIds). A synthetic card keyed by payload.id would
  // render a second "Delegating" box beside the real Completed/pending one.
  if (payload.type !== "tool_approval_request") return undefined;

  const parentTool =
    payload.tools.find(tool => tool.name === nested.subagent_name) ??
    payload.tools.find(tool => isAgentToolName(tool.name)) ??
    payload.tools[0];
  return createMessage("", "agent", {
    originalType: "ToolCallRequestEvent",
    contextId,
    taskId,
    additionalMetadata: {
      toolCallData: [{
        id: parentTool?.call_id ?? nested.context_id,
        name: parentTool?.name ?? nested.subagent_name,
        args: parentTool?.args ?? {},
        subagent_session_id: nested.context_id,
      }],
    },
  });
}

function buildHitlMessagesFromPayload(payload: HitlRequestPayload, contextId: string | undefined, taskId: string | undefined, options: BuildApprovalMessageOptions = {}): Message[] {
  const approvalMessage = buildApprovalMessageFromPayload(payload, contextId, taskId, options);
  const agentCallMessage = buildNestedAgentCallMessage(payload, contextId, taskId);
  return agentCallMessage ? [agentCallMessage, approvalMessage] : [approvalMessage];
}

interface HistoricalHitl {
  requestPayload: HitlRequestPayload;
  response: HitlResponsePayload;
}

/** Index resolved history-only HITL interactions by the tool call they govern. */
function indexHistoricalHitlByCallId(history: Message[]): Map<string, HistoricalHitl> {
  const byCallId = new Map<string, HistoricalHitl>();
  for (let i = 0; i < history.length; i++) {
    const requestPayload = getHitlPayload(history[i]);
    if (!requestPayload || !isHitlRequest(requestPayload)) continue;
    const response = history
      .slice(i + 1)
      .map(getHitlPayload)
      .find((payload): payload is HitlResponsePayload =>
        payload !== undefined && isHitlResponse(payload) && responseMatchesRequest(requestPayload, payload));
    if (!response) continue;
    const hitl: HistoricalHitl = { requestPayload, response };
    for (const callId of relatedHitlCallIds(requestPayload)) {
      byCallId.set(callId, hitl);
    }
  }
  return byCallId;
}

interface BuildApprovalMessageOptions {
  response?: HitlResponsePayload;
  tokenStats?: TokenStats;
}

/**
 * Extract token stats from a single message's own metadata (if the message
 * was generated by an LLM call and carries per-call usage).
 */
function getMessageTokenStats(metadata: Record<string, unknown> | undefined): TokenStats | undefined {
  const usage = getMetadataValue<ADKMetadata["kagent_usage_metadata"]>(metadata, "usage_metadata");
  if (!usage) return undefined;
  const prompt = usage.promptTokenCount ?? 0;
  const completion = usage.candidatesTokenCount ?? 0;
  return {
    total: usage.totalTokenCount ?? prompt + completion,
    prompt,
    completion,
  };
}

function lastArtifactTokenStats(task: Task): TokenStats | undefined {
  let last: TokenStats | undefined;
  for (const artifact of task.artifacts ?? []) {
    const stats = getMessageTokenStats(artifact.metadata as Record<string, unknown> | undefined);
    if (stats) last = stats;
  }
  return last;
}

export function extractTokenStatsFromTasks(tasks: Task[]): TokenStats {
  let total = 0, prompt = 0, completion = 0;
  for (const task of tasks) {
    // Per-event artifact usage is cumulative for a task (prompt grows with
    // context). Use the last usage-bearing artifact, not the sum.
    const last = lastArtifactTokenStats(task);
    if (last) {
      total += last.total;
      prompt += last.prompt;
      completion += last.completion;
    }

    for (const artifact of task.artifacts ?? []) {
      for (const part of artifact.parts ?? []) {
        if (!isDataPart(part)) continue;
        const partMetadata = part.metadata as Record<string, unknown> | undefined;
        if (getMetadataValue<string>(partMetadata, "type") !== "function_response") continue;
        const toolData = part.content.value as unknown as ToolResponseData;
        if (!isAgentToolName(toolData.name)) continue;
        const responseUsage = (toolData.response as Record<string, unknown> | undefined)?.kagent_usage_metadata;
        if (!responseUsage) continue;
        const childStats = getMessageTokenStats({ kagent_usage_metadata: responseUsage });
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
  | "ToolCallSummaryMessage";

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
  hitlCard?: import("@/lib/hitl").HitlCard;
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
    [key: string]: unknown;
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
  raw_result?: unknown;
  subagent_session_id?: string;
}

export function getRawToolResult(toolData: ToolResponseData): unknown {
  return toolData.response?.result ?? toolData.response;
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

// Keys that belong to the model protocol rather than to the answer. Gemini
// attaches an encrypted `thoughtSignature` to the parts it returns; it is meant
// to be round-tripped into the next request, never rendered.
const MODEL_INTERNAL_DATA_KEYS = new Set(["thoughtSignature", "thought_signature"]);

// A data part that carries nothing but model-internal keys is not content.
// It reaches the UI unlabeled (no kagent_type metadata), so without this check
// it falls through to the JSON.stringify fallback and lands in the visible
// answer text.
function isModelInternalDataPart(data: unknown): boolean {
  if (!data || typeof data !== "object" || Array.isArray(data)) {
    return false;
  }
  const keys = Object.keys(data as Record<string, unknown>);
  return keys.length > 0 && keys.every(key => MODEL_INTERNAL_DATA_KEYS.has(key));
}

function getSourceFromMetadata(metadata: ADKMetadata | undefined, fallback: string = "assistant"): string {
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
    messageId,
    role: source === "user" ? Role.ROLE_USER : Role.ROLE_AGENT,
    parts: [{
      content: { $case: "text", value: content },
      metadata: undefined,
      filename: "",
      mediaType: "text/plain",
    }],
    contextId: contextId ?? "",
    taskId: taskId ?? "",
    metadata: {
      originalType,
      displaySource: source,
      ...additionalMetadata,
    },
    extensions: [],
    referenceTaskIds: [],
  };
  return message;
}

export type MessageHandlers = {
  setMessages: (updater: (prev: Message[]) => Message[]) => void;
  setIsStreaming: (value: boolean) => void;
  setStreamingContent: (updater: (prev: string) => string) => void;
  setChatStatus?: (status: ChatStatus) => void;
  /** Transient progress text from WORKING status updates (not chat transcript). */
  setStatusMessage?: (message: string | undefined) => void;
  setSessionStats?: (updater: (prev: TokenStats) => TokenStats) => void;
  /**
   * External mutable container for pending turn stats. Pass a ref-like object
   * (`useRef<TokenStats | undefined>(undefined)`) from the component so that
   * `pendingTurnStats` survives re-renders instead of being reset to `undefined`
   * every time `createMessageHandlers` is called.
   */
  pendingTurnStats?: { current: TokenStats | undefined };
  /** Called when a task reaches a terminal A2A status (finished-reply signal). */
  onTerminalTask?: (taskId: string, tokenStats?: TokenStats) => void;
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
  const artifactTextBuffers = new Map<string, {
    text: string;
    source: string;
    contextId: string | undefined;
    taskId: string | undefined;
  }>();

  const updateStreamingArtifactText = () => {
    const content = [...artifactTextBuffers.values()].map(buffer => buffer.text).join("");
    handlers.setIsStreaming(content.length > 0);
    handlers.setStreamingContent(() => content);
  };

  const commitArtifactText = (tokenStats?: TokenStats, artifactId?: string) => {
    const ids = artifactId ? [artifactId] : [...artifactTextBuffers.keys()];
    for (const id of ids) {
      const buffer = artifactTextBuffers.get(id);
      if (!buffer?.text) continue;
      appendMessage(createMessage(buffer.text, buffer.source, {
        originalType: "TextMessage",
        contextId: buffer.contextId,
        taskId: buffer.taskId,
        additionalMetadata: { ...(tokenStats && { tokenStats }) },
      }));
      artifactTextBuffers.delete(id);
    }
    updateStreamingArtifactText();
  };

  const getTokenStatsFromMetadata = (adkMetadata: ADKMetadata | undefined): TokenStats | undefined => {
    return getMessageTokenStats(adkMetadata as Record<string, unknown> | undefined);
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
    artifactTextBuffers.clear();
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
    options?: { setProcessingStatus?: boolean; tokenStats?: TokenStats }
  ) => {
    if (options?.setProcessingStatus && handlers.setChatStatus) {
      handlers.setChatStatus("processing_tools");
    }
    const toolCallContent: ProcessedToolCallData[] = [{
      id: toolData.id,
      name: toolData.name,
      args: toolData.args || {},
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
      raw_result: getRawToolResult(toolData),
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

  const isUserMessage = (message: Message): boolean => isUserRole(message.role);

  // Simple fallback source when metadata is not available
  const defaultAgentSource = handlers.agentContext
    ? `${handlers.agentContext.namespace}/${handlers.agentContext.agentName.replace(/_/g, "-")}`
    : "assistant";

  const applyTurnStats = (turnStats: TokenStats | undefined) => {
    if (!turnStats) return;

    // Keep the latest usage for this task. Artifact usage is cumulative
    // (prompt grows with context), so mid-stream values must not be summed.
    // Session totals accumulate once on HITL pause / terminal finalize.
    pts.current = turnStats;
    handlers.setMessages(prev => {
      const updated = [...prev];
      for (let i = updated.length - 1; i >= 0; i--) {
        if (isUserRole(updated[i].role)) break;
        const iterMeta = updated[i].metadata as ADKMetadata | undefined;
        const type = iterMeta?.originalType;
        if (iterMeta?.hitlCard) break;
        // Stamp the nearest non-agent tool-call request; text gets stats on terminal status.
        if (type === "ToolCallRequestEvent") {
          if (iterMeta?.toolCallData?.some(tc => isAgentToolName(tc.name))) break;
          updated[i] = { ...updated[i], metadata: { ...(updated[i].metadata as object || {}), tokenStats: turnStats } };
          break;
        }
      }
      return updated;
    });
  };

  const handleA2ATaskStatusUpdate = (statusUpdate: TaskStatusUpdateEvent) => {
    try {
      const adkMetadata = getADKMetadata(statusUpdate);
      const turnStats = getTokenStatsFromMetadata(adkMetadata);

      applyTurnStats(turnStats);

      // Check for tool approval interrupt
      if (isInputRequiredState(statusUpdate.status?.state) && statusUpdate.status?.message) {
        const hitlPayload = getHitlPayload(statusUpdate.status.message as Message);
        if (hitlPayload?.type === "tool_approval_request" || hitlPayload?.type === "ask_user_request") {
          commitArtifactText();
          const callIds = relatedHitlCallIds(hitlPayload);
          if (callIds.size > 0) {
            handlers.setMessages(prev => prev.filter(message => {
              const metadata = message.metadata as ADKMetadata | undefined;
              return metadata?.originalType !== "ToolCallRequestEvent" ||
                !metadata.toolCallData?.some(tool => callIds.has(tool.id));
            }));
          }
          for (const message of buildHitlMessagesFromPayload(hitlPayload, statusUpdate.contextId, statusUpdate.taskId, {
            tokenStats: pts.current ?? turnStats,
          })) {
            appendMessage(message);
          }
          if (pts.current) accumulateSessionStats(pts.current);
          pts.current = undefined;
          handlers.setChatStatus?.("input_required");
          return;
        }
      }

      // Task output is delivered exclusively through artifact updates. Status
      // messages are reserved for progress (WORKING), errors, and auth
      // challenges (INPUT_REQUIRED was handled above).
      const state = statusUpdate.status?.state;
      if (isTerminalTaskState(state)) {
        // Some producers close a run with status only. Commit any artifacts
        // that never received a content-bearing lastChunk before resetting.
        commitArtifactText();
        if (statusUpdate.taskId) {
          handlers.onTerminalTask?.(statusUpdate.taskId, pts.current ?? turnStats);
        }
      }
      const isFailureOrAuth =
        state === TaskState.TASK_STATE_FAILED ||
        state === TaskState.TASK_STATE_AUTH_REQUIRED;
      const statusMessage = statusUpdate.status?.message;
      if (isFailureOrAuth && statusMessage && !isUserMessage(statusMessage)) {
        const content = aggregatePartsToDisplayText(statusMessage.parts);
        if (content) {
          appendMessage(createMessage(content, getSourceFromMetadata(adkMetadata, defaultAgentSource), {
            originalType: "TextMessage",
            contextId: statusUpdate.contextId,
            taskId: statusUpdate.taskId,
          }));
        }
      }

      if (state === TaskState.TASK_STATE_WORKING && statusMessage && !isUserMessage(statusMessage)) {
        const progress = aggregatePartsToDisplayText(statusMessage.parts).trim();
        handlers.setStatusMessage?.(progress || undefined);
      } else if (isTerminalTaskState(state) || isFailureOrAuth) {
        handlers.setStatusMessage?.(undefined);
      }

      if (isTerminalTaskState(statusUpdate.status?.state)) {
        finalizeStreaming();
      }
      if (handlers.setChatStatus) {
        handlers.setChatStatus(mapA2AStateToStatus(state));
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

    // Built-in ADK executors attach per-event usage to artifact metadata. Some
    // other producers repeat it on status, so applyTurnStats de-duplicates the
    // adjacent equal values.
    const artifactStats = getTokenStatsFromMetadata(adkMetadata);
    applyTurnStats(artifactStats);
    const turnStats = pts.current ?? artifactStats;

    // Every artifact update may contain output. Preserve wire order: flush
    // buffered text before emitting a tool part from the same update.
    const artifactId = artifactUpdate.artifact?.artifactId || `${artifactUpdate.contextId}:${artifactUpdate.taskId}:artifact`;
    let artifactText = "";
    let append = Boolean(artifactUpdate.append);

    const bufferArtifactText = (chunk: string) => {
      if (!chunk) return;
      const existing = artifactTextBuffers.get(artifactId);
      artifactTextBuffers.set(artifactId, {
        text: append && existing ? existing.text + chunk : chunk,
        source: getSourceFromMetadata(adkMetadata, defaultAgentSource),
        contextId: artifactUpdate.contextId,
        taskId: artifactUpdate.taskId,
      });
      // Later chunks in this update extend rather than replace.
      append = true;
      updateStreamingArtifactText();
      if (handlers.setChatStatus) {
        handlers.setChatStatus("generating_response");
      }
    };

    const flushBufferedText = () => {
      if (artifactText) {
        bufferArtifactText(artifactText);
        artifactText = "";
      }
      if (artifactTextBuffers.has(artifactId)) {
        commitArtifactText(undefined, artifactId);
      }
    };

    for (const part of artifactUpdate.artifact?.parts ?? []) {
      if (isTextPart(part)) {
        artifactText += part.content.value || "";
        continue;
      }
      if (isDataPart(part)) {
        const partMetadata = part.metadata as ADKMetadata | undefined;
        const data = part.content.value;
        const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);

        const partType = getMetadataValue<string>(partMetadata as Record<string, unknown>, "type");
        if (partType === "function_call") {
          const toolData = data as unknown as ToolCallData;
          if (toolData.name === "adk_request_credential") {
            continue;
          }
          flushBufferedText();
          processFunctionCallPart(toolData, artifactUpdate.contextId, artifactUpdate.taskId, source, {
            setProcessingStatus: true,
            tokenStats: isAgentToolName(toolData.name) ? undefined : turnStats,
          });
          continue;
        }

        if (partType === "function_response") {
          const toolData = data as unknown as ToolResponseData;
          const responseData = toolData.response as Record<string, unknown> | undefined;
          const responseStatus = responseData?.status as string | undefined;
          const isPendingAgentSession =
            responseStatus === "pending" &&
            isAgentToolName(toolData.name) &&
            typeof responseData?.subagent_session_id === "string";
          if (
            (responseStatus === "confirmation_requested" || responseStatus === "pending") &&
            !isPendingAgentSession
          ) {
            continue;
          }
          flushBufferedText();
          processFunctionResponsePart(toolData, artifactUpdate.contextId, artifactUpdate.taskId, source);
          continue;
        }

        // Empty data parts do not contribute displayable text.
        if (!data || (typeof data === "object" && Object.keys(data).length === 0)) {
          continue;
        }

        // Skip model-internal parts (e.g. Gemini's thoughtSignature), which are
        // protocol state rather than content.
        if (isModelInternalDataPart(data)) {
          continue;
        }

        try {
          artifactText += JSON.stringify(data);
        } catch {
          artifactText += String(data);
        }
        continue;
      }
      if (part.content?.$case === "raw" || part.content?.$case === "url") {
        artifactText += `[File: ${part.filename || "unknown"}]`;
        continue;
      }
      artifactText += String(part);
    }

    if (artifactText) {
      bufferArtifactText(artifactText);
    }

    if (artifactUpdate.lastChunk) {
      if (artifactTextBuffers.has(artifactId)) {
        // Intermediate text commits have no feedback chrome; terminal status
        // marks the finished reply.
        commitArtifactText(undefined, artifactId);
      } else {
        updateStreamingArtifactText();
      }
      return;
    }
  };

  const handleA2AMessage = (message: Message) => {
    const content = aggregatePartsToDisplayText(message.parts);

    if (!isUserRole(message.role)) {
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

  const handleMessageEvent = (streamEvent: StreamResponse) => {
    const payload = streamEvent.payload;
    if (!payload) return;

    switch (payload.$case) {
      case "task":
        handlers.setIsStreaming(true);
        return;
      case "statusUpdate":
        handleA2ATaskStatusUpdate(payload.value);
        return;
      case "artifactUpdate":
        handleA2ATaskArtifactUpdate(payload.value);
        return;
      case "message":
        handleA2AMessage(payload.value);
        return;
      default:
        console.warn("Unknown A2A stream payload:", payload);
        return;
    }
  };

  return {
    handleMessageEvent
  };
};
