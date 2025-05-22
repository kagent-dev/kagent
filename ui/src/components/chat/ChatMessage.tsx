import { messageUtils } from "@/lib/utils";
import { AgentMessageConfig } from "@/types/datamodel";
import { TruncatableText } from "@/components/chat/TruncatableText";
import LLMCallModal from "@/components/chat/LLMCallModal";
import ToolCallDisplay from "@/components/chat/ToolCallDisplay";
import MemoryQueryDisplay from "./MemoryQueryDisplay";
import KagentLogo from "../kagent-logo";

interface ChatMessageProps {
  message: AgentMessageConfig;
  allMessages: AgentMessageConfig[];
}

export default function ChatMessage({ message, allMessages }: ChatMessageProps) {
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

  const messageBorderColor = isErrorMessage ? "border-l-red-500" : source === "user" ? "border-l-blue-500" : "border-l-violet-500";

  return <div className={`flex items-center gap-2 text-sm border-l-2 py-2 px-4 ${messageBorderColor}`}>
    <div className="flex flex-col gap-1 w-full">
      {source !== "user" ? <div className="flex items-center gap-1">
        <KagentLogo className="w-4 h-4" />
        <div className="text-xs font-bold">{source}</div>
      </div> : <div className="text-xs font-bold">{source}</div>}
      <TruncatableText content={String(content)} className="break-all text-primary-foreground" />
    </div>
  </div>

}
