import { type Message } from "@a2a-js/sdk";
import { TruncatableText } from "@/components/chat/TruncatableText";
import ToolCallDisplay from "@/components/chat/ToolCallDisplay";
import AskUserDisplay from "@/components/chat/AskUserDisplay";
import KagentLogo from "../kagent-logo";
import { ThumbsUp, ThumbsDown } from "lucide-react";
import TokenStatsTooltip from "@/components/chat/TokenStatsTooltip";
import type { TokenStats } from "@/types";
import { useState } from "react";
import { FeedbackDialog } from "./FeedbackDialog";
import { toast } from "sonner";
import { convertToUserFriendlyName, isDataPart, isTextPart, isUserRole } from "@/lib/utils";
import { ADKMetadata, getMetadataValue } from "@/lib/messageHandlers";
import { ToolDecision } from "@/types";
import { getHitlCard } from "@/lib/hitl";
import type { ChatMcpAppTool } from "@/components/chat/ChatMcpAppsContext";

interface ChatMessageProps {
  message: Message;
  allMessages: Message[];
  agentContext?: {
    namespace: string;
    agentName: string;
  };
  /** Derived from terminal task status + last text for that task */
  showReplyActions?: boolean;
  replyTokenStats?: TokenStats;
  onApprove?: (toolCallId: string) => void;
  onReject?: (toolCallId: string, reason?: string) => void;
  onAskUserSubmit?: (answers: Array<{ answer: string[] }>) => void;
  pendingDecisions?: Record<string, ToolDecision>;
  getMcpAppForTool?: (toolName: string) => ChatMcpAppTool | undefined;
  onMcpAppSendMessage?: (text: string) => Promise<void>;
}

export default function ChatMessage({
  message,
  allMessages,
  agentContext,
  showReplyActions = false,
  replyTokenStats,
  onApprove,
  onReject,
  onAskUserSubmit,
  pendingDecisions,
  getMcpAppForTool,
  onMcpAppSendMessage,
}: ChatMessageProps) {
  const [feedbackDialogOpen, setFeedbackDialogOpen] = useState(false);
  const [isPositiveFeedback, setIsPositiveFeedback] = useState(true);

  if (!message) return null;

  const content = message.parts?.filter(isTextPart).map((part) => part.content.value).join("") || "";

  const source = isUserRole(message.role) ? "user" : "assistant";
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

    // For stored messages from Task history, try to get app_name from metadata
    const adkAppName = getMetadataValue<string>(message.metadata as Record<string, unknown>, "app_name");

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

  const metadata = message.metadata as ADKMetadata;
  const originalType = metadata?.originalType;
  const hitlCard = getHitlCard(message);

  // Check for tool call parts (works for both stored and streaming messages)
  const hasToolCallParts = message.parts?.some((part) => {
    if (!isDataPart(part) || !part.metadata) return false;
    const partType = getMetadataValue<string>(part.metadata as Record<string, unknown>, "type");
    return partType === "function_call" || partType === "function_response";
  });

  // Also check for streaming tool calls via originalType (fallback for streaming messages)
  const isStreamingToolCall = originalType === "ToolCallRequestEvent" || originalType === "ToolCallExecutionEvent";

  // Ask-user requests get their own dedicated display component
  if (hitlCard?.kind === "ask_user") {
    return (
      <AskUserDisplay
        questions={hitlCard.request.questions}
        isResolved={!!hitlCard.response}
        resolvedAnswers={hitlCard.response?.answers ?? null}
        onSubmit={onAskUserSubmit}
        subagentName={hitlCard.subagentName}
      />
    );
  }

  // Tool approval requests get routed to ToolCallDisplay with approval callbacks
  if (hitlCard?.kind === "tool_approval") {
    return <ToolCallDisplay
      currentMessage={message}
      allMessages={allMessages}
      onApprove={onApprove}
      onReject={onReject}
      pendingDecisions={pendingDecisions}
      getMcpAppForTool={getMcpAppForTool}
      onMcpAppSendMessage={onMcpAppSendMessage}
    />;
  }

  if (hasToolCallParts || isStreamingToolCall) {
    return <ToolCallDisplay
      currentMessage={message}
      allMessages={allMessages}
      onApprove={onApprove}
      onReject={onReject}
      pendingDecisions={pendingDecisions}
      getMcpAppForTool={getMcpAppForTool}
      onMcpAppSendMessage={onMcpAppSendMessage}
    />;
  }

  if (originalType === "ToolCallSummaryMessage") {
    const hasToolCalls = allMessages.some((msg) =>
      msg.parts?.some((part) => {
        if (!isDataPart(part) || !part.metadata) return false;
        const partType = getMetadataValue<string>(part.metadata as Record<string, unknown>, "type");
        return partType === "function_call" || partType === "function_response";
      }),
    );

    if (hasToolCalls) {
      return <ToolCallDisplay currentMessage={message} allMessages={allMessages} getMcpAppForTool={getMcpAppForTool} onMcpAppSendMessage={onMcpAppSendMessage} />;
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
      {/*
        `break-all` breaks a line inside a word even when the word would have
        fit on the next line ("ModelConfig" renders as "M / odelConfig").
        `overflow-wrap: anywhere` only breaks a word that cannot fit on a line
        of its own, which is what long tool-call ids and URLs need.
      */}
      <TruncatableText content={String(content)} className="[overflow-wrap:anywhere] text-primary-foreground" />
      {source !== "user" && showReplyActions && (
        <div className="flex mt-2 justify-end items-center gap-2">
          {replyTokenStats && <TokenStatsTooltip stats={replyTokenStats} />}
          {messageId !== undefined && (
            <>
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
            </>
          )}
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
