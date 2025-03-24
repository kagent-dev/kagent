"use client";

import { useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";
import type { Team, AssistantAgentConfig, ToolConfig, Component } from "@/types/datamodel";
import { getToolDescription, getToolDisplayName, getToolIdentifier } from "@/lib/data";
import { getTeam } from "@/app/actions/teams";
import { findAllAssistantAgents } from "@/lib/agents";
import { SidebarHeader, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "@/components/ui/sidebar";
import { AgentActions } from "./AgentActions";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingState } from "@/components/LoadingState";

interface AgentDetailsSidebarProps {
  selectedAgentId: number;
}

export function AgentDetailsSidebar({ selectedAgentId }: AgentDetailsSidebarProps) {
  const [selectedTeam, setSelectedTeam] = useState<Team | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchTeam = async () => {
      setLoading(true);
      try {
        const teamData = await getTeam(selectedAgentId);
        if (teamData && teamData.data) {
          setSelectedTeam(teamData.data);
        }
      } catch (error) {
        console.error("Failed to fetch team:", error);
      } finally {
        setLoading(false);
      }
    };

    fetchTeam();
  }, [selectedAgentId]);

  if (loading) {
    return <LoadingState />;
  }

  const renderAgentTools = (tools: Component<ToolConfig>[] = []) => {
    if (tools.length === 0) {
      return (
        <SidebarMenu>
          <div className="text-sm italic">No tools available</div>
        </SidebarMenu>
      );
    }

    return (
      <SidebarMenu>
        {tools.map((tool) => {
          const toolIdentifier = getToolIdentifier(tool);
          const displayName = getToolDisplayName(tool);
          const displayDescription = getToolDescription(tool);

          return (
            <Collapsible key={toolIdentifier} asChild className="group/collapsible">
              <SidebarMenuItem>
                <CollapsibleTrigger asChild>
                  <SidebarMenuButton tooltip={displayName}>
                    <span>{displayName}</span>
                    <ChevronRight className="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
                  </SidebarMenuButton>
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <span className="text-sm text-muted-foreground flex px-2">{displayDescription}</span>
                </CollapsibleContent>
              </SidebarMenuItem>
            </Collapsible>
          );
        })}
      </SidebarMenu>
    );
  };

  const assistantAgents = findAllAssistantAgents(selectedTeam?.component);

  return (
    <>
      <Sidebar side={"right"} collapsible="offcanvas">
        <SidebarHeader>Agent Details</SidebarHeader>
        <SidebarContent>
          <ScrollArea>
            {assistantAgents.map((participant, index) => {
              const assistantAgent = participant.config as AssistantAgentConfig;
              return (
                <div key={index}>
                  <SidebarGroup>
                    <SidebarGroupLabel className="font-bold">
                      {selectedTeam?.component.label} ({assistantAgent.model_client.config.model})
                    </SidebarGroupLabel>
                    <p className="text-sm flex px-2 text-muted-foreground">{assistantAgent.description}</p>
                  </SidebarGroup>
                  <SidebarGroup>
                    <AgentActions agentId={selectedTeam?.id ?? 0} onCopyJson={() => navigator.clipboard.writeText(JSON.stringify(assistantAgent, null, 2))} />
                  </SidebarGroup>
                  <SidebarGroup className="group-data-[collapsible=icon]:hidden">
                    <SidebarGroupLabel>Tools</SidebarGroupLabel>
                    {renderAgentTools(assistantAgent.tools)}
                  </SidebarGroup>
                </div>
              );
            })}
          </ScrollArea>
        </SidebarContent>
      </Sidebar>
    </>
  );
}
