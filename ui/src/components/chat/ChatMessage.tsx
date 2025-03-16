import { messageUtils } from "@/lib/utils";
import { Message, Run } from "@/types/datamodel";
import { TruncatableText } from "@/components/chat/TruncatableText";
import LLMCallModal from "@/components/chat/LLMCallModal";
import ToolCallDisplay from "@/components/chat/ToolCallDisplay";

interface ChatMessageProps {
  message: Message;
  run: Run | null;
}

function SingleMessage({ source, message, isStreaming }: { source: string; message: string; isStreaming: boolean }) {
  return (
    <div className={`flex items-center gap-2 text-sm border-l-2 py-2 px-4 ${source === "user" ? "border-l-blue-500" : "border-l-violet-500"}`}>
      <div className="flex flex-col gap-1 w-full">
        <div className="text-xs font-bold">{source}</div>
        <TruncatableText content={message} isStreaming={isStreaming} className="break-all text-primary-foreground" />
      </div>
    </div>
  );
}

export default function ChatMessage({ message, run }: ChatMessageProps) {
  if (!message?.config) {
    return null;
  }

  const { content: messageContent, source } = message.config;

  // Filter out system messages
  // TODO: Decide whether we want to filter out som agent
  if (source === "system" || source === "user_proxy" || source === "society_of_mind_agent") {
    return null;
  }

  // Handle special message types
  if (messageUtils.isTeamResult(message.config)) {
    return (
      <div className="text-sm p-4">
        <span className="font-semibold">Task completed</span>
        <ul className="mt-2">
          <li>Duration: {Math.floor(message.config.duration)} seconds</li>
          <li>Messages sent: {message.config.task_result.messages.length}</li>
        </ul>
      </div>
    );
  }

  if (messageUtils.isFunctionExecutionResult(messageContent) || (messageUtils.isToolCallContent(messageContent) && run)) {
    return <ToolCallDisplay currentMessage={message} currentRun={run} />;
  }

  if (messageUtils.isLlmCallEvent(messageContent)) {
    return <LLMCallModal content={String(messageContent)} />;
  }

  return <SingleMessage source={source} message={String(messageContent)} isStreaming={false} />;
}
