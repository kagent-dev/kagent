"use client";

import React, { useState, useEffect } from "react";
import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import { getSessionsForAgent } from "@/app/actions/sessions";
import { AgentResponse, Session, ToolResponse } from "@/types";
import { toast } from "sonner";

interface ChatLayoutUIProps {
  agentName: string;
  namespace: string;
  currentAgent: AgentResponse;
  allAgents: AgentResponse[];
  allTools: ToolResponse[];
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
  const [sessions, setSessions] = useState<Session[]>([]);
  const [isLoadingSessions, setIsLoadingSessions] = useState(true);

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

  useEffect(() => {
    refreshSessions();
  }, [agentName]);

  useEffect(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const handleNewSession = (event: any) => {
      const { agentRef, session } = event.detail;
      // Only update if this is for our current agent (agentRef format: "namespace/agentName")
      const currentAgentRef = `${namespace}/${agentName}`;
      if (agentRef === currentAgentRef && session) {
        setSessions(prevSessions => {
          const exists = prevSessions.some(s => s.id === session.id);
          if (exists) {
            return prevSessions;
          }
          return [session, ...prevSessions];
        });
      }
    };

    window.addEventListener('new-session-created', handleNewSession);
    return () => {
      window.removeEventListener('new-session-created', handleNewSession);
    };
  }, [agentName]);

  return (
    <>
      <SessionsSidebar
        agentName={agentName}
        agentNamespace={namespace}
        currentAgent={currentAgent}
        allAgents={allAgents}
        agentSessions={sessions}
        isLoadingSessions={isLoadingSessions}
      />
      <main className="w-full max-w-6xl mx-auto px-4">
        {children}
      </main>
      <AgentDetailsSidebar
        selectedAgentName={agentName}
        currentAgent={currentAgent}
        allTools={allTools}
      />
    </>
  );
} 