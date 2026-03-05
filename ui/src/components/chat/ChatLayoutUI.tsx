"use client";

import React, { useState, useEffect, useMemo } from "react";
import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import { getSessionsForAgent } from "@/app/actions/sessions";
import { AgentResponse, Session, RemoteMCPServerResponse, ToolsResponse } from "@/types";
import { toast } from "sonner";
import { Info } from "lucide-react";
import { Button } from "@/components/ui/button";

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
  const [sessions, setSessions] = useState<Session[]>([]);
  const [isLoadingSessions, setIsLoadingSessions] = useState(true);
  const [agentDetailsOpen, setAgentDetailsOpen] = useState(false);

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
  }, [agentName, namespace]);

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
  }, [agentName, namespace]);

  return (
    <div className="flex h-full w-full">
      <div className="flex-1 flex flex-col min-w-0">
        <div className="flex items-center justify-end gap-2 px-4 py-2 border-b shrink-0">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setAgentDetailsOpen(true)}
            aria-label="Show agent details"
          >
            <Info className="h-4 w-4" />
          </Button>
        </div>
        <div className="flex-1 w-full max-w-6xl mx-auto px-4 overflow-y-auto">
          {children}
        </div>
      </div>
      <SessionsSidebar
        agentName={agentName}
        agentNamespace={namespace}
        currentAgent={currentAgent}
        allAgents={allAgents}
        agentSessions={sessions}
        isLoadingSessions={isLoadingSessions}
      />
      <AgentDetailsSidebar
        selectedAgentName={agentName}
        currentAgent={currentAgent}
        allTools={convertedTools}
        open={agentDetailsOpen}
        onClose={() => setAgentDetailsOpen(false)}
      />
    </div>
  );
} 