"use client";

import React, { useState, useEffect } from "react";
import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import { AgentResponse, Session, Component, ToolConfig } from "@/types/datamodel";
import { getSessions } from "@/app/actions/sessions";
import { toast } from "sonner";

interface ChatLayoutUIProps {
  agentId: number;
  currentAgent: AgentResponse;
  allAgents: AgentResponse[];
  allTools: Component<ToolConfig>[];
  children: React.ReactNode;
}

export default function ChatLayoutUI({
  agentId,
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
      const sessionsResponse = await getSessions(agentId);
      if (sessionsResponse.success && sessionsResponse.data) {
        setSessions(sessionsResponse.data);
      } else {
        console.log(`No sessions found for agent ${agentId}`);
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
  }, [agentId]);

  useEffect(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const handleNewSession = (event: any) => {
      const { agentId: eventAgentId, session } = event.detail;
      // Only update if this is for our current agent
      if (Number(eventAgentId) === Number(agentId) && session) {
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
  }, [agentId]);

  return (
    <>
      <SessionsSidebar
        agentId={agentId}
        currentAgent={currentAgent}
        allAgents={allAgents}
        agentSessions={sessions}
        isLoadingSessions={isLoadingSessions}
      />
      <main className="w-full max-w-6xl mx-auto px-4">
        {children}
      </main>
      <AgentDetailsSidebar
        selectedAgentId={agentId}
        currentAgent={currentAgent}
        allTools={allTools}
      />
    </>
  );
} 