"use client";
import { use, useEffect, useState } from "react";
import { LoadingState } from "@/components/LoadingState";
import ChatInterface from "@/components/chat/ChatInterface";

export default function ChatPageView({ params }: { params: Promise<{ agentId: number; chatId: string }> }) {
  const { agentId, chatId } = use(params);
  const [loading, setLoading] = useState(true);

  // useEffect(() => {
  //   const loadData = async (chatId: string) => {
  //     setLoading(true);
  //     try {
  //       await loadChat(chatId);
  //     } catch (e) {
  //       console.error(e);
  //     } finally {
  //       setLoading(false);
  //     }
  //   };

  //   if (chatId) {
  //     loadData(chatId);
  //   } else {
  //     setLoading(false);
  //   }
  // }, [chatId, loadChat]);

  // if (loading) {
  //   return <LoadingState />;
  // }

  return (
    <main className="w-full max-w-6xl mx-auto px-4">
      <ChatInterface selectedAgentId={agentId} selectedSession={null} />
    </main>
  );
}
