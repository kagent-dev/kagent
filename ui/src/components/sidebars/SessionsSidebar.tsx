"use client";
import React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sidebar, SidebarContent, SidebarHeader, SidebarRail } from "../ui/sidebar";
import { AgentSwitcher } from "./AgentSwitcher";
import GroupedSessions from "./GroupedSessions";
import { AgentResponse, SessionWithRuns } from "@/types/datamodel";
import useChatStore from "@/lib/useChatStore";

interface SessionsSidebarProps {
  agentId: number;
  currentAgent: AgentResponse;
  allAgents: AgentResponse[];
  sessionsWithRuns: SessionWithRuns[];
}

export default function SessionsSidebar({ agentId, currentAgent, allAgents, sessionsWithRuns }: SessionsSidebarProps) {
  const storeSessions = useChatStore((state) => state.sessions);
  const displaySessions = storeSessions.length > 0 ? storeSessions : sessionsWithRuns;

  return (
    <Sidebar side="left" collapsible="offcanvas">
      <SidebarHeader>
        <AgentSwitcher currentAgent={currentAgent} allAgents={allAgents} />
      </SidebarHeader>
      <SidebarContent>
        <ScrollArea className="flex-1 my-4">
          <GroupedSessions agentId={agentId} sessionsWithRuns={displaySessions} />
        </ScrollArea>
      </SidebarContent>
      <SidebarRail />
    </Sidebar>
  );
}
