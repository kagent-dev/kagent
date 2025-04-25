import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import ChatInterface from "@/components/chat/ChatInterface";
import { ErrorState } from "@/components/ErrorState";
import { getTeam } from "@/app/actions/teams";

export default async function ChatLayout({ params }: { params: Promise<{ agentId: number }> }) {
  const { agentId } = await params;

  try {
    const response = await getTeam(String(agentId));
    
    if (!response.success || !response.data) {
      return <ErrorState message={response.error || "Agent not found"} />;
    }

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
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : "An unexpected error occurred";
    return <ErrorState message={errorMessage} />;
  }
}