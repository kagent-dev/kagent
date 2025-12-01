import ChatInterface from "@/components/chat/ChatInterface";
import { use } from 'react';

// This page component receives props (like params) from the Layout
export default function ChatAgentPage({ params }: { params: Promise<{ name: string, namespace: string }> }) {
  const { name, namespace } = use(params);
  return <ChatInterface selectedAgentName={name} selectedNamespace={namespace} />;
}