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
import { toast } from "sonner";

function convertToUserFriendlyName(name: string): string {
  if (!name) return "Unknown Source";
  name = name.replace(/__NS__/g, "/");
  return name.replace(/_/g, "-");
}

interface ChatMessageProps {
  message: AgentMessageConfig & { id?: number };
  allMessages: AgentMessageConfig[];
}

export default function ChatMessage({ message, allMessages }: ChatMessageProps) {
  const [feedbackDialogOpen, setFeedbackDialogOpen] = useState(false);
  const [isPositiveFeedback, setIsPositiveFeedback] = useState(true);
  // We try to get the message ID from the message object, if it exists (this is for stored messages/existing sessions)
  // If it doesn't exist, we use ID from the metadata (this is for new messages/in-progress chats)
  const messageId = message.id || message.metadata?.id;

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

  const { source } = message;
  let { content } = message;

  const isErrorMessage = messageUtils.isErrorMessageContent(message);
  if (isErrorMessage) {
    content = message.data.task_result?.stop_reason || "An error occurred";
  }

  // Filter out system messages
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
    if (messageId === undefined) {
      console.error("Message ID is undefined (from message.id), cannot submit feedback.");
      toast.error("Cannot submit feedback: Message ID not found.");
      return;
    }
    setIsPositiveFeedback(isPositive);
    setFeedbackDialogOpen(true);
  };

  const formattedSource = convertToUserFriendlyName(source);
  
  const messageBorderColor = isErrorMessage ? "border-l-red-500" : source === "user" ? "border-l-blue-500" : "border-l-violet-500";
  return <div className={`flex items-center gap-2 text-sm border-l-2 py-2 px-4 ${messageBorderColor}`}>
    <div className="flex flex-col gap-1 w-full">
      {source !== "user" ? <div className="flex items-center gap-1">
        <KagentLogo className="w-4 h-4" />
        <div className="text-xs font-bold">{formattedSource}</div>
      </div> : <div className="text-xs font-bold">{formattedSource}</div>}
      <TruncatableText content={String(content)} className="break-all text-primary-foreground" />
      
      {source !== "user" && messageId !== undefined && (
        <div className="flex mt-2 justify-end items-center gap-2">
           {message.metadata?.created_at && (
            <div className="text-xs text-muted-foreground">
              {new Date(Number(message.metadata.created_at) * 1000).toLocaleString()}
              {message.metadata.duration != null && (
                <>
                  <span className="mx-1">-</span>
                  {Number(message.metadata.duration).toFixed(2)}s
                </>
              )}
            </div>
          )}
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

    {messageId !== undefined && (
      <FeedbackDialog 
        isOpen={feedbackDialogOpen}
        onClose={() => setFeedbackDialogOpen(false)}
        isPositive={isPositiveFeedback}
        messageId={Number(messageId)}
      />
    )}
  </div>
}
