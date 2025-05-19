"use client";

import { useEffect, useState } from "react";
import { ChevronRight, Edit, Plus } from "lucide-react";
import type { AgentResponse, Tool, Component, ToolConfig, MCPToolConfig } from "@/types/datamodel";
import { SidebarHeader, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "@/components/ui/sidebar";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingState } from "@/components/LoadingState";
import { getToolIdentifier, getToolProvider, getToolDisplayName, SSE_MCP_TOOL_PROVIDER_NAME, STDIO_MCP_TOOL_PROVIDER_NAME, isAgentTool } from "@/lib/toolUtils";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import Link from "next/link";

interface AgentDetailsSidebarProps {
  selectedAgentId: number;
  currentAgent: AgentResponse;
  allTools: Component<ToolConfig>[];
}

export function AgentDetailsSidebar({ selectedAgentId, currentAgent, allTools }: AgentDetailsSidebarProps) {
  const [toolDescriptions, setToolDescriptions] = useState<Record<string, string>>({});
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});
  const router = useRouter();

  const selectedTeam = currentAgent;

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
    const processToolDescriptions = async () => {
      setToolDescriptions({});

      if (!selectedTeam || !allTools) return;

      try {
        const descriptions: Record<string, string> = {};
        const toolsInSpec = selectedTeam.agent.spec.tools;
        const allToolDefinitions = allTools;

        if (toolsInSpec && Array.isArray(toolsInSpec)) {
          await Promise.all(toolsInSpec.map(async (tool) => {
            if (tool.mcpServer && tool.mcpServer?.toolNames && tool.mcpServer.toolNames.length > 0) {
              const baseMcpIdentifier = getToolIdentifier(tool);
              await Promise.all(tool.mcpServer.toolNames.map(async (mcpToolName) => {
                const subToolIdentifier = `${baseMcpIdentifier}::${mcpToolName}`;
                let description = "No description available";
                try {
                  const foundToolDefinition = allToolDefinitions.find(
                    (def) => (
                      (def.provider === SSE_MCP_TOOL_PROVIDER_NAME || def.provider === STDIO_MCP_TOOL_PROVIDER_NAME) &&
                      (def.config as MCPToolConfig)?.tool?.name === mcpToolName
                    )
                  ) || null;

                  if (foundToolDefinition) {
                    const toolConfig = foundToolDefinition.config as MCPToolConfig;
                    // isMcpProvider check is implicitly true due to the find condition
                    description = toolConfig?.tool?.description || `Description for MCP tool \'${mcpToolName}\' is missing in definition`;
                  } else {
                    description = `Definition for MCP tool \'${mcpToolName}\' not available`;
                  }
                } catch (err) {
                  console.error(`Failed to process description for MCP tool ${subToolIdentifier}:`, err);
                  description = "Failed to load description for MCP tool";
                }
                descriptions[subToolIdentifier] = description;
              }));
            } else {
              // Handle Agent tools or Builtin tools
              const toolIdentifier = getToolIdentifier(tool);
              let description = "No description available";
              try {
                if (isAgentTool(tool)) {
                  description = tool.agent?.description || "Agent description not found";
                } else {
                  const toolProvider = getToolProvider(tool);
                  const foundToolDefinition = allToolDefinitions.find(def => def.provider === toolProvider) || null;

                  if (foundToolDefinition) {
                      description = foundToolDefinition.description || "Description not found";
                  }

                }
              } catch (err) {
                console.error(`Failed to process description for tool ${toolIdentifier}:`, err);
                description = "Failed to load description";
              }
              descriptions[toolIdentifier] = description;
            }
          }));
        }
        setToolDescriptions(descriptions);
      } catch (err) {
        console.error("Failed processing tool descriptions:", err);
      }
    };

    processToolDescriptions();
  }, [selectedTeam, allTools]);

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
            const mcpProvider = getToolProvider(tool) || "mcp_server";
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
            const provider = getToolProvider(tool) || "unknown";
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
                  {selectedTeam?.agent.metadata.name} ({selectedTeam?.model})
                </SidebarGroupLabel>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  asChild
                  aria-label={`Edit agent ${selectedTeam?.agent.metadata.name}`}
                >
                  <Link href={`/agents/new?edit=true&id=${selectedAgentId}`}>
                    <Edit className="h-3.5 w-3.5" />
                  </Link>
                </Button>
              </div>
              <p className="text-sm flex px-2 text-muted-foreground">{selectedTeam?.agent.spec.description}</p>
            </SidebarGroup>
            <SidebarGroup className="group-data-[collapsible=icon]:hidden">
              <SidebarGroupLabel>Tools & Agents</SidebarGroupLabel>
              {selectedTeam && renderAgentTools(selectedTeam.agent.spec.tools)}
            </SidebarGroup>
            <SidebarGroup className="group-data-[collapsible=icon]:hidden">
              <SidebarGroupLabel>Memory</SidebarGroupLabel>
              <SidebarMenu>
                {selectedTeam?.agent.spec.memory && selectedTeam.agent.spec.memory.length > 0 ? (
                  selectedTeam.agent.spec.memory.map((memoryName) => (
                    <SidebarMenuItem key={memoryName}>
                      <div className="flex justify-between items-center w-full">
                        <SidebarMenuButton className="justify-start" disabled>
                          <span className="truncate max-w-[180px]">{memoryName}</span>
                        </SidebarMenuButton>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7 ml-1"
                          onClick={(e) => {
                            e.stopPropagation();
                            router.push(`/memories/new?edit=${encodeURIComponent(memoryName)}`);
                          }}
                          aria-label={`Edit memory ${memoryName}`}
                        >
                          <Edit className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </SidebarMenuItem>
                  ))
                ) : (
                  <div className="flex items-center justify-between px-2">
                    <span className="text-sm italic text-muted-foreground">No memory configured</span>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7"
                      onClick={() => router.push('/memories/new')}
                      aria-label="Add new memory"
                    >
                      <Plus className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                )}
              </SidebarMenu>
            </SidebarGroup>
          </ScrollArea>
        </SidebarContent>
      </Sidebar>
    </>
  );
}
