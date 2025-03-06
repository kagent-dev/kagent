"use client";
import { MessageSquare, Users } from "lucide-react";
import { useRouter } from "next/navigation";
import KagentLogo from "../kagent-logo";

const EmptyState = () => (
  <div className="h-full flex flex-col items-center justify-center p-8 text-center">
    <div className="bg-[#3A3A3A] rounded-full p-4 mb-4">
      <MessageSquare className="h-8 w-8 text-violet-500" />
    </div>
    <h3 className="text-lg font-semibold text-white mb-2">No chats yet</h3>
    <p className="text-sm text-white/50 max-w-[250px] mb-6">{"Start a new conversation to begin using kagent"}</p>
    <ActionButtons hasSessions={false} />
  </div>
);

interface ActionButtonsProps {
  hasSessions?: boolean;
  currentAgentId?: number;
}
const ActionButtons = ({ hasSessions, currentAgentId }: ActionButtonsProps) => {
  const router = useRouter();

  return (
    <div className="px-2 space-y-4">
      {hasSessions && currentAgentId && (
        <button
          onClick={() => {
            router.push(`/agents/${currentAgentId}/chat`);
            router.refresh();
          }}
          className="w-full flex justify-start items-center text-sm font-normal text-white/80 hover:text-white"
        >
          <KagentLogo className="mr-3 h-4 w-4" />
          Start a new chat
        </button>
      )}
      <button onClick={() => router.push("/")} className="w-full flex justify-start items-center text-sm font-normal text-white/80 hover:text-white">
        <Users className="mr-3 h-4 w-4" />
        Switch Agent
      </button>
    </div>
  );
};
export { EmptyState, ActionButtons };
