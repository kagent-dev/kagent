"use client";
import { use } from "react";
import { LoadingState } from "@/components/LoadingState";
import ChatInterface from "@/components/chat/ChatInterface";
import { useChatData } from "@/lib/useChatData"; // Adjust path as needed
import { useSessionActions } from "@/lib/useSessionActions";

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

  return <ChatInterface selectedAgentTeam={chatData.agent} onNewSession={createNewSession} selectedRun={chatData.viewState?.run} selectedSession={chatData.viewState?.session} />;
}
