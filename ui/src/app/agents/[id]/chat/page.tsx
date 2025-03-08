import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import ChatInterface from "@/components/chat/ChatInterface";

export default async function ChatLayout({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;

  return (
    <SidebarProvider>
      <SessionsSidebar agentId={id} />
      <main className="w-full max-w-6xl mx-auto">
        <ChatInterface
          selectedTeamId={id}
        />
      </main>
      <AgentDetailsSidebar selectedTeamId={id} />
    </SidebarProvider>
  );
}