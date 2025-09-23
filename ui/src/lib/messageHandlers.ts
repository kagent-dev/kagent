import { Message, Task, TaskStatusUpdateEvent, TaskArtifactUpdateEvent, TextPart, Part, DataPart } from "@a2a-js/sdk";
import { v4 as uuidv4 } from "uuid";
import { convertToUserFriendlyName, messageUtils, isAgentToolName } from "@/lib/utils";
import { TokenStats, ChatStatus } from "@/types";
import { mapA2AStateToStatus } from "@/lib/statusUtils";

// Helper functions for extracting data from stored tasks
export function extractMessagesFromTasks(tasks: Task[]): Message[] {
  const messages: Message[] = [];
  const seenMessageIds = new Set<string>();
  
  for (const task of tasks) {
    if (task.history) {
      for (const historyItem of task.history) {
        if (historyItem.kind === "message") {
          // Deduplicate by messageId to avoid showing the same message twice
          if (!seenMessageIds.has(historyItem.messageId)) {
            seenMessageIds.add(historyItem.messageId);
            messages.push(historyItem);
          }
        }
      }
    }
  }
  
  return messages;
}

// getUsageFromWellknownFields gets token usage from metadata fields they could be defined in (kagent_usage_metadata for kagent agents. usage_metadata, token_usage, usage fields for remote agents)
// TODO: Should we instead allow for Remote Agent configuration to define where (assuming if) they define their token usage in metadata - defaulting to kagent_usage_metadata.?
function getUsageFromWellknownFields(metadata: ADKMetadata | undefined): TokenStats | undefined {
  if (!metadata) {
    return undefined;
  }

  const usage = metadata?.kagent_usage_metadata ||
    metadata?.usage_metadata as ADKMetadata["kagent_usage_metadata"] ||
    metadata?.token_usage as ADKMetadata["kagent_usage_metadata"] ||
    metadata?.usage as ADKMetadata["kagent_usage_metadata"] ||
    undefined;

  if (!usage) {
    return undefined;
  }

  return {
    total: usage?.totalTokenCount || 0,
    input: usage?.promptTokenCount || 0,
    output: usage?.candidatesTokenCount || 0,
  };
}

export function extractTokenStatsFromTasks(tasks: Task[]): TokenStats {
  let maxTotal = 0;
  let maxInput = 0;
  let maxOutput = 0;
  
  for (const task of tasks) {
    if (task.metadata) {
      const metadata = task.metadata as ADKMetadata;
      const usage = getUsageFromWellknownFields(metadata);

      if (usage) {
        maxTotal = Math.max(maxTotal, usage.total);
        maxInput = Math.max(maxInput, usage.input);
        maxOutput = Math.max(maxOutput, usage.output);
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
  displaySource?: string;
  toolCallData?: ProcessedToolCallData[];
  toolResultData?: ProcessedToolResultData[];
  [key: string]: unknown; // Allow for additional metadata fields
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
}

// Normalize various tool response result shapes into plain text
export function normalizeToolResultToText(toolData: ToolResponseData): string {
  const result = toolData.response?.result;

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
  if (metadata?.kagent_app_name) {
    return convertToUserFriendlyName(metadata.kagent_app_name);
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
    const usage = getUsageFromWellknownFields(adkMetadata);
    if (!usage) return;
    handlers.setTokenStats(prev => ({
      total: Math.max(prev.total, usage.total),
      input: Math.max(prev.input, usage.input),
      output: Math.max(prev.output, usage.output),
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
    defaultSource: string,
    includeDelegatedPlainMessage: boolean
  ) => {
    const textContent = normalizeToolResultToText(toolData);

    if (isAgentToolName(toolData.name) && includeDelegatedPlainMessage) {
      const delegatedSource = convertToUserFriendlyName(toolData.name);
      const plainMessage = createMessage(
        textContent,
        delegatedSource,
        {
          originalType: "TextMessage",
          contextId,
          taskId
        }
      );
      appendMessage(plainMessage);
    }

    const toolResultContent: ProcessedToolResultData[] = [{
      call_id: toolData.id,
      name: toolData.name,
      content: textContent,
      is_error: toolData.response?.isError || false
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


  const handleA2ATask = (task: Task) => {
    handlers.setIsStreaming(true);
    // TODO: figure out how/if we want to handle tasks separately from messages
  };

  const handleA2ATaskStatusUpdate = (statusUpdate: TaskStatusUpdateEvent) => {
    try {
      const adkMetadata = getADKMetadata(statusUpdate);

      updateTokenStatsFromMetadata(adkMetadata);

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

            if (partMetadata?.kagent_type === "function_call") {
              const toolData = data as unknown as ToolCallData;
              const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);
              processFunctionCallPart(toolData, statusUpdate.contextId, statusUpdate.taskId, source, { setProcessingStatus: true });

            } else if (partMetadata?.kagent_type === "function_response") {
              const toolData = data as unknown as ToolResponseData;
              const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);
              processFunctionResponsePart(toolData, statusUpdate.contextId, statusUpdate.taskId, source, true);
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

        if (partMetadata?.kagent_type === "function_call") {
          const toolData = data as unknown as ToolCallData;
          const toolCallContent: ProcessedToolCallData[] = [{ id: toolData.id, name: toolData.name, args: toolData.args || {} }];
          const convertedMessage = createMessage("", source, { originalType: "ToolCallRequestEvent", contextId: artifactUpdate.contextId, taskId: artifactUpdate.taskId, additionalMetadata: { toolCallData: toolCallContent } });
          convertedMessages.push(convertedMessage);
          continue;
        }

        if (partMetadata?.kagent_type === "function_response") {
          const toolData = data as unknown as ToolResponseData;
          const textContent = normalizeToolResultToText(toolData);
          const toolResultContent: ProcessedToolResultData[] = [{ call_id: toolData.id, name: toolData.name, content: textContent, is_error: toolData.response?.isError || false }];
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
      handleA2ATask(message);
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