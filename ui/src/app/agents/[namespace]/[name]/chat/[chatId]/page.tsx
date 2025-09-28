"use client";
import { use } from "react";
import ChatInterface from "@/components/chat/ChatInterface";

export default function ChatPageView({ params }: { params: Promise<{ name: string; namespace: string; chatId: string }> }) {
  const { name, namespace, chatId } = use(params);

  return <ChatInterface
    selectedAgentName={name}
    selectedNamespace={namespace}
    sessionId={chatId}
  />;
}
