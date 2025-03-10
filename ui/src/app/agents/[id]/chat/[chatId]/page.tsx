"use client";
import { use, useEffect, useState } from "react";
import { LoadingState } from "@/components/LoadingState";
import ChatInterface from "@/components/chat/ChatInterface";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import useChatStore from "@/lib/useChatStore";

export default function ChatPageView({ params }: { params: Promise<{ id: string; chatId: string }> }) {
  const { id, chatId } = use(params);
  const { loadChat, run, session } = useChatStore();
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const loadData = async (chatId: string) => {
      try {
        await loadChat(chatId);
      } catch (e) {
        console.error(e);
      } finally {
        setLoading(false);
      }
    };

    if (chatId) {
      loadData(chatId);
    }
  }, [chatId, loadChat]);

  if (loading) {
    return <LoadingState />;
  }

  return (
    <SidebarProvider>
      <SessionsSidebar agentId={id} />
      <main className="w-full max-w-6xl mx-auto">
        <ChatInterface selectedTeamId={id} selectedRun={run} selectedSession={session} />;
      </main>
      <AgentDetailsSidebar selectedTeamId={id} />
    </SidebarProvider>
  );
}
