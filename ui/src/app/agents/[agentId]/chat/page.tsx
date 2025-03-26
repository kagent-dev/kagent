import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import ChatInterface from "@/components/chat/ChatInterface";

export default async function ChatLayout({ params }: { params: Promise<{ agentId: number }> }) {
  const { agentId } = await params;

  return (
    <SidebarProvider>
      <SessionsSidebar agentId={agentId} />
      <main className="w-full max-w-6xl mx-auto">
        <ChatInterface
          selectedAgentId={agentId}
        />
      </main>
      <AgentDetailsSidebar selectedAgentId={agentId} />
    </SidebarProvider>
  );
}