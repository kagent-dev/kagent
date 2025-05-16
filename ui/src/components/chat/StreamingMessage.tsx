import { TruncatableText } from "./TruncatableText";
import KagentLogo from "../kagent-logo";

interface StreamingMessageProps {
  content: string;
}

export default function StreamingMessage({ content }: StreamingMessageProps) {
  if (!content) {
    return null;
  }
  return (
    <div className="flex items-center gap-2 text-sm animate-pulse  border-l-violet-500 border-l-2 py-2 px-4">
      <div className="flex flex-col gap-1 w-full">
        <KagentLogo className="w-4 h-4" />
        <TruncatableText content={String(content)} className="break-all" isStreaming={true} />
      </div>
    </div>
  );
}
