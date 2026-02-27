import { Message, Task, TaskStatusUpdateEvent, TaskArtifactUpdateEvent, TextPart, Part, DataPart } from "@a2a-js/sdk";
import { v4 as uuidv4 } from "uuid";
import { convertToUserFriendlyName, messageUtils } from "@/lib/utils";
import { TokenStats, ChatStatus } from "@/types";
import { mapA2AStateToStatus } from "@/lib/statusUtils";

// Helper functions for extracting data from stored tasks
export function extractMessagesFromTasks(tasks: Task[]): Message[] {
  const messages: Message[] = [];
  const seenMessageIds = new Set<string>();

  for (const task of tasks) {
    if (task.history) {
      // Pre-compute the user's HITL decision for this task (if any) so we
      // can attach it to the reconstructed approval card.
      const decision = findUserDecision(
        task.history as Array<{ kind?: string; role?: string; parts?: Part[] }>
      );

      for (const historyItem of task.history) {
        if (historyItem.kind === "message") {
          // Deduplicate by messageId to avoid showing the same message twice
          if (seenMessageIds.has(historyItem.messageId)) continue;

          // If this history message IS an adk_request_confirmation, replace
          // it with a ToolApprovalRequest card carrying the decision status.
          const confirmationParts = findConfirmationParts(historyItem);
          if (confirmationParts.length > 0) {
            for (const confPart of confirmationParts) {
              const approvalMsg = buildApprovalMessage(confPart, task, decision);
              messages.push(approvalMsg);
            }
            seenMessageIds.add(historyItem.messageId);
            continue;
          }

          // Skip user decision messages — the decision is shown on the
          // approval card itself, not as a separate chat bubble.
          if (isUserDecisionMessage(historyItem)) continue;

          seenMessageIds.add(historyItem.messageId);
          messages.push(historyItem);
        }
      }
    }
  }

  return messages;
}

/** Returns true if the message is a user HITL decision (approve/deny). */
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
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const status = task.status as any;
    if (status?.state !== "input-required" || !status?.message) continue;

    const confirmationParts = findConfirmationParts(status.message as Message);
    if (confirmationParts.length === 0) continue;

    // Safety net: skip if the user already responded (covers pre-taskId-fix
    // data where the response went to a different task).
    if (hasUserDecisionInHistory(task)) continue;

    for (const confPart of confirmationParts) {
      approvalMessages.push(buildApprovalMessage(confPart, task));
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

/** Check if any user message in the task's history contains a decision_type DataPart. */
function hasUserDecisionInHistory(task: Task): boolean {
  return (task.history || []).some((item: { kind?: string; role?: string; parts?: Part[] }) => {
    if (item.kind !== "message" || item.role !== "user" || !item.parts) return false;
    return item.parts.some((p: Part) => {
      if (p.kind !== "data") return false;
      const data = (p as DataPart).data as Record<string, unknown> | undefined;
      return data?.decision_type != null;
    });
  });
}

/** Find the user's approval/rejection decision from task history. Returns "approve" | "deny" | undefined. */
function findUserDecision(history: Array<{ kind?: string; role?: string; parts?: Part[] }>): string | undefined {
  for (const item of history) {
    if (item.kind !== "message" || item.role !== "user" || !item.parts) continue;
    for (const p of item.parts) {
      if (p.kind !== "data") continue;
      const data = (p as DataPart).data as Record<string, unknown> | undefined;
      if (data?.decision_type != null) {
        return data.decision_type as string;
      }
    }
  }
  return undefined;
}

/** Build a ToolApprovalRequest message from a confirmation DataPart. */
function buildApprovalMessage(confPart: DataPart, task: Task, decision?: string): Message {
  const data = confPart.data as {
    name: string;
    args: { originalFunctionCall: { name: string; args: Record<string, unknown>; id?: string } };
    id: string;
  };
  const origFc = data.args.originalFunctionCall;
  const toolCallContent: ProcessedToolCallData[] = [{
    id: origFc.id || data.id,
    name: origFc.name,
    args: origFc.args || {},
  }];
  return createMessage("", "agent", {
    originalType: "ToolApprovalRequest",
    contextId: task.contextId,
    taskId: task.id,
    additionalMetadata: {
      toolCallData: toolCallContent,
      // "approve" | "deny" | undefined — ToolCallDisplay reads this to
      // set the initial status to "approved", "rejected", or "pending_approval".
      approvalDecision: decision,
    },
  });
}

export function extractTokenStatsFromTasks(tasks: Task[]): TokenStats {
  let maxTotal = 0;
  let maxInput = 0;
  let maxOutput = 0;
  
  for (const task of tasks) {
    if (task.metadata) {
      const usage = getMetadataValue<ADKMetadata["kagent_usage_metadata"]>(task.metadata as Record<string, unknown>, "usage_metadata");
      
      if (usage) {
        maxTotal = Math.max(maxTotal, usage.totalTokenCount || 0);
        maxInput = Math.max(maxInput, usage.promptTokenCount || 0);
        maxOutput = Math.max(maxOutput, usage.candidatesTokenCount || 0);
      }
    }
  }
  
  return {
    total: maxTotal,
    input: maxInput,
    output: maxOutput
  };
}

export type OriginalMessageType =
  | "TextMessage"
  | "ToolCallRequestEvent"
  | "ToolCallExecutionEvent"
  | "ToolCallSummaryMessage"
  | "ToolApprovalRequest";

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
}

export interface ProcessedToolResultData {
  call_id: string;
  name: string;
  content: string;
  is_error: boolean;
  /** True when the before_tool_callback rejected this tool call (HITL denial). */
  is_hitl_rejection?: boolean;
}

/** Returns true if a raw ToolResponseData result object represents a HITL rejection. */
export function isHitlRejection(toolData: ToolResponseData): boolean {
  const result = toolData.response?.result;
  return (
    result !== null &&
    typeof result === "object" &&
    (result as Record<string, unknown>).status === "rejected"
  );
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
  setTokenStats: (updater: (prev: TokenStats) => TokenStats) => void;
  setChatStatus?: (status: ChatStatus) => void;
  agentContext?: {
    namespace: string;
    agentName: string;
  };
};

export const createMessageHandlers = (handlers: MessageHandlers) => {
  const appendMessage = (message: Message) => {
    handlers.setMessages(prev => [...prev, message]);
  };

  const updateTokenStatsFromMetadata = (adkMetadata: ADKMetadata | undefined) => {
    const usage = getMetadataValue<ADKMetadata["kagent_usage_metadata"]>(adkMetadata as Record<string, unknown>, "usage_metadata");
    if (!usage) return;
    const tokenStats = {
      total: usage.totalTokenCount || 0,
      input: usage.promptTokenCount || 0,
      output: usage.candidatesTokenCount || 0,
    };
    handlers.setTokenStats(prev => ({
      total: Math.max(prev.total, tokenStats.total),
      input: Math.max(prev.input, tokenStats.input),
      output: Math.max(prev.output, tokenStats.output),
    }));
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

  const finalizeStreaming = () => {
    handlers.setIsStreaming(false);
    handlers.setStreamingContent(() => "");
    if (handlers.setChatStatus) {
      handlers.setChatStatus("ready");
    }
  };

  const processFunctionCallPart = (
    toolData: ToolCallData,
    contextId: string | undefined,
    taskId: string | undefined,
    source: string,
    options?: { setProcessingStatus?: boolean }
  ) => {
    if (options?.setProcessingStatus && handlers.setChatStatus) {
      handlers.setChatStatus("processing_tools");
    }
    const toolCallContent: ProcessedToolCallData[] = [{
      id: toolData.id,
      name: toolData.name,
      args: toolData.args || {}
    }];
    const convertedMessage = createMessage(
      "",
      source,
      {
        originalType: "ToolCallRequestEvent",
        contextId,
        taskId,
        additionalMetadata: { toolCallData: toolCallContent }
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
    const toolResultContent: ProcessedToolResultData[] = [{
      call_id: toolData.id,
      name: toolData.name,
      content: normalizeToolResultToText(toolData),
      is_error: toolData.response?.isError || false,
      is_hitl_rejection: isHitlRejection(toolData),
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
  };

  const isUserMessage = (message: Message): boolean => message.role === "user";

  // Simple fallback source when metadata is not available
  const defaultAgentSource = handlers.agentContext 
    ? `${handlers.agentContext.namespace}/${handlers.agentContext.agentName.replace(/_/g, "-")}`
    : "assistant";

  const handleA2ATaskStatusUpdate = (statusUpdate: TaskStatusUpdateEvent) => {
    try {
      const adkMetadata = getADKMetadata(statusUpdate);

      updateTokenStatsFromMetadata(adkMetadata);

      // Check for tool approval interrupt (ADK-native adk_request_confirmation)
      if (
        statusUpdate.status.state === "input-required" &&
        statusUpdate.status.message
      ) {
        const message = statusUpdate.status.message;

        // Detect ADK-native confirmation requests: long-running function_call DataParts
        // with name "adk_request_confirmation". These contain originalFunctionCall
        // details from which we extract the real tool name/args for the approval UI.
        const confirmationParts = message.parts.filter(
          (part): part is DataPart =>
            isDataPart(part) &&
            getMetadataValue<string>(part.metadata as Record<string, unknown>, "type") === "function_call" &&
            getMetadataValue<boolean>(part.metadata as Record<string, unknown>, "is_long_running") === true &&
            (part.data as Record<string, unknown>)?.name === "adk_request_confirmation"
        );

        if (confirmationParts.length > 0) {
          const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);
          for (const confPart of confirmationParts) {
            const data = confPart.data as {
              name: string;
              args: { originalFunctionCall: { name: string; args: Record<string, unknown>; id?: string } };
              id: string;
            };
            const origFc = data.args.originalFunctionCall;
            // Use the original function call ID (origFc.id) so that the
            // ToolCallDisplay cache merges the approval status into the
            // existing "Call requested" card instead of creating a second card.
            const toolCallContent: ProcessedToolCallData[] = [{
              id: origFc.id || data.id,
              name: origFc.name,
              args: origFc.args || {},
            }];
            const approvalMessage = createMessage("", source, {
              originalType: "ToolApprovalRequest",
              contextId: statusUpdate.contextId,
              taskId: statusUpdate.taskId,
              additionalMetadata: {
                toolCallData: toolCallContent,
              },
            });
            appendMessage(approvalMessage);
          }

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
                  taskId: statusUpdate.taskId
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
              // Skip ADK internal confirmation/auth function calls — they are
              // handled by the approval/auth interrupt detection above and
              // should not be rendered as regular tool call cards.
              const fcName = (data as Record<string, unknown>)?.name as string | undefined;
              if (fcName === "adk_request_confirmation" || fcName === "adk_request_credential") {
                continue;
              }
              const toolData = data as unknown as ToolCallData;
              const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);
              processFunctionCallPart(toolData, statusUpdate.contextId, statusUpdate.taskId, source, { setProcessingStatus: true });

            } else if (partType === "function_response") {
              // Skip the initial confirmation_requested marker from the
              // before_tool_callback (it's not a real tool result — the tool
              // hasn't run yet, it's just the approval request signal).
              const responseData = (data as { response?: Record<string, unknown> })?.response;
              const responseStatus = responseData?.status as string | undefined;
              if (responseStatus === "confirmation_requested") {
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

    updateTokenStatsFromMetadata(adkMetadata);

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
          const toolCallContent: ProcessedToolCallData[] = [{ id: toolData.id, name: toolData.name, args: toolData.args || {} }];
          const convertedMessage = createMessage("", source, { originalType: "ToolCallRequestEvent", contextId: artifactUpdate.contextId, taskId: artifactUpdate.taskId, additionalMetadata: { toolCallData: toolCallContent } });
          convertedMessages.push(convertedMessage);
          continue;
        }

        if (partType === "function_response") {
          const toolData = data as unknown as ToolResponseData;
          const textContent = normalizeToolResultToText(toolData);
          const toolResultContent: ProcessedToolResultData[] = [{ call_id: toolData.id, name: toolData.name, content: textContent, is_error: toolData.response?.isError || false, is_hitl_rejection: isHitlRejection(toolData) }];
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

      const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);
      if (artifactText) {
        const displayMessage = createMessage(
          artifactText,
          source,
          {
            originalType: "TextMessage",
            contextId: artifactUpdate.contextId,
            taskId: artifactUpdate.taskId
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
