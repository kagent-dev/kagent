import { Message } from "@/types/datamodel";
import { TruncatableText } from "./TruncatableText";

interface StreamingMessageProps {
  message: Message;
}

export default function StreamingMessage({ message }: StreamingMessageProps) {
  if (!message?.config) {
    return null;
  }

  const { content: messageContent, source } = message.config;

  // TODO: Decide at some point what we want to filter out
  if (source === "system" || source === "user_proxy" || (typeof source === "string" && source.endsWith("society_of_mind_agent"))) {
    return null;
  }

  return (
    <div className="flex items-center gap-2 text-sm animate-pulse  border-l-violet-500 border-l-2 py-2 px-4">
      <div className="flex flex-col gap-1 w-full">
        <div className="text-xs font-bold">{source}</div>
        <TruncatableText content={String(messageContent)} isStreaming={true} className="break-all" />
      </div>
    </div>
  );
}
