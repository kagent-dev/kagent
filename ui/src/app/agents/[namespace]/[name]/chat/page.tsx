import ChatInterface from "@/components/chat/ChatInterface";

export const dynamic = "force-dynamic";

export default async function ChatAgentPage({ params }: { params: Promise<{ name: string, namespace: string }> }) {
  const { name, namespace } = await params;
  const headline = process.env.NEXT_PUBLIC_HEADLINE;
  return <ChatInterface selectedAgentName={name} selectedNamespace={namespace} headline={headline} />;
}
