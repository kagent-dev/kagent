import ChatInterface from "@/components/chat/ChatInterface";

export const dynamic = "force-dynamic";

// This page component receives props (like params) from the Layout
export default async function ChatAgentPage({ params }: { params: Promise<{ name: string, namespace: string }> }) {
  const { name, namespace } = await params;
  const headline = process.env.KAGENT_UI_HEADLINE;
  return <ChatInterface selectedAgentName={name} selectedNamespace={namespace} headline={headline} />;
}
