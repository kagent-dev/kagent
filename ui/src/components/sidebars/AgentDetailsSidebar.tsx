"use client";

import { useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";
import type { AgentResponse, Tool, Component, ToolConfig, MCPToolConfig } from "@/types/datamodel";
import { SidebarHeader, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "@/components/ui/sidebar";
import { AgentActions } from "./AgentActions";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingState } from "@/components/LoadingState";
import { getToolIdentifier, getToolProvider, getToolDisplayName, SSE_MCP_TOOL_PROVIDER_NAME, STDIO_MCP_TOOL_PROVIDER_NAME, isAgentTool, isMcpProvider } from "@/lib/toolUtils";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";

interface AgentDetailsSidebarProps {
  selectedAgentId: number;
  currentAgent: AgentResponse;
  allTools: Component<ToolConfig>[];
}

export function AgentDetailsSidebar({ selectedAgentId, currentAgent, allTools }: AgentDetailsSidebarProps) {
  const [toolDescriptions, setToolDescriptions] = useState<Record<string, string>>({});
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});

  const selectedTeam = currentAgent;

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
                  foundToolDefinition = allToolDefinitions.find(
                    (def) => def.provider === SSE_MCP_TOOL_PROVIDER_NAME && (def.config as MCPToolConfig)?.tool?.name === toolName
                  ) || allToolDefinitions.find(
                    (def) => def.provider === STDIO_MCP_TOOL_PROVIDER_NAME && (def.config as MCPToolConfig)?.tool?.name === toolName
                  ) || null;
                } else if (tool.type === "Builtin") {
                  foundToolDefinition = allToolDefinitions.find(def => def.provider === toolProvider) || null;
                }

                if (foundToolDefinition && isMcpProvider(foundToolDefinition.provider)) {
                  const nestedDescription = (foundToolDefinition.config as MCPToolConfig)?.tool?.description;
                  if (nestedDescription) {
                    description = nestedDescription;
                  }
                } else if (foundToolDefinition?.description) {
                  description = foundToolDefinition.description;
                }
              }
            } catch (err) {
              console.error(`Failed to process description for tool ${toolIdentifier}:`, err);
              description = "Failed to load description";
            }
            descriptions[toolIdentifier] = description;
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
            <SidebarGroup className="group-data-[collapsible=icon]:hidden">
              <SidebarGroupLabel>Memory</SidebarGroupLabel>
              <SidebarMenu>
                {selectedTeam?.agent.spec.memory && selectedTeam.agent.spec.memory.length > 0 ? (
                  selectedTeam.agent.spec.memory.map((memoryName) => (
                    <SidebarMenuItem key={memoryName}>
                      <SidebarMenuButton className="justify-start" disabled>
                        <span className="truncate max-w-[200px]">{memoryName}</span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))
                ) : (
                  <div className="text-sm italic px-2 text-muted-foreground">No memory configured</div>
                )}
              </SidebarMenu>
            </SidebarGroup>
          </ScrollArea>
        </SidebarContent>
      </Sidebar>
    </>
  );
}
