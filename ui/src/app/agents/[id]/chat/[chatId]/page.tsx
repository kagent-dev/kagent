"use client";
import { use } from "react";
import { LoadingState } from "@/components/LoadingState";
import ChatInterface from "@/components/chat/ChatInterface";
import { useChatData } from "@/lib/useChatData"; // Adjust path as needed
import { useSessionActions } from "@/lib/useSessionActions";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { SidebarProvider } from "@/components/ui/sidebar";

export default function ChatPageView({ params }: { params: Promise<{ id: string; chatId: string }> }) {
  const { id, chatId } = use(params);
  const [chatData, chatActions] = useChatData({
    agentId: id,
    chatId,
  });

  const { createNewSession } = useSessionActions({
    agentId: id,
    handleNewSession: chatActions.handleNewSession,
  });

  if (chatData.isLoading || !chatData.agent) {
    return <LoadingState />;
  }

  return (
    <SidebarProvider>
      <SessionsSidebar agentId={id} />
      <main className="w-full max-w-6xl mx-auto">
        <ChatInterface selectedTeamId={id} onNewSession={createNewSession} selectedRun={chatData.viewState?.run} selectedSession={chatData.viewState?.session} />;
      </main>
      <AgentDetailsSidebar selectedTeamId={id} />
    </SidebarProvider>
  );
}
