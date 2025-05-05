import React from "react";
import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { ErrorState } from "@/components/ErrorState";
import { getTeam, getTeams } from "@/app/actions/teams";
import { getSessions, getSessionRuns } from "@/app/actions/sessions";
import { getTools } from "@/app/actions/tools";
import { SessionWithRuns } from "@/types/datamodel";

async function getData(agentId: number) {
  try {
    const [teamResponse, teamsResponse, sessionsResponse, toolsResponse] = await Promise.all([
      getTeam(agentId),
      getTeams(),
      getSessions(),
      getTools()
    ]);

    if (!teamResponse.success || !teamResponse.data) {
      return { error: teamResponse.error || "Agent not found" };
    }
    if (!teamsResponse.success || !teamsResponse.data) {
      return { error: teamsResponse.error || "Failed to fetch agents" };
    }
    if (!sessionsResponse.success || !sessionsResponse.data) {
      console.log(`No sessions found for agent ${agentId}`);
    }
    if (!toolsResponse.success || !toolsResponse.data) {
      return { error: toolsResponse.error || "Failed to fetch tools" };
    }

    const currentAgent = teamResponse.data;
    const allAgents = teamsResponse.data;
    const allAgentSessionsRaw = sessionsResponse.data || [];
    const allTools = toolsResponse.data;

    const agentSessions = allAgentSessionsRaw.filter(session => session.team_id === agentId);

    const sessionsWithRuns: SessionWithRuns[] = await Promise.all(
      agentSessions.map(async (session) => {
        const runsResponse = await getSessionRuns(String(session.id));
        return { session, runs: runsResponse.data || [] };
      })
    );

    return {
      currentAgent,
      allAgents,
      sessionsWithRuns,
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
  const { agentId } = params;
  const { currentAgent, allAgents, sessionsWithRuns, allTools, error } = await getData(agentId);

  if (error || !currentAgent) {
    return (
        <main className="w-full max-w-6xl mx-auto px-4 flex items-center justify-center h-screen">
            <ErrorState message={error || "Agent data could not be loaded."} />
        </main>
    );
  }

  const safeAllAgents = allAgents || [];
  const safeSessionsWithRuns = sessionsWithRuns || [];
  const safeAllTools = allTools || [];

  return (
    <SidebarProvider>
      <SessionsSidebar
        agentId={agentId}
        currentAgent={currentAgent}
        allAgents={safeAllAgents}
        sessionsWithRuns={safeSessionsWithRuns}
      />
      <main className="w-full max-w-6xl mx-auto px-4">
        {children}
      </main>
      <AgentDetailsSidebar
        selectedAgentId={agentId}
        currentAgent={currentAgent}
        allTools={safeAllTools}
      />
    </SidebarProvider>
  );
} 