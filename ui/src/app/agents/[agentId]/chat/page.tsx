import ChatInterface from "@/components/chat/ChatInterface";
import { use } from 'react';

// This page component receives props (like params) from the Layout
export default function ChatAgentPage({ params }: { params: Promise<{ agentId: number }> }) {
  const { agentId } = use(params);

  return (
      <ChatInterface selectedAgentId={agentId} />
  );
}