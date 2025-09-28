import ChatInterface from "@/components/chat/ChatInterface";
import MultiAgentChat from "@/components/chat/MultiAgentChat";
import { use } from 'react';

// This page component receives props (like params) from the Layout
export default function ChatAgentPage({ params }: { params: Promise<{ name: string, namespace: string }> }) {
  const { name, namespace } = use(params);
  if (namespace === 'kagent' && name === 'multiagent') {
    return <MultiAgentChat namespace={namespace} name={name} />;
  }
  return <ChatInterface selectedAgentName={name} selectedNamespace={namespace} />;
}