import React from "react";
import { SidebarProvider } from "@/components/ui/sidebar";
import { ErrorState } from "@/components/ErrorState";
import { getAgent, getAgents } from "@/app/actions/agents";
import { getServers } from "@/app/actions/servers";
import ChatLayoutUI from "@/components/chat/ChatLayoutUI";

async function getData(agentName: string, namespace: string) {
  try {
    const [agentResponse, agentsResponse, serversResponse] = await Promise.all([
      getAgent(agentName, namespace),
      getAgents(),
      getServers()
    ]);

    if (agentResponse.error || !agentResponse.data) {
      return { error: agentResponse.error || "Agent not found" };
    }
    if (agentsResponse.error || !agentsResponse.data) {
      return { error: agentsResponse.error || "Failed to fetch agents" };
    }
    if (serversResponse.error || !serversResponse.data) {
      return { error: serversResponse.error || "Failed to fetch servers" };
    }

    const currentAgent = agentResponse.data;
    const allAgents = agentsResponse.data || [];
    const allTools = serversResponse.data || [];

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

export default async function ChatLayout({ children, params }: { children: React.ReactNode, params: { name: string, namespace: string } }) {
  const resolvedParams = await params;
  const { name, namespace } = resolvedParams;
  const { currentAgent, allAgents, allTools, error } = await getData(name, namespace);

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
        agentName={name}
        namespace={namespace}
        currentAgent={currentAgent}
        allAgents={allAgents}
        allTools={allTools}
      >
        {children}
      </ChatLayoutUI>
    </SidebarProvider>
  );
} 