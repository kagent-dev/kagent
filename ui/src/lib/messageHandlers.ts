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

export function extractTokenStatsFromTasks(tasks: Task[]): TokenStats {
  let maxTotal = 0;
  let maxInput = 0;
  let maxOutput = 0;
  
  for (const task of tasks) {
    if (task.metadata) {
      const metadata = task.metadata as ADKMetadata;
      const usage = metadata.kagent_usage_metadata;
      
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
    result?: {
      content?: Array<{ text?: string } | unknown>;
    };
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

      if (adkMetadata?.kagent_usage_metadata) {
        const usage = adkMetadata.kagent_usage_metadata;

        const tokenStats = {
          total: usage.totalTokenCount || 0,
          input: usage.promptTokenCount || 0,
          output: usage.candidatesTokenCount || 0,
        };
        // Update token stats cumulatively - each A2A event might have incremental usage
        handlers.setTokenStats(prev => ({
          total: Math.max(prev.total, tokenStats.total),
          input: Math.max(prev.input, tokenStats.input),
          output: Math.max(prev.output, tokenStats.output),
        }));
      }

      // If the status update has a message, process it
      if (statusUpdate.status.message) {
        const message = statusUpdate.status.message;

        // Skip user messages to avoid duplicates (they're already shown immediately)
        if (message.role === "user") {
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
              handlers.setStreamingContent(() => textContent);
              if (handlers.setChatStatus) {
                handlers.setChatStatus("generating_response");
              }
            }

                    } else if (isDataPart(part)) {
            const data = part.data;
            const partMetadata = part.metadata as ADKMetadata | undefined;

            if (partMetadata?.kagent_type === "function_call") {
              if (handlers.setChatStatus) {
                handlers.setChatStatus("processing_tools");
              }

              const toolData = data as unknown as ToolCallData;
              const toolCallContent: ProcessedToolCallData[] = [{
                id: toolData.id,
                name: toolData.name,
                args: toolData.args || {}
              }];
              const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);
              const convertedMessage = createMessage(
                "",
                source,
                {
                  originalType: "ToolCallRequestEvent",
                  contextId: statusUpdate.contextId,
                  taskId: statusUpdate.taskId,
                  additionalMetadata: { toolCallData: toolCallContent }
                }
              );
              handlers.setMessages(prevMessages => [...prevMessages, convertedMessage]);

            } else if (partMetadata?.kagent_type === "function_response") {
              const toolData = data as unknown as ToolResponseData;
              const content = toolData.response?.result?.content || [];
              const textContent = content.map((c: unknown) => {
                if (typeof c === 'object' && c !== null && 'text' in c) {
                  return (c as { text?: string }).text || '';
                }
                return String(c);
              }).join("");

              const toolResultContent: ProcessedToolResultData[] = [{
                call_id: toolData.id,
                name: toolData.name,
                content: textContent,
                is_error: toolData.response?.isError || false
              }];
              const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);
              
              const convertedMessage = createMessage(
                "",
                source,
                {
                  originalType: "ToolCallExecutionEvent",
                  contextId: statusUpdate.contextId,
                  taskId: statusUpdate.taskId,
                  additionalMetadata: { toolResultData: toolResultContent }
                }
              );
              handlers.setMessages(prevMessages => [...prevMessages, convertedMessage]);
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
        handlers.setIsStreaming(false);
        handlers.setStreamingContent(() => "");
        if (handlers.setChatStatus) {
          handlers.setChatStatus("ready");
        }
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

    if (adkMetadata?.kagent_usage_metadata) {
      const usage = adkMetadata.kagent_usage_metadata;

      const tokenStats = {
        total: usage.totalTokenCount || 0,
        input: usage.promptTokenCount || 0,
        output: usage.candidatesTokenCount || 0,
      };
      // Update token stats cumulatively for final counts
      handlers.setTokenStats(prev => ({
        total: Math.max(prev.total, tokenStats.total),
        input: Math.max(prev.input, tokenStats.input),
        output: Math.max(prev.output, tokenStats.output),
      }));
    }

    // Add artifact content as final message (artifacts are typically final responses)
    const artifactText = artifactUpdate.artifact.parts.map((part: Part) => {
      // Handle different part types from A2A SDK
      if (isTextPart(part)) {
        return part.text || "";
      } else if (isDataPart(part)) {
        return JSON.stringify(part.data || "");
      } else if (part.kind === "file") {
        return `[File: ${(part as { file?: { name?: string } }).file?.name || "unknown"}]`;
      }
      return String(part);
    }).join("");

    if (artifactUpdate.lastChunk) {
      handlers.setIsStreaming(false);
      handlers.setStreamingContent(() => "");

      const source = getSourceFromMetadata(adkMetadata, defaultAgentSource);
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
    const content = message.parts.map(part => {
      if (isTextPart(part)) {
        return part.text || "";
      } else if (isDataPart(part)) {
        return JSON.stringify(part.data || "");
      } else if (part.kind === "file") {
        return `[File: ${(part as { file?: { name?: string } }).file?.name || "unknown"}]`;
      }
      return "";
    }).join("");

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
    handlers.setIsStreaming(false);
    handlers.setStreamingContent(() => "");
    handlers.setMessages(prevMessages => [...prevMessages, message]);
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