"use client";

import { useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";
import type { AgentResponse, AgentTool } from "@/types/datamodel";
import { getTeam } from "@/app/actions/teams";
import { SidebarHeader, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "@/components/ui/sidebar";
import { AgentActions } from "./AgentActions";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingState } from "@/components/LoadingState";
import { getToolDescription, getToolIdentifier, getToolProvider } from "@/lib/data";
import { extractSocietyOfMindAgentTools } from "@/lib/toolUtils";

interface AgentDetailsSidebarProps {
  selectedAgentId: number;
}

export function AgentDetailsSidebar({ selectedAgentId }: AgentDetailsSidebarProps) {
  const [selectedTeam, setSelectedTeam] = useState<AgentResponse | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchTeam = async () => {
      setLoading(true);
      try {
        const { data } = await getTeam(selectedAgentId);
        if (data && data.agent) {
          setSelectedTeam(data);
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

  const renderAgentTools = (tools: AgentTool[] = []) => {
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
          const toolProvider = getToolProvider(tool);
          const displayDescription = getToolDescription(tool);

          // Split the provider at . and get the last part
          const providerParts = toolProvider.split(".");
          const providerName = providerParts[providerParts.length - 1];

          return (
            <Collapsible key={toolIdentifier} asChild className="group/collapsible">
              <SidebarMenuItem>
                <CollapsibleTrigger asChild>
                  <SidebarMenuButton tooltip={providerName}>
                    <span>{providerName}</span>
                    {displayDescription && <ChevronRight className="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />}
                  </SidebarMenuButton>
                </CollapsibleTrigger>
                {displayDescription && (
                  <CollapsibleContent>
                    <span className="text-sm text-muted-foreground flex px-2">{displayDescription}</span>
                  </CollapsibleContent>
                )}
              </SidebarMenuItem>
            </Collapsible>
          );
        })}
      </SidebarMenu>
    );
  };

  return (
    <>
      <Sidebar side={"right"} collapsible="offcanvas">
        <SidebarHeader>Agent Details</SidebarHeader>
        <SidebarContent>
          <ScrollArea>
            <SidebarGroup>
              <SidebarGroupLabel className="font-bold">
                {selectedTeam?.agent.metadata.name} ({selectedTeam?.model})
              </SidebarGroupLabel>
              <p className="text-sm flex px-2 text-muted-foreground">{selectedTeam?.agent.spec.description}</p>
            </SidebarGroup>
            <SidebarGroup>
              <AgentActions agentName={selectedTeam?.agent.metadata.name ?? ""} onCopyJson={() => navigator.clipboard.writeText(JSON.stringify(selectedTeam, null, 2))} />
            </SidebarGroup>
            <SidebarGroup className="group-data-[collapsible=icon]:hidden">
              <SidebarGroupLabel>Tools</SidebarGroupLabel>
              {selectedTeam && renderAgentTools(extractSocietyOfMindAgentTools(selectedTeam))}
            </SidebarGroup>
          </ScrollArea>
        </SidebarContent>
      </Sidebar>
    </>
  );
}
