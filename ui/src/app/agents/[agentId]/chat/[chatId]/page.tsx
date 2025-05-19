"use client";
import { use } from "react";
import ChatInterface from "@/components/chat/ChatInterface";

export default function ChatPageView({ params }: { params: Promise<{ agentId: number; chatId: string }> }) {
  const { agentId, chatId } = use(params);

  return <ChatInterface
    selectedAgentId={agentId}
    sessionId={chatId}
  />;
}
