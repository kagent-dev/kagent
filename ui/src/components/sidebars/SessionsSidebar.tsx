"use client";
import React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sidebar, SidebarContent, SidebarHeader, SidebarRail } from "../ui/sidebar";
import { AgentSwitcher } from "./AgentSwitcher";
import GroupedChats from "./GroupedChats";
import { AgentResponse, Session } from "@/types/datamodel";
import { Loader2 } from "lucide-react";

interface SessionsSidebarProps {
  agentName: string;
  agentNamespace: string;
  currentAgent: AgentResponse;
  allAgents: AgentResponse[];
  agentSessions: Session[];
  isLoadingSessions?: boolean;
}

export default function SessionsSidebar({ 
  agentName, 
  agentNamespace,
  currentAgent, 
  allAgents, 
  agentSessions, 
  isLoadingSessions = false 
}: SessionsSidebarProps) {
    return (
    <Sidebar side="left" collapsible="offcanvas">
      <SidebarHeader>
        <AgentSwitcher currentAgent={currentAgent} allAgents={allAgents} />
      </SidebarHeader>
      <SidebarContent>
        <ScrollArea className="flex-1 my-4">
          {isLoadingSessions ? (
            <div className="flex items-center justify-center h-20">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
              <span className="ml-2 text-sm text-muted-foreground">Loading sessions...</span>
            </div>
          ) : (
            <GroupedChats agentName={agentName} agentNamespace={agentNamespace} sessions={agentSessions} />
          )}
        </ScrollArea>
      </SidebarContent>
      <SidebarRail />
    </Sidebar>
  );
}
