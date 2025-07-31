import { AgentMessageConfig, TextMessageConfig } from "@/types/datamodel";
import { Message, Task, TaskStatusUpdateEvent, TaskArtifactUpdateEvent, TextPart, Part, DataPart } from "@a2a-js/sdk";
import { convertToUserFriendlyName, messageUtils } from "@/lib/utils";
import { TokenStats } from "@/lib/types";
import { calculateTokenStats } from "@/components/chat/TokenStats";
import { mapA2AStateToStatus } from "@/lib/statusUtils";

export interface ADKMetadata {
  adk_app_name?: string;
  adk_session_id?: string;
  adk_user_id?: string;
  adk_usage_metadata?: {
    totalTokenCount?: number;
    promptTokenCount?: number;
    candidatesTokenCount?: number;
  };
  adk_type?: "function_call" | "function_response";
  adk_author?: string;
  adk_invocation_id?: string;
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


function isTextPart(part: Part): part is TextPart {
  return part.kind === "text";
}

function isDataPart(part: Part): part is DataPart {
  return part.kind === "data";
}

function  getSourceFromMetadata(metadata: ADKMetadata | undefined, context: string = "unknown", fallback: string = "assistant"): string {
  if (metadata?.adk_app_name) {
    return convertToUserFriendlyName(metadata.adk_app_name);
  }
  return fallback;
}

// Helper to safely cast metadata to ADKMetadata
function getADKMetadata(obj: { metadata?: { [k: string]: unknown } }): ADKMetadata | undefined {
  return obj.metadata as ADKMetadata | undefined;
}

export type MessageHandlers = {
  setMessages: (updater: (prev: AgentMessageConfig[]) => AgentMessageConfig[]) => void;
  setIsStreaming: (value: boolean) => void;
  setStreamingContent: (updater: (prev: string) => string) => void;
  setTokenStats: (updater: (prev: TokenStats) => TokenStats) => void;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  setChatStatus?: (status: any) => void;
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
  const handleStreamingMessage = (message: AgentMessageConfig) => {
    if (messageUtils.isStreamingMessage(message)) {
      handlers.setIsStreaming(true);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      handlers.setStreamingContent(prev => prev + (message as any).content);
    }
  };

  const handleErrorMessage = (message: AgentMessageConfig) => {
    handlers.setMessages(prevMessages => [...prevMessages, message]);
  };

  const handleToolCallMessage = (message: AgentMessageConfig) => {
    handlers.setMessages(prevMessages => [...prevMessages, message]);
  };

  const handleA2ATask = (task: Task) => {
    handlers.setIsStreaming(true);
  };

  const handleA2ATaskStatusUpdate = (statusUpdate: TaskStatusUpdateEvent) => {
    try {
      const adkMetadata = getADKMetadata(statusUpdate);

      if (adkMetadata?.adk_usage_metadata) {
        const usage = adkMetadata.adk_usage_metadata;

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
            const displayMessage = {
              type: "TextMessage",
              content: textContent,
              source: getSourceFromMetadata(adkMetadata, "statusUpdate-textMessage", defaultAgentSource)
            } as AgentMessageConfig;

            if (statusUpdate.final) {
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

            if (partMetadata?.adk_type === "function_call") {
              if (handlers.setChatStatus) {
                handlers.setChatStatus("processing_tools");
              }

              const toolData = data as unknown as ToolCallData;
              const toolCallMessage = {
                type: "ToolCallRequestEvent",
                content: [{
                  id: toolData.id,
                  name: toolData.name,
                  arguments: JSON.stringify(toolData.args || {})
                }],
                source: getSourceFromMetadata(adkMetadata, "statusUpdate-toolCall", defaultAgentSource)
              } as AgentMessageConfig;
              handlers.setMessages(prevMessages => [...prevMessages, toolCallMessage]);

            } else if (partMetadata?.adk_type === "function_response") {
              const toolData = data as unknown as ToolResponseData;
              const content = toolData.response?.result?.content || [];
              const textContent = content.map((c: unknown) => {
                if (typeof c === 'object' && c !== null && 'text' in c) {
                  return (c as { text?: string }).text || '';
                }
                return String(c);
              }).join("");

              const toolResponseMessage = {
                type: "ToolCallExecutionEvent",
                content: [{
                  call_id: toolData.id,
                  name: toolData.name,
                  content: textContent,
                  is_error: toolData.response?.isError || false
                }],
                source: getSourceFromMetadata(adkMetadata, "statusUpdate-toolResponse", defaultAgentSource)
              } as AgentMessageConfig;
              handlers.setMessages(prevMessages => [...prevMessages, toolResponseMessage]);
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

    // Try to get metadata from artifactUpdate first, then from artifact as fallback
    let adkMetadata = getADKMetadata(artifactUpdate);
    if (!adkMetadata && artifactUpdate.artifact) {
      console.log("🔄 Trying artifact metadata as fallback");
      adkMetadata = getADKMetadata(artifactUpdate.artifact);
    }
    


    if (adkMetadata?.adk_usage_metadata) {
      const usage = adkMetadata.adk_usage_metadata;

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

            const displayMessage = {
        type: "TextMessage",
        content: artifactText,
        source: getSourceFromMetadata(adkMetadata, "artifact-displayMessage", defaultAgentSource)
      } as AgentMessageConfig;
      handlers.setMessages(prevMessages => [...prevMessages, displayMessage]);
      
      // Add a tool call summary message to mark any pending tool calls as completed
      const toolSummaryMessage = {
        type: "ToolCallSummaryMessage",
        content: "Tool execution completed",
        source: getSourceFromMetadata(adkMetadata, "artifact-toolSummary", defaultAgentSource)
      } as AgentMessageConfig;
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

    const displayMessage = {
      type: "TextMessage",
      content,
      source: message.role === "user" ? "user" : getSourceFromMetadata(message.metadata as ADKMetadata, "a2aMessage", defaultAgentSource)
    } as AgentMessageConfig;

    if (message.role !== "user") {
      handlers.setMessages(prevMessages => [...prevMessages, displayMessage]);
    }
  };

  const handleOtherMessage = (message: AgentMessageConfig) => {
    handlers.setIsStreaming(false);
    handlers.setStreamingContent(() => "");
    handlers.setMessages(prevMessages => [...prevMessages, message]);
  };

  const handleMessageEvent = (message: AgentMessageConfig) => {
    console.log("🔄 Handling message event:", message);
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

    if (messageUtils.isStreamingMessage(message)) {
      handleStreamingMessage(message);
      return;
    }

    if (messageUtils.isErrorMessageContent(message)) {
      handleErrorMessage(message);
      return;
    }

    if (messageUtils.isToolCallRequestEvent(message) ||
      messageUtils.isToolCallExecutionEvent(message) ||
      messageUtils.isToolCallSummaryMessage(message)) {
      handleToolCallMessage(message);
      return;
    }

    handleOtherMessage(message);
  };

  return {
    handleMessageEvent
  };
}; 