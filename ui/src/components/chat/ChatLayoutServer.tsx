import { getAgent, getAgents } from "@/app/actions/agents";
import { getServers } from "@/app/actions/servers";
import ChatLayoutUI from "@/components/chat/ChatLayoutUI";
import { ErrorState } from "@/components/ErrorState";
import { SidebarProvider } from "@/components/ui/sidebar";
import { CSSProperties, ReactNode } from "react";

async function getData(agentName: string, namespace: string, agentKind?: string) {
  try {
    const [agentsResponse, serversResponse] = await Promise.all([getAgents(), getServers()]);

    if (agentsResponse.error || !agentsResponse.data) {
      return { error: agentsResponse.error || "Failed to fetch agents" };
    }
    if (serversResponse.error || !serversResponse.data) {
      return { error: serversResponse.error || "Failed to fetch servers" };
    }

    // A route-pinned kind wins; otherwise resolve by namespace/name lookup
    // (which prefers the Agent when kinds share a name).
    const kind =
      agentKind ??
      agentsResponse.data.find(
        (a) =>
          a.agent.metadata?.name === agentName &&
          (a.agent.metadata?.namespace || "") === namespace
      )?.agent.kind;
    const agentResponse = await getAgent(agentName, namespace, kind);

    if (agentResponse.error || !agentResponse.data) {
      return { error: agentResponse.error || "Agent not found" };
    }

    const currentAgent = agentResponse.data;
    const allAgents = agentsResponse.data || [];
    const allTools = serversResponse.data || [];

    return {
      currentAgent,
      allAgents,
      allTools,
      error: null,
    };
  } catch (error) {
    const errorMessage =
      error instanceof Error
        ? error.message
        : "An unexpected server error occurred";
    console.error("Error fetching data for chat layout:", errorMessage);
    return { error: errorMessage };
  }
}

export default async function ChatLayoutServer({
  children,
  params,
  agentKind,
}: {
  children: ReactNode;
  params: Promise<{ name: string; namespace: string }>;
  /** Kind pinned by the route prefix (e.g. "SandboxAgent" under /sandbox-agents); absent resolves via list lookup. */
  agentKind?: string;
}) {
  const { name, namespace } = await params;
  const { currentAgent, allAgents, allTools, error } = await getData(
    name,
    namespace,
    agentKind
  );

  if (error || !currentAgent) {
    return (
      <main className="w-full max-w-6xl mx-auto px-4 flex items-center justify-center h-screen">
        <ErrorState message={error || "Agent data could not be loaded."} />
      </main>
    );
  }

  return (
    <SidebarProvider
      className="!min-h-0 h-full"
      style={
        {
          "--sidebar-width": "350px",
          "--sidebar-width-mobile": "150px",
        } as CSSProperties
      }
    >
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
