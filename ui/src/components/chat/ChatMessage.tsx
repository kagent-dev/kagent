import { Message } from "@a2a-js/sdk";
import { TruncatableText } from "@/components/chat/TruncatableText";
import ToolCallDisplay from "@/components/chat/ToolCallDisplay";
import KagentLogo from "../kagent-logo";
import { ThumbsUp, ThumbsDown } from "lucide-react";
import { useState } from "react";
import { FeedbackDialog } from "./FeedbackDialog";
import { toast } from "sonner";
import { convertToUserFriendlyName } from "@/lib/utils";
import { ADKMetadata } from "@/lib/messageHandlers";

interface ChatMessageProps {
  message: Message;
  allMessages: Message[];
  agentContext?: {
    namespace: string;
    agentName: string;
  };
}

export default function ChatMessage({ message, allMessages, agentContext }: ChatMessageProps) {
  const [feedbackDialogOpen, setFeedbackDialogOpen] = useState(false);
  const [isPositiveFeedback, setIsPositiveFeedback] = useState(true);
  
  const textParts = message.parts?.filter(part => part.kind === "text") || [];
  let content = textParts.map(part => (part as any).text).join("");

  const source = message.role === "user" ? "user" : "assistant";
  const messageId = message.messageId;
  
  // Extract agent name from metadata for display
  const getDisplayName = () => {
    if (source === "user") {
      return "user";
    }

    const msgMetadata = message.metadata as ADKMetadata;
    const displaySource = msgMetadata?.displaySource;
    
    if (displaySource && displaySource !== "assistant") {
      return displaySource;
    }
    
    // For stored messages from Task history, try to get kagent_app_name from metadata
    const adkAppName = msgMetadata?.kagent_app_name;
    
    if (adkAppName) {
      return convertToUserFriendlyName(adkAppName);
    }
    
    // Use agent context as fallback for stored messages
    if (agentContext) {
      return `${agentContext.namespace}/${agentContext.agentName.replace(/_/g, "-")}`;
    }
    
    return "assistant"; // final fallback
  };
  
  const displayName = getDisplayName();
  const numericMessageId = messageId ? Math.abs(messageId.split('').reduce((a, b) => {
    a = ((a << 5) - a) + b.charCodeAt(0);
    return a & a;
  }, 0)) : 0;

  if (!message) {
    return null;
  }

  const metadata = message.metadata as ADKMetadata;
  const originalType = metadata?.originalType;
  
  // Check for tool call parts (works for both stored and streaming messages)
  const hasToolCallParts = message.parts?.some(part => {
    if (part.kind === "data" && part.metadata) {
      const partMetadata = part.metadata as ADKMetadata;
      return partMetadata?.kagent_type === "function_call" || partMetadata?.kagent_type === "function_response";
    }
    return false;
  });
  
  // Also check for streaming tool calls via originalType (fallback for streaming messages)
  const isStreamingToolCall = originalType === "ToolCallRequestEvent" || originalType === "ToolCallExecutionEvent";
  
  if (hasToolCallParts || isStreamingToolCall) {
    return <ToolCallDisplay currentMessage={message} allMessages={allMessages} />;
  }

  if (originalType === "ToolCallSummaryMessage") {
    const hasToolCalls = allMessages.some(msg => {
      return msg.parts?.some(part => {
        if (part.kind === "data" && part.metadata) {
          const partMetadata = part.metadata as ADKMetadata;
          return partMetadata?.kagent_type === "function_call" || partMetadata?.kagent_type === "function_response";
        }
        return false;
      });
    });
    
    if (hasToolCalls) {
      return <ToolCallDisplay currentMessage={message} allMessages={allMessages} />;
    }
    return null;
  }

  // Skip empty messages
  if (!content) {
    return null;
  }


  const handleFeedback = (isPositive: boolean) => {
    if (!messageId) {
      console.error("Message ID is undefined, cannot submit feedback.");
      toast.error("Cannot submit feedback: Message ID not found.");
      return;
    }
    setIsPositiveFeedback(isPositive);
    setFeedbackDialogOpen(true);
  };

  const messageBorderColor = source === "user" ? "border-l-blue-500" : "border-l-violet-500";
  return <div className={`flex items-center gap-2 text-sm border-l-2 py-2 px-4 ${messageBorderColor}`}>
    <div className="flex flex-col gap-1 w-full">
      {source !== "user" ? <div className="flex items-center gap-1">
        <KagentLogo className="w-4 h-4" />
        <div className="text-xs font-bold">{displayName}</div>
      </div> : <div className="text-xs font-bold">{displayName}</div>}
      <TruncatableText content={String(content)} className="break-all text-primary-foreground" />
      
      {source !== "user" && messageId !== undefined && (
        <div className="flex mt-2 justify-end items-center gap-2">
           {message.metadata && (metadata as any).created_at && (
            <div className="text-xs text-muted-foreground">
              {new Date(Number((metadata as any).created_at) * 1000).toLocaleString()}
              {(metadata as any).duration != null && (
                <>
                  <span className="mx-1">-</span>
                  {Number((metadata as any).duration).toFixed(2)}s
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

    {messageId && (
      <FeedbackDialog 
        isOpen={feedbackDialogOpen}
        onClose={() => setFeedbackDialogOpen(false)}
        isPositive={isPositiveFeedback}
        messageId={numericMessageId}
      />
    )}
  </div>
}
