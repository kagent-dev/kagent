import { MessageCircleMore, MessageSquare } from "lucide-react";
import { SidebarMenuButton } from "../ui/sidebar";
import Link from "next/link";

export interface EmptyStateProps {
  /** Sandbox agents: one persistent chat; hide “new chat” framing. */
  variant?: "default" | "singleChat";
}

const EmptyState = ({ variant = "default" }: EmptyStateProps) => {
  const isSingle = variant === "singleChat";
  return (
    <div className="h-full flex flex-col items-center justify-center p-8 text-center">
      <div className="bg-primary rounded-full p-4 mb-4">
        <MessageSquare className="h-8 w-8 text-primary-foreground " />
      </div>
      <h3 className="text-lg font-semibold mb-2">
        {isSingle ? "Your conversation" : "No chats yet"}
      </h3>
      <p className="text-sm max-w-[250px] mb-6">
        {isSingle
          ? "This agent keeps a single chat. Messages and history appear here after you send something in the panel."
          : "Start a new conversation to begin using kagent"}
      </p>
      {!isSingle && <ActionButtons hasSessions={false} />}
    </div>
  );
};

interface ActionButtonsProps {
  hasSessions?: boolean;
  currentAgentId?: number;
}
const ActionButtons = ({ hasSessions, currentAgentId }: ActionButtonsProps) => {
  return (
    <div className="px-2 space-y-4">
      {hasSessions && currentAgentId && (
        <Link href={`/agents/${currentAgentId}/chat`}>
          <SidebarMenuButton>
            <MessageCircleMore className="mr-3 h-4 w-4" />
            <span>Start a new chat</span>
          </SidebarMenuButton>
        </Link>
      )}
    </div>
  );
};
export { EmptyState, ActionButtons };
