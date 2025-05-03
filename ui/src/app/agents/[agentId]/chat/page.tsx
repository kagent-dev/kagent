import ChatInterface from "@/components/chat/ChatInterface";

// This page component receives props (like params) from the Layout
export default function ChatAgentPage({ params }: { params: { agentId: number } }) {
  const { agentId } = params;

  return (
      <ChatInterface selectedAgentId={agentId} />
  );
}