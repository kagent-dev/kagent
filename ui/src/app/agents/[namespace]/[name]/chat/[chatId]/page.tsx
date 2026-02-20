import ChatInterface from "@/components/chat/ChatInterface";

export const dynamic = "force-dynamic";

export default async function ChatPageView({ params }: { params: Promise<{ name: string; namespace: string; chatId: string }> }) {
  const { name, namespace, chatId } = await params;
  const headline = process.env.NEXT_PUBLIC_HEADLINE;

  return <ChatInterface
    selectedAgentName={name}
    selectedNamespace={namespace}
    sessionId={chatId}
    headline={headline}
  />;
}
