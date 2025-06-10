import React from "react";
import { SidebarProvider } from "@/components/ui/sidebar";
import { ErrorState } from "@/components/ErrorState";
import { getTeam, getTeams } from "@/app/actions/teams";
import { getTools } from "@/app/actions/tools";
import ChatLayoutUI from "@/components/chat/ChatLayoutUI";

async function getData(agentId: number) {
  try {
    const [teamResponse, teamsResponse, toolsResponse] = await Promise.all([
      getTeam(agentId),
      getTeams(),
      getTools()
    ]);

    if (!teamResponse.success || !teamResponse.data) {
      return { error: teamResponse.error || "Agent not found" };
    }
    if (!teamsResponse.success || !teamsResponse.data) {
      return { error: teamsResponse.error || "Failed to fetch agents" };
    }
    if (!toolsResponse.success || !toolsResponse.data) {
      return { error: toolsResponse.error || "Failed to fetch tools" };
    }

    const currentAgent = teamResponse.data;
    const allAgents = teamsResponse.data || [];
    const allTools = toolsResponse.data || [];

    return {
      currentAgent,
      allAgents,
      allTools,
      error: null
    };
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : "An unexpected server error occurred";
    console.error("Error fetching data for chat layout:", errorMessage);
    return { error: errorMessage };
  }
}

export default async function ChatLayout({ children, params }: { children: React.ReactNode, params: { agentId: number } }) {
  const resolvedParams = await params;
  const { agentId } = resolvedParams;
  const { currentAgent, allAgents, allTools, error } = await getData(agentId);

  if (error || !currentAgent) {
    return (
      <main className="w-full max-w-6xl mx-auto px-4 flex items-center justify-center h-screen">
        <ErrorState message={error || "Agent data could not be loaded."} />
      </main>
    );
  }

  return (
    <SidebarProvider style={{
      "--sidebar-width": "350px",
      "--sidebar-width-mobile": "150px",
    } as React.CSSProperties}>
      <ChatLayoutUI
        agentId={agentId}
        currentAgent={currentAgent}
        allAgents={allAgents}
        allTools={allTools}
      >
        {children}
      </ChatLayoutUI>
    </SidebarProvider>
  );
} 