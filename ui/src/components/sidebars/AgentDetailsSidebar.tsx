"use client";

import { useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";
import type { AgentResponse, AgentTool } from "@/types/datamodel";
import { getTeam } from "@/app/actions/teams";
import { getToolByProvider, getTools } from "@/app/actions/tools";
import { SidebarHeader, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "@/components/ui/sidebar";
import { AgentActions } from "./AgentActions";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingState } from "@/components/LoadingState";
import { getToolIdentifier, getToolProvider, getToolDisplayName } from "@/lib/toolUtils";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";

interface AgentDetailsSidebarProps {
  selectedAgentId: number;
}

export function AgentDetailsSidebar({ selectedAgentId }: AgentDetailsSidebarProps) {
  const [selectedTeam, setSelectedTeam] = useState<AgentResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [toolDescriptions, setToolDescriptions] = useState<Record<string, string>>({});
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});

  useEffect(() => {
    const fetchTeam = async () => {
      setLoading(true);
      setError(null);
      try {
        const response = await getTeam(selectedAgentId);
        const allTools = await getTools();
        if (!allTools.success || !allTools.data) {
          setError("Failed to get tools");
          return;
        }

        if (response.success && response.data) {
          setSelectedTeam(response.data);
          
          // Fetch descriptions for all tools
          const descriptions: Record<string, string> = {};
          if (response.data.agent.spec.tools && Array.isArray(response.data.agent.spec.tools)) {
            for (const tool of response.data.agent.spec.tools) {
              const toolIdentifier = getToolIdentifier(tool);
              const toolProvider = getToolProvider(tool);

              try {
                // For MCP tools, we need to use the SseMcpToolAdapter provider and the tool name
                if (tool.type === "McpServer" && tool.mcpServer?.toolNames?.[0]) {
                  const toolName = tool.mcpServer.toolNames[0];
                  const mcpProvider = "autogen_ext.tools.mcp.SseMcpToolAdapter";
                  
                  const toolResponse = await getToolByProvider(allTools.data, mcpProvider, toolName);
                  descriptions[toolIdentifier] = toolResponse?.description 
                    ? toolResponse.description 
                    : "No description available";
                } else {
                  // For non-MCP tools, use the provider directly
                  const toolResponse = await getToolByProvider(allTools.data, toolProvider);
                  descriptions[toolIdentifier] = toolResponse?.description 
                    ? toolResponse.description 
                    : "No description available";
                }
              } catch (error) {
                console.error(`Failed to fetch description for tool ${toolProvider}:`, error);
                descriptions[toolIdentifier] = "Failed to load description";
              }
            }
          }
          setToolDescriptions(descriptions);
        } else {
          setError(response.error || "Failed to fetch team data");
        }
      } catch (error) {
        const errorMessage = error instanceof Error ? error.message : "An unexpected error occurred";
        setError(errorMessage);
        console.error("Failed to fetch team:", error);
      } finally {
        setLoading(false);
      }
    };

    fetchTeam();
  }, [selectedAgentId]);

  const toggleToolExpansion = (toolProvider: string) => {
    setExpandedTools(prev => ({
      ...prev,
      [toolProvider]: !prev[toolProvider]
    }));
  };

  if (loading) {
    return <LoadingState />;
  }

  if (error) {
    return <div className="p-4 text-red-500">{error}</div>;
  }

  const renderAgentTools = (tools: AgentTool[] = []) => {    
    if (!tools || tools.length === 0) {
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
          const displayName = getToolDisplayName(tool);
          const description = toolDescriptions[toolIdentifier] || "No description available";
          const isExpanded = expandedTools[toolIdentifier] || false;

          // Split the provider at . and get the last part
          const providerParts = toolProvider.split(".");
          const providerName = providerParts[providerParts.length - 1];

          return (
            <Collapsible
              key={toolIdentifier}
              open={isExpanded}
              onOpenChange={() => toggleToolExpansion(toolIdentifier)}
              className="group/collapsible"
            >
              <SidebarMenuItem>
                <CollapsibleTrigger asChild>
                  <SidebarMenuButton tooltip={providerName} className="w-full">
                    <div className="flex items-center justify-between w-full">
                      <span className="truncate max-w-[200px]">{displayName}</span>
                      <ChevronRight className={cn(
                        "h-4 w-4 transition-transform duration-200",
                        isExpanded && "rotate-90"
                      )} />
                    </div>
                  </SidebarMenuButton>
                </CollapsibleTrigger>
                <CollapsibleContent className="px-2 py-1">
                  <div className="rounded-md bg-muted/50 p-2">
                    <p className="text-sm text-muted-foreground">
                      {description}
                    </p>
                  </div>
                </CollapsibleContent>
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
              <AgentActions 
                agentId={selectedAgentId}
                onCopyJson={() => navigator.clipboard.writeText(JSON.stringify(selectedTeam, null, 2))} 
              />
            </SidebarGroup>
            <SidebarGroup className="group-data-[collapsible=icon]:hidden">
              <SidebarGroupLabel>Tools</SidebarGroupLabel>
              {selectedTeam && renderAgentTools(selectedTeam.agent.spec.tools)}
            </SidebarGroup>
          </ScrollArea>
        </SidebarContent>
      </Sidebar>
    </>
  );
}
