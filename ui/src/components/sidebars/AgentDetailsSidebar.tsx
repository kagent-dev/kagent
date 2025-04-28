"use client";

import { useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";
import type { AgentResponse, Tool, Component, ToolConfig, MCPToolConfig } from "@/types/datamodel";
import { getTeam } from "@/app/actions/teams";
import { getToolByProvider, getTools } from "@/app/actions/tools";
import { SidebarHeader, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "@/components/ui/sidebar";
import { AgentActions } from "./AgentActions";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingState } from "@/components/LoadingState";
import { getToolIdentifier, getToolProvider, getToolDisplayName, SSE_MCP_TOOL_PROVIDER_NAME, STDIO_MCP_TOOL_PROVIDER_NAME, isAgentTool, isMcpProvider } from "@/lib/toolUtils";
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
    const fetchTeamAndToolDescriptions = async () => {
      setLoading(true);
      setError(null);
      setToolDescriptions({});

      try {
        const teamResponse = await getTeam(selectedAgentId);
        if (!teamResponse.success || !teamResponse.data) {
          setError(teamResponse.error || "Failed to fetch team data");
          setLoading(false);
          return;
        }
        const currentTeam = teamResponse.data;
        setSelectedTeam(currentTeam);

        const allToolsResponse = await getTools();
        if (!allToolsResponse.success || !allToolsResponse.data) {
          setError("Failed to get tool definitions");
        }
        const allToolDefinitions = allToolsResponse.data || [];

        const descriptions: Record<string, string> = {};
        const toolsInSpec = currentTeam.agent.spec.tools;

        if (toolsInSpec && Array.isArray(toolsInSpec)) {
          for (const tool of toolsInSpec) {
            const toolIdentifier = getToolIdentifier(tool);
            let description = "No description available";

            try {
              if (isAgentTool(tool)) {
                description = tool.agent?.description || "Agent description not found";
              } else {
                const toolProvider = getToolProvider(tool);
                let foundToolDefinition: Component<ToolConfig> | null = null;

                if (tool.type === "McpServer" && tool.mcpServer?.toolNames?.[0]) {
                  const toolName = tool.mcpServer.toolNames[0];
                  foundToolDefinition = await getToolByProvider(allToolDefinitions, SSE_MCP_TOOL_PROVIDER_NAME, toolName);
                  if (!foundToolDefinition) {
                    foundToolDefinition = await getToolByProvider(allToolDefinitions, STDIO_MCP_TOOL_PROVIDER_NAME, toolName);
                  }
                } else if (tool.type === "Builtin") {
                  foundToolDefinition = await getToolByProvider(allToolDefinitions, toolProvider);
                }

                if (foundToolDefinition?.description) {
                  description = foundToolDefinition.description;
                } else if (foundToolDefinition && isMcpProvider(foundToolDefinition.provider)) {
                  const nestedDescription = (foundToolDefinition.config as MCPToolConfig)?.tool?.description;
                  if (nestedDescription) {
                    description = nestedDescription;
                  }
                }
              }
            } catch (err) {
              console.error(`Failed to process description for tool ${toolIdentifier}:`, err);
              description = "Failed to load description";
            }

            descriptions[toolIdentifier] = description;
          }
        }
        setToolDescriptions(descriptions);

      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : "An unexpected error occurred";
        setError(errorMessage);
        console.error("Failed fetching data:", err);
      } finally {
        setLoading(false);
      }
    };

    fetchTeamAndToolDescriptions();
  }, [selectedAgentId]);

  const toggleToolExpansion = (toolIdentifier: string) => {
    setExpandedTools(prev => ({
      ...prev,
      [toolIdentifier]: !prev[toolIdentifier]
    }));
  };

  if (loading) {
    return <LoadingState />;
  }

  if (error) {
    return <div className="p-4 text-red-500">{error}</div>;
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
        {tools.map((tool) => {
          const toolIdentifier = getToolIdentifier(tool);
          const provider = getToolProvider(tool) || "unknown";
          const displayName = getToolDisplayName(tool);
          const description = toolDescriptions[toolIdentifier] || "Description loading or unavailable";
          const isExpanded = expandedTools[toolIdentifier] || false;

          const providerParts = provider.split(".");
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
              <SidebarGroupLabel>Tools & Agents</SidebarGroupLabel>
              {selectedTeam && renderAgentTools(selectedTeam.agent.spec.tools)}
            </SidebarGroup>
          </ScrollArea>
        </SidebarContent>
      </Sidebar>
    </>
  );
}
