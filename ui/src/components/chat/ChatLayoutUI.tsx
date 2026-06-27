"use client";

import React, { useState, useEffect, useMemo } from "react";
import { usePathname } from "next/navigation";
import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import { getSessionsForAgent } from "@/app/actions/sessions";
import { AgentResponse, Session, RemoteMCPServerResponse, ToolsResponse } from "@/types";
import { toast } from "sonner";
import { ChatAgentProvider } from "@/components/chat/ChatAgentContext";
import { isSubstrateSandboxAgent } from "@/lib/sandboxAgentForm";
import { mergeSessionUpdate, normalizeSessionTimestamps } from "@/lib/sessionTimestamps";

interface ChatLayoutUIProps {
  agentName: string;
  namespace: string;
  currentAgent: AgentResponse;
  allAgents: AgentResponse[];
  allTools: RemoteMCPServerResponse[];
  children: React.ReactNode;
}

export default function ChatLayoutUI({
  agentName,
  namespace,
  currentAgent,
  allAgents,
  allTools,
  children
}: ChatLayoutUIProps) {
  const pathname = usePathname();
  const [sessions, setSessions] = useState<Session[]>([]);
  const [acpSessions, setAcpSessions] = useState<Array<{ sessionId: string; title?: string; updatedAt?: string }>>([]);
  const [isLoadingSessions, setIsLoadingSessions] = useState(true);

  // Convert RemoteMCPServerResponse[] to ToolsResponse[]
  const convertedTools = useMemo(() => {
    const tools: ToolsResponse[] = [];
    allTools.forEach(server => {
      server.discoveredTools.forEach(tool => {
        tools.push({
          id: tool.name,
          server_name: server.ref,
          description: tool.description,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          deleted_at: "",
          group_kind: server.groupKind
        });
      });
    });
    return tools;
  }, [allTools]);


  useEffect(() => {
    const refreshSessions = async () => {
      setIsLoadingSessions(true);
      try {
        const sessionsResponse = await getSessionsForAgent(namespace, agentName);
        if (!sessionsResponse.error && sessionsResponse.data) {
          setSessions(sessionsResponse.data);
        } else {
          console.log(`No sessions found for agent ${agentName}`);
          setSessions([]);
        }
      } catch (error) {
        toast.error(`Failed to load sessions: ${error}`);
        setSessions([]);
      } finally {
        setIsLoadingSessions(false);
      }
    };
    refreshSessions();
    // Same agent may navigate /chat → /chat/:id (e.g. Sandbox); refetch so the sidebar lists the session.
  }, [agentName, namespace, pathname]);

  useEffect(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const handleNewSession = (event: any) => {
      const { agentRef, session } = event.detail;
      // Only update if this is for our current agent (agentRef format: "namespace/agentName")
      const currentAgentRef = `${namespace}/${agentName}`;
      if (agentRef === currentAgentRef && session) {
        const normalized = normalizeSessionTimestamps(session);
        setSessions(prevSessions => {
          const exists = prevSessions.some(s => s.id === normalized.id);
          if (exists) {
            return prevSessions.map(s => (s.id === normalized.id ? mergeSessionUpdate(s, normalized) : s));
          }
          return [normalized, ...prevSessions];
        });
      }
    };

    window.addEventListener('new-session-created', handleNewSession);
    return () => {
      window.removeEventListener('new-session-created', handleNewSession);
    };
  }, [agentName, namespace]);

  useEffect(() => {
    // Listen for ACP harness session list updates from AcpHarnessChat
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const handleAcpSessionsUpdate = (event: any) => {
      const { agentRef, sessions } = event.detail;
      const currentAgentRef = `${namespace}/${agentName}`;
      if (agentRef === currentAgentRef && Array.isArray(sessions)) {
        setAcpSessions(sessions);
      }
    };

    window.addEventListener('acp-sessions-updated', handleAcpSessionsUpdate);
    return () => {
      window.removeEventListener('acp-sessions-updated', handleAcpSessionsUpdate);
    };
  }, [agentName, namespace]);

  useEffect(() => {
    // AcpHarnessChat adopts the agent-generated chat title as the DB session
    // name; reflect it in the sidebar list immediately (and across reloads it
    // comes back from the DB).
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const handleSessionTitled = (event: any) => {
      const { sessionId, title } = event.detail ?? {};
      if (!sessionId || !title) return;
      setSessions(prev => prev.map(s => (s.id === sessionId ? { ...s, name: title } : s)));
    };

    window.addEventListener('harness-session-titled', handleSessionTitled);
    return () => {
      window.removeEventListener('harness-session-titled', handleSessionTitled);
    };
  }, []);

  return (
    <>
      <SessionsSidebar
        agentName={agentName}
        agentNamespace={namespace}
        currentAgent={currentAgent}
        allAgents={allAgents}
        agentSessions={sessions}
        acpSessions={acpSessions}
        isLoadingSessions={isLoadingSessions}
      />
      <main className="flex min-w-0 flex-1 flex-col overflow-x-hidden px-4">
        <div className="mx-auto flex w-full min-w-0 max-w-6xl flex-1 flex-col">
          <ChatAgentProvider
            agentType={currentAgent.agent.spec.type}
            runInSandbox={currentAgent.workloadMode === "sandbox"}
            substrateSandbox={isSubstrateSandboxAgent(currentAgent)}
          >
            {children}
          </ChatAgentProvider>
        </div>
      </main>
      <AgentDetailsSidebar
        currentAgent={currentAgent}
        allTools={convertedTools}
      />
    </>
  );
}
