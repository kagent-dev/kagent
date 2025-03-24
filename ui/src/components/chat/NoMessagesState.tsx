import { MessageSquarePlus } from "lucide-react";
import { Button } from "../ui/button";
import Link from "next/link";

interface NoMessagesStateProps {
  agentId: number;
}

export default function NoMessagesState({ agentId }: NoMessagesStateProps) {
  return (
    <div className="flex flex-col items-center justify-center px-4 py-12 text-center">
      <MessageSquarePlus className="mb-4 h-8 w-8 text-violet-500" />
      <h3 className="mb-2 text-xl font-medium">Ready to start chatting?</h3>
      <p className="text-base">Begin a new conversation with your agent</p>

      <Button className="mt-4" asChild>
        <Link href={`/agents/${agentId}/chat`}>Start New Chat</Link>
      </Button>
    </div>
  );
}
