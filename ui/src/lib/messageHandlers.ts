import { AgentMessageConfig, TextMessageConfig } from "@/types/datamodel";
import { messageUtils } from "@/lib/utils";
import { TokenStats } from "@/lib/types";
import { calculateTokenStats } from "@/components/chat/TokenStats";

export type MessageHandlers = {
  setMessages: (updater: (prev: AgentMessageConfig[]) => AgentMessageConfig[]) => void;
  setIsStreaming: (value: boolean) => void;
  setStreamingContent: (updater: (prev: string) => string) => void;
  setTokenStats: (updater: (prev: TokenStats) => TokenStats) => void;
};

export const createMessageHandlers = (handlers: MessageHandlers) => {
  const handleStreamingMessage = (message: AgentMessageConfig) => {
    handlers.setIsStreaming(true);
    handlers.setStreamingContent(prev => prev + message.content);
  };

  const handleTextMessage = (message: AgentMessageConfig) => {
    handlers.setTokenStats(prev => calculateTokenStats(prev, message as TextMessageConfig));
    handlers.setIsStreaming(false);
    handlers.setStreamingContent(() => "");
    
    // Only add non-user messages to the messages array
    if (message.source !== "user") {
      handlers.setMessages(prevMessages => [...prevMessages, message]);
    }
  };

  const handleErrorMessage = (message: AgentMessageConfig) => {
    handlers.setMessages(prevMessages => [...prevMessages, message]);
  };

  const handleToolCallMessage = (message: AgentMessageConfig) => {
    handlers.setMessages(prevMessages => [...prevMessages, message]);
  };

  const handleOtherMessage = (message: AgentMessageConfig) => {
    handlers.setIsStreaming(false);
    handlers.setStreamingContent(() => "");
    handlers.setMessages(prevMessages => [...prevMessages, message]);
  };

  const handleMessageEvent = (message: AgentMessageConfig) => {
    if (messageUtils.isStreamingMessage(message)) {
      handleStreamingMessage(message);
      return;
    }

    if (messageUtils.isTextMessageContent(message)) {
      handleTextMessage(message);
      return;
    }

    if (messageUtils.isErrorMessageContent(message)) {
      handleErrorMessage(message);
      return;
    }

    // Handle tool call messages
    if (messageUtils.isToolCallRequestEvent(message) || 
        messageUtils.isToolCallExecutionEvent(message) || 
        messageUtils.isToolCallSummaryMessage(message)) {
      handleToolCallMessage(message);
      return;
    }

    // Handle any other message types
    handleOtherMessage(message);
  };

  return {
    handleMessageEvent
  };
}; 