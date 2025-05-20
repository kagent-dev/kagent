import { messageUtils } from "@/lib/utils";
import { AgentMessageConfig } from "@/types/datamodel";
import { TruncatableText } from "@/components/chat/TruncatableText";
import LLMCallModal from "@/components/chat/LLMCallModal";
import ToolCallDisplay from "@/components/chat/ToolCallDisplay";
import MemoryQueryDisplay from "./MemoryQueryDisplay";
import KagentLogo from "../kagent-logo";
import { ThumbsUp, ThumbsDown } from "lucide-react";
import { useState } from "react";
import { FeedbackDialog } from "./FeedbackDialog";

interface ChatMessageProps {
  message: AgentMessageConfig;
  allMessages: AgentMessageConfig[];
  sessionID?: string;
}

export default function ChatMessage({ message, allMessages, sessionID }: ChatMessageProps) {
  const [feedbackDialogOpen, setFeedbackDialogOpen] = useState(false);
  const [isPositiveFeedback, setIsPositiveFeedback] = useState(true);

  if (!message) {
    return null;
  }

  // We also ignroe the task_result messages (for now)
  if (messageUtils.isTaskResultMessage(message) || messageUtils.isStreamingMessage(message) || messageUtils.isCompletionMessage(message)) {
    return null;
  }

  if (messageUtils.isToolCallSummaryMessage(message) || (messageUtils.isToolCallRequestEvent(message) || messageUtils.isToolCallExecutionEvent(message))) {
    return <ToolCallDisplay currentMessage={message} allMessages={allMessages} />;
  }

  let { content, source } = message;

  const isErrorMessage = messageUtils.isErrorMessageContent(message);
  if (isErrorMessage) {
    content = message.data.task_result.stop_reason || "An error occurred";
  }

  // Filter out system messages
  // TODO: Decide whether we want to filter out som agent
  if (source === "system" || source === "user_proxy" || (typeof source === "string" && source.endsWith("society_of_mind_agent"))) {
    return null;
  }

  if (messageUtils.isMemoryQueryEvent(message)) {
    return <MemoryQueryDisplay currentMessage={message} />
  }


  if (messageUtils.isLlmCallEvent(message)) {
    return <LLMCallModal content={String(message)} />;
  }

  const handleFeedback = (isPositive: boolean) => {
    setIsPositiveFeedback(isPositive);
    setFeedbackDialogOpen(true);
  };
  
  // Get all preceding messages for this message
  const getMessageIndex = allMessages.findIndex(m => m === message);
  const precedingMessages = getMessageIndex > 0 ? allMessages.slice(0, getMessageIndex) : [];

  const messageBorderColor = isErrorMessage ? "border-l-red-500" : source === "user" ? "border-l-blue-500" : "border-l-violet-500";
  return <div className={`flex items-center gap-2 text-sm border-l-2 py-2 px-4 ${messageBorderColor}`}>
    <div className="flex flex-col gap-1 w-full">
      {source !== "user" ? <div className="flex items-center gap-1">
        <KagentLogo className="w-4 h-4" />
        <div className="text-xs font-bold">{source}</div>
      </div> : <div className="text-xs font-bold">{source}</div>}
      <TruncatableText content={String(content)} className="break-all text-primary-foreground" />
      
      {/* Feedback buttons - only show for agent responses, not user messages */}
      {source !== "user" && (
        <div className="flex mt-2 justify-end gap-2">
          <button 
            onClick={() => handleFeedback(true)}
            className="p-1 rounded-full hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
            aria-label="Thumbs up"
          >
            <ThumbsUp className="w-4 h-4" />
          </button>
          <button 
            onClick={() => handleFeedback(false)}
            className="p-1 rounded-full hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
            aria-label="Thumbs down"
          >
            <ThumbsDown className="w-4 h-4" />
          </button>
        </div>
      )}
    </div>

    {/* Feedback Dialog */}
    <FeedbackDialog 
      isOpen={feedbackDialogOpen}
      onClose={() => setFeedbackDialogOpen(false)}
      isPositive={isPositiveFeedback}
      message={message}
      precedingMessages={precedingMessages}
      sessionID={sessionID}
    />
  </div>
}
