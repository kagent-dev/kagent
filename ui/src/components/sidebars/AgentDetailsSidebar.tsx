"use client";

import { useState } from "react";
import { ChevronRight } from "lucide-react";
import type { Team, AssistantAgentConfig, ToolConfig, Component } from "@/types/datamodel";
import { SystemPromptEditor } from "./SystemPromptEditor"; // Import the new component
import { getToolDescription, getToolDisplayName, getToolIdentifier, isMcpTool } from "@/lib/data";
import { createTeam } from "@/app/actions/teams";
import { findAllAssistantAgents, updateUsersAgent } from "@/lib/agents";
import { SidebarHeader, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton, SidebarMenuSubItem } from "../ui/sidebar";
import { AgentActions } from "./AgentActions";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "../ui/collapsible";
import { useRouter } from "next/navigation";
import { ScrollArea } from "@/components/ui/scroll-area";

interface AgentDetailsSidebarProps {
  selectedTeam: Team | null;
}

export function AgentDetailsSidebar({ selectedTeam }: AgentDetailsSidebarProps) {
  const router = useRouter();
  const [isSystemPromptOpen, setIsSystemPromptOpen] = useState(false);
  const [currentSystemPrompt, setCurrentSystemPrompt] = useState("");

  // Keep track of the current agent for updating
  const [currentAgentIndex, setCurrentAgentIndex] = useState<number | null>(null);

  const renderAgentTools = (tools: Component<ToolConfig>[] = []) => {
    if (tools.length === 0) {
      return (
        <SidebarMenu>
          <div className="text-sm italic">No tools available</div>;
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
                    {isMcpTool(tool) && <span className="ml-2 text-xs bg-blue-400/20 text-blue-400 px-2 py-0.5 rounded">MCP</span>}
                    <span>{displayName}</span>
                    <ChevronRight className="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
                  </SidebarMenuButton>
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <SidebarMenuSubItem>
                    <span className="text-sm text-muted-foreground flex px-2">{displayDescription}</span>
                  </SidebarMenuSubItem>
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
                      onViewInstructions={() => handleOpenSystemPrompt(assistantAgent.system_message ?? "No system prompt available", index)}
                      onCopyJson={() => navigator.clipboard.writeText(JSON.stringify(assistantAgent, null, 2))}
                      onEdit={() => router.push(`/agents/new?edit=true&id=${selectedTeam?.id}`)}
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
