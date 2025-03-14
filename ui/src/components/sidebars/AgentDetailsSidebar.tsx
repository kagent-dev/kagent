"use client";

import { useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";
import type { Team, AssistantAgentConfig, ToolConfig, Component } from "@/types/datamodel";
import { SystemPromptEditor } from "./SystemPromptEditor";
import { getToolDescription, getToolDisplayName, getToolIdentifier } from "@/lib/data";
import { createTeam, getTeam } from "@/app/actions/teams";
import { findAllAssistantAgents, updateUsersAgent } from "@/lib/agents";
import { SidebarHeader, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "../ui/sidebar";
import { AgentActions } from "./AgentActions";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "../ui/collapsible";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingState } from "../LoadingState";

interface AgentDetailsSidebarProps {
  selectedTeamId: string;
}

export function AgentDetailsSidebar({ selectedTeamId }: AgentDetailsSidebarProps) {
  const [isSystemPromptOpen, setIsSystemPromptOpen] = useState(false);
  const [currentSystemPrompt, setCurrentSystemPrompt] = useState("");
  const [selectedTeam, setSelectedTeam] = useState<Team | null>(null);
  const [currentAgentIndex, setCurrentAgentIndex] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchTeam = async () => {
      setLoading(true);
      try {
        const teamData = await getTeam(selectedTeamId);
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
  }, [selectedTeamId]);

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

  const handleOpenSystemPrompt = (systemMessage: string, agentIndex: number) => {
    setCurrentSystemPrompt(systemMessage);
    setCurrentAgentIndex(agentIndex);
    setIsSystemPromptOpen(true);
  };

  const handleUpdateSystemPrompt = async (newSystemPrompt: string) => {
    if (!selectedTeam || currentAgentIndex === null) return;

    try {
      const updatedTeam = updateUsersAgent(selectedTeam, (agent) => {
        agent.config.system_message = newSystemPrompt;
      });

      // Remove the created_at and updated_at variables from the selectedTeam
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { created_at, updated_at, ...editedTeam } = updatedTeam;
      await createTeam(editedTeam);

      setCurrentSystemPrompt(newSystemPrompt);
      return Promise.resolve();
    } catch (error) {
      console.error("Failed to update instructions:", error);
      return Promise.reject(error);
    }
  };

  const assistantAgents = findAllAssistantAgents(selectedTeam?.component);

  return (
    <>
      <Sidebar side={"right"} collapsible="offcanvas">
        <SidebarHeader>Agent Details</SidebarHeader>
        <SidebarContent className="">
          <ScrollArea className="">
            {assistantAgents.map((participant, index) => {
              const assistantAgent = participant.config as AssistantAgentConfig;
              return (
                <div key={index}>
                  <SidebarGroup>
                    <SidebarGroupLabel className="font-bold">
                      {participant.label} ({assistantAgent.model_client.config.model})
                    </SidebarGroupLabel>
                    <p className="text-sm flex px-2 text-muted-foreground">{assistantAgent.description}</p>
                  </SidebarGroup>
                  <SidebarGroup>
                    <AgentActions
                      agentId={selectedTeam?.id ?? 0}
                      onViewInstructions={() => handleOpenSystemPrompt(assistantAgent.system_message ?? "No system prompt available", index)}
                      onCopyJson={() => navigator.clipboard.writeText(JSON.stringify(assistantAgent, null, 2))}
                    />
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

      {/* Using the new SystemPromptEditor component */}
      <SystemPromptEditor isOpen={isSystemPromptOpen} onOpenChange={setIsSystemPromptOpen} systemPrompt={currentSystemPrompt} onSave={handleUpdateSystemPrompt} />
    </>
  );
}
