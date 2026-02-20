import ChatInterface from "@/components/chat/ChatInterface";

// This page component receives props (like params) from the Layout
export default async function ChatAgentPage({ params }: { params: Promise<{ name: string, namespace: string }> }) {
  const { name, namespace } = await params;
  const headline = process.env.NEXT_PUBLIC_HEADLINE;
  return <ChatInterface selectedAgentName={name} selectedNamespace={namespace} headline={headline} />;
}
