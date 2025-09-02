"use client";

import { useEffect, useState } from "react";
import { ChevronRight, Edit, Plus } from "lucide-react";
import type { AgentResponse, Tool, ToolsResponse } from "@/types";
import { SidebarHeader, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "@/components/ui/sidebar";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingState } from "@/components/LoadingState";
import { isAgentTool, isMcpTool, getToolDescription, getToolIdentifier, getToolDisplayName } from "@/lib/toolUtils";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import Link from "next/link";
import { getAgents } from "@/app/actions/agents";
import { k8sRefUtils } from "@/lib/k8sUtils";

interface AgentDetailsSidebarProps {
  selectedAgentName: string;
  currentAgent: AgentResponse;
  allTools: ToolsResponse[];
}

export function AgentDetailsSidebar({ selectedAgentName, currentAgent, allTools }: AgentDetailsSidebarProps) {
  const [toolDescriptions, setToolDescriptions] = useState<Record<string, string>>({});
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});
  const [availableAgents, setAvailableAgents] = useState<AgentResponse[]>([]);
  const router = useRouter();

  const selectedTeam = currentAgent;

  // Fetch agents for looking up agent tool descriptions
  useEffect(() => {
    const fetchAgents = async () => {
      try {
        const response = await getAgents();
        if (response.data) {
          setAvailableAgents(response.data);

        } else if (response.error) {
          console.error("AgentDetailsSidebar: Error fetching agents:", response.error);
        }
      } catch (error) {
        console.error("AgentDetailsSidebar: Failed to fetch agents:", error);
      }
    };

    fetchAgents();
  }, []);



  const RenderToolCollapsibleItem = ({
    itemKey,
    displayName,
    providerTooltip,
    description,
    isExpanded,
    onToggleExpansion,
  }: {
    itemKey: string;
    displayName: string;
    providerTooltip: string;
    description: string;
    isExpanded: boolean;
    onToggleExpansion: () => void;
  }) => {
    return (
      <Collapsible
        key={itemKey}
        open={isExpanded}
        onOpenChange={onToggleExpansion}
        className="group/collapsible"
      >
        <SidebarMenuItem>
          <CollapsibleTrigger asChild>
            <SidebarMenuButton tooltip={providerTooltip} className="w-full">
              <div className="flex items-center justify-between w-full">
                <span className="truncate max-w-[200px]">{displayName}</span>
                <ChevronRight
                  className={cn(
                    "h-4 w-4 transition-transform duration-200",
                    isExpanded && "rotate-90"
                  )}
                />
              </div>
            </SidebarMenuButton>
          </CollapsibleTrigger>
          <CollapsibleContent className="px-2 py-1">
            <div className="rounded-md bg-muted/50 p-2">
              <p className="text-sm text-muted-foreground">{description}</p>
            </div>
          </CollapsibleContent>
        </SidebarMenuItem>
      </Collapsible>
    );
  };

  useEffect(() => {
    const processToolDescriptions = () => {
      setToolDescriptions({});

      if (!selectedTeam || !allTools) return;

      const descriptions: Record<string, string> = {};
      const toolRefs = selectedTeam.tools;

      if (toolRefs && Array.isArray(toolRefs)) {
        toolRefs.forEach((tool) => {
          if (isMcpTool(tool)) {
            const mcpTool = tool as Tool;
            // For MCP tools, each tool name gets its own description
            const baseToolIdentifier = getToolIdentifier(mcpTool);
            mcpTool.mcpServer?.toolNames.forEach((mcpToolName) => {
              const subToolIdentifier = `${baseToolIdentifier}::${mcpToolName}`;
              
              // Find the tool in allTools by matching server ref and tool name
              const toolFromDB = allTools.find(server => {
                const { name } = k8sRefUtils.fromRef(server.server_name);
                return name === mcpTool.mcpServer?.name && server.id === mcpToolName;
              });

              if (toolFromDB) {
                descriptions[subToolIdentifier] = toolFromDB.description;
              } else {
                descriptions[subToolIdentifier] = "No description available";
              }
            });
          } else {
            // Handle Agent tools or regular tools using getToolDescription
            const toolIdentifier = getToolIdentifier(tool);
            descriptions[toolIdentifier] = getToolDescription(tool, allTools);
          }
        });
      }
      
      setToolDescriptions(descriptions);
    };

    processToolDescriptions();
  }, [selectedTeam, allTools, availableAgents]);

  const toggleToolExpansion = (toolIdentifier: string) => {
    setExpandedTools(prev => ({
      ...prev,
      [toolIdentifier]: !prev[toolIdentifier]
    }));
  };

  if (!selectedTeam) {
    return <LoadingState />;
  }

  const renderAgentTools = (tools: Tool[] = []) => {
    if (!tools || tools.length === 0) {
      return (
        <SidebarMenu>
          <div className="text-sm italic">No tools/agents available</div>
        </SidebarMenu>
      );
    }

    return (
      <SidebarMenu>
        {tools.flatMap((tool) => {
          const baseToolIdentifier = getToolIdentifier(tool);

          if (tool.mcpServer && tool.mcpServer?.toolNames && tool.mcpServer.toolNames.length > 0) {
            const mcpProvider = tool.mcpServer.name || "mcp_server";
            const mcpProviderParts = mcpProvider.split(".");
            const mcpProviderNameTooltip = mcpProviderParts[mcpProviderParts.length - 1];

            return tool.mcpServer.toolNames.map((mcpToolName) => {
              const subToolIdentifier = `${baseToolIdentifier}::${mcpToolName}`;
              const description = toolDescriptions[subToolIdentifier] || "Description loading or unavailable";
              const isExpanded = expandedTools[subToolIdentifier] || false;

              return (
                <RenderToolCollapsibleItem
                  key={subToolIdentifier}
                  itemKey={subToolIdentifier}
                  displayName={mcpToolName}
                  providerTooltip={mcpProviderNameTooltip}
                  description={description}
                  isExpanded={isExpanded}
                  onToggleExpansion={() => toggleToolExpansion(subToolIdentifier)}
                />
              );
            });
          } else {
            const toolIdentifier = baseToolIdentifier;
            const provider = isAgentTool(tool) ? (tool.agent?.name || "unknown") : (tool.mcpServer?.name || "unknown");
            const displayName = getToolDisplayName(tool);
            const description = toolDescriptions[toolIdentifier] || "Description loading or unavailable";
            const isExpanded = expandedTools[toolIdentifier] || false;

            const providerParts = provider.split(".");
            const providerNameTooltip = providerParts[providerParts.length - 1];

            return [(
              <RenderToolCollapsibleItem
                key={toolIdentifier}
                itemKey={toolIdentifier}
                displayName={displayName}
                providerTooltip={providerNameTooltip}
                description={description}
                isExpanded={isExpanded}
                onToggleExpansion={() => toggleToolExpansion(toolIdentifier)}
              />
            )];
          }
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
              <div className="flex items-center justify-between px-2 mb-1">
                <SidebarGroupLabel className="font-bold mb-0 p-0">
                  {selectedTeam?.agent.metadata.namespace}/{selectedTeam?.agent.metadata.name} {selectedTeam?.model && `(${selectedTeam?.model})`}
                </SidebarGroupLabel>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  asChild
                  aria-label={`Edit agent ${selectedTeam?.agent.metadata.namespace}/${selectedTeam?.agent.metadata.name}`}
                >
                  <Link href={`/agents/new?edit=true&name=${selectedAgentName}&namespace=${currentAgent.agent.metadata.namespace}`}>
                    <Edit className="h-3.5 w-3.5" />
                  </Link>
                </Button>
              </div>
              <p className="text-sm flex px-2 text-muted-foreground">{selectedTeam?.agent.spec.description}</p>
            </SidebarGroup>
            <SidebarGroup className="group-data-[collapsible=icon]:hidden">
              <SidebarGroupLabel>Tools & Agents</SidebarGroupLabel>
              {selectedTeam && renderAgentTools(selectedTeam.tools)}
            </SidebarGroup>
          </ScrollArea>
        </SidebarContent>
      </Sidebar>
    </>
  );
}
