"use client";
import React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail
} from "../ui/sidebar";
import { AgentSwitcher } from "./AgentSwitcher";
import GroupedChats from "./GroupedChats";
import type { AgentResponse, Session, Tool } from "@/types";
import { Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";

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
  const isDeclarativeAgent = currentAgent?.agent.spec.type === "Declarative";

  const toolLabels = React.useMemo(() => {
    if (!isDeclarativeAgent || !currentAgent?.tools?.length) {
      return [];
    }

    return currentAgent.tools.flatMap((tool: Tool) => {
      if (tool.type === "Agent" && tool.agent?.name) {
        return [`Agent: ${tool.agent.namespace || agentNamespace}/${tool.agent.name}`];
      }

      if (tool.mcpServer?.toolNames?.length) {
        const serverRef = `${tool.mcpServer.namespace || agentNamespace}/${tool.mcpServer.name || "unknown-server"}`;
        return tool.mcpServer.toolNames.map((toolName) => `${toolName} (${serverRef})`);
      }

      if (tool.mcpServer?.name) {
        return [`${tool.mcpServer.namespace || agentNamespace}/${tool.mcpServer.name}`];
      }

      return [];
    });
  }, [agentNamespace, currentAgent, isDeclarativeAgent]);

  const skills = React.useMemo(() => {
    if (!isDeclarativeAgent) {
      return [];
    }

    return currentAgent.agent.spec.skills?.refs || [];
  }, [currentAgent, isDeclarativeAgent]);

  return (
    <Sidebar side="right" collapsible="offcanvas">
      <SidebarHeader>
        <AgentSwitcher currentAgent={currentAgent} allAgents={allAgents} />
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup className="pt-0">
          <SidebarGroupLabel>Sessions</SidebarGroupLabel>
          <ScrollArea className="flex-1 my-2">
            {isLoadingSessions ? (
              <div className="flex items-center justify-center h-20">
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                <span className="ml-2 text-sm text-muted-foreground">Loading sessions...</span>
              </div>
            ) : (
              <GroupedChats agentName={agentName} agentNamespace={agentNamespace} sessions={agentSessions} />
            )}
          </ScrollArea>
        </SidebarGroup>

        {isDeclarativeAgent && (
          <SidebarGroup className="pt-0">
            <div className="flex items-center justify-between px-2">
              <SidebarGroupLabel>Tools</SidebarGroupLabel>
              <Badge variant="secondary" className="h-5">
                {toolLabels.length}
              </Badge>
            </div>
            <SidebarMenu>
              {toolLabels.length ? (
                toolLabels.map((toolLabel, index) => (
                  <SidebarMenuItem key={`${toolLabel}-${index}`}>
                    <SidebarMenuButton className="h-auto py-1.5">
                      <span className="truncate">{toolLabel}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))
              ) : (
                <SidebarMenuItem>
                  <SidebarMenuButton disabled>
                    <span className="italic text-muted-foreground">No tools configured</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              )}
            </SidebarMenu>
          </SidebarGroup>
        )}

        {isDeclarativeAgent && (
          <SidebarGroup className="pt-0">
            <div className="flex items-center justify-between px-2">
              <SidebarGroupLabel>Skills</SidebarGroupLabel>
              <Badge variant="secondary" className="h-5">
                {skills.length}
              </Badge>
            </div>
            <SidebarMenu>
              {skills.length ? (
                skills.map((skillRef, index) => (
                  <SidebarMenuItem key={`${skillRef}-${index}`}>
                    <SidebarMenuButton className="h-auto py-1.5">
                      <span className="truncate" title={skillRef}>
                        {skillRef}
                      </span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))
              ) : (
                <SidebarMenuItem>
                  <SidebarMenuButton disabled>
                    <span className="italic text-muted-foreground">No skills configured</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              )}
            </SidebarMenu>
          </SidebarGroup>
        )}
      </SidebarContent>
      <SidebarRail />
    </Sidebar>
  );
}
