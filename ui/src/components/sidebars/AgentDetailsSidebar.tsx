"use client";

import { useEffect, useState } from "react";
import { ChevronRight, Edit, ShieldAlert } from "lucide-react";
import type { AgentResponse, Tool, ToolsResponse } from "@/types";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@/components/ui/sheet";
import { SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "@/components/ui/sidebar";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingState } from "@/components/LoadingState";
import { isAgentTool, isMcpTool, getToolDescription, getToolIdentifier, getToolDisplayName } from "@/lib/toolUtils";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import Link from "next/link";
import { getAgents } from "@/app/actions/agents";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Badge } from "@/components/ui/badge";

interface AgentDetailsSidebarProps {
  selectedAgentName: string;
  currentAgent: AgentResponse;
  allTools: ToolsResponse[];
  open: boolean;
  onClose: () => void;
}

export function AgentDetailsSidebar({ selectedAgentName, currentAgent, allTools, open, onClose }: AgentDetailsSidebarProps) {
  const [toolDescriptions, setToolDescriptions] = useState<Record<string, string>>({});
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});
  const [availableAgents, setAvailableAgents] = useState<AgentResponse[]>([]);

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
    requiresApproval,
    isExpanded,
    onToggleExpansion,
  }: {
    itemKey: string;
    displayName: string;
    providerTooltip: string;
    description: string;
    requiresApproval?: boolean;
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
                <div className="flex items-center gap-1">
                  {requiresApproval && (
                    <ShieldAlert className="h-3.5 w-3.5 text-amber-500 shrink-0" />
                  )}
                  <ChevronRight
                    className={cn(
                      "h-4 w-4 transition-transform duration-200",
                      isExpanded && "rotate-90"
                    )}
                  />
                </div>
              </div>
            </SidebarMenuButton>
          </CollapsibleTrigger>
          <CollapsibleContent className="px-2 py-1">
            <div className="rounded-md bg-muted/50 p-2">
              <p className="text-sm text-muted-foreground">{description}</p>
              {requiresApproval && (
                <p className="text-xs text-amber-600 dark:text-amber-400 mt-1">Requires approval before execution</p>
              )}
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

    const agentNamespace = currentAgent.agent.metadata.namespace || "";

    // Group MCP tools by server, collect agent tools separately
    const mcpServerGroups = new Map<string, { serverDisplayName: string; toolNames: string[]; approvalSet: Set<string>; baseToolIdentifier: string }>();
    const agentTools: Tool[] = [];

    tools.forEach((tool) => {
      if (tool.mcpServer && tool.mcpServer?.toolNames && tool.mcpServer.toolNames.length > 0) {
        const serverKey = tool.mcpServer.name || "unknown";
        const existing = mcpServerGroups.get(serverKey);
        if (existing) {
          tool.mcpServer.toolNames.forEach(name => existing.toolNames.push(name));
          (tool.mcpServer.requireApproval || []).forEach(name => existing.approvalSet.add(name));
        } else {
          mcpServerGroups.set(serverKey, {
            serverDisplayName: `${tool.mcpServer.namespace || agentNamespace}/${tool.mcpServer.name || ""}`,
            toolNames: [...tool.mcpServer.toolNames],
            approvalSet: new Set(tool.mcpServer.requireApproval || []),
            baseToolIdentifier: getToolIdentifier(tool),
          });
        }
      } else {
        agentTools.push(tool);
      }
    });

    return (
      <div className="space-y-3">
        {Array.from(mcpServerGroups.entries()).map(([serverKey, group]) => (
          <div key={serverKey}>
            <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase tracking-wide">
              {group.serverDisplayName}
            </div>
            <SidebarMenu>
              {group.toolNames.map((mcpToolName) => {
                const subToolIdentifier = `${group.baseToolIdentifier}::${mcpToolName}`;
                const description = toolDescriptions[subToolIdentifier] || "Description loading or unavailable";
                const isExpanded = expandedTools[subToolIdentifier] || false;

                return (
                  <RenderToolCollapsibleItem
                    key={subToolIdentifier}
                    itemKey={subToolIdentifier}
                    displayName={mcpToolName}
                    providerTooltip={serverKey}
                    description={description}
                    requiresApproval={group.approvalSet.has(mcpToolName)}
                    isExpanded={isExpanded}
                    onToggleExpansion={() => toggleToolExpansion(subToolIdentifier)}
                  />
                );
              })}
            </SidebarMenu>
          </div>
        ))}
        {agentTools.length > 0 && (
          <div>
            <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase tracking-wide">
              Agents
            </div>
            <SidebarMenu>
              {agentTools.map((tool) => {
                const toolIdentifier = getToolIdentifier(tool);
                const displayName = getToolDisplayName(tool, agentNamespace);
                const description = toolDescriptions[toolIdentifier] || "Description loading or unavailable";
                const isExpanded = expandedTools[toolIdentifier] || false;

                return (
                  <RenderToolCollapsibleItem
                    key={toolIdentifier}
                    itemKey={toolIdentifier}
                    displayName={displayName}
                    providerTooltip={isAgentTool(tool) ? (tool.agent?.name || "unknown") : "unknown"}
                    description={description}
                    isExpanded={isExpanded}
                    onToggleExpansion={() => toggleToolExpansion(toolIdentifier)}
                  />
                );
              })}
            </SidebarMenu>
          </div>
        )}
      </div>
    );
  };

    // Check if agent is BYO type
  const isDeclarativeAgent = selectedTeam?.agent.spec.type === "Declarative";
  
  return (
    <Sheet open={open} onOpenChange={onClose}>
      <SheetContent side="right" className="p-0 overflow-hidden">
        <SheetHeader className="px-4 py-3 border-b">
          <SheetTitle className="text-base">Agent Details</SheetTitle>
          <SheetDescription className="sr-only">Details about the selected agent</SheetDescription>
        </SheetHeader>
        <ScrollArea className="h-[calc(100vh-4rem)]">
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
            {isDeclarativeAgent &&(
              <SidebarGroup className="group-data-[collapsible=icon]:hidden">
                <SidebarGroupLabel>Tools & Agents</SidebarGroupLabel>
                {selectedTeam && renderAgentTools(selectedTeam.tools)}
              </SidebarGroup>
            )}

            {isDeclarativeAgent && selectedTeam?.agent.spec?.skills?.refs && selectedTeam.agent.spec.skills.refs.length > 0 && (
              <SidebarGroup className="group-data-[collapsible=icon]:hidden">
                <div className="flex items-center justify-between px-2 mb-2">
                  <SidebarGroupLabel className="mb-0">Skills</SidebarGroupLabel>
                  <Badge variant="secondary" className="h-5">
                    {selectedTeam.agent.spec.skills.refs.length}
                  </Badge>
                </div>
                <SidebarMenu>
                  <TooltipProvider>
                    {selectedTeam.agent.spec.skills.refs.map((skillRef, index) => {
                      // Parse OCI image reference: [registry/]repository[:tag][@digest]
                      // Groups: (1) registry, (2) repository, (3) tag, (4) digest
                      const refMatch = skillRef.match(
                        /^(?:((?:[a-zA-Z0-9-]+\.)+[a-zA-Z0-9-]+(?::\d+)?|localhost(?::\d+)?|[a-zA-Z0-9-]+:\d+)\/)?([^:@]+)(?::([^@]+))?(?:@(.+))?$/
                      );
                      const registry = refMatch?.[1] ?? null;
                      const repoName = refMatch?.[2] ?? null;
                      const tag = refMatch?.[3] ?? null;
                      const digest = refMatch?.[4] ?? null;

                      // Only show a version badge when the ref was successfully parsed.
                      // Truncate digests to keep the badge compact.
                      const versionBadge = refMatch
                        ? tag ?? (digest ? (digest.length > 16 ? digest.substring(0, 16) + "\u2026" : digest) : "latest")
                        : null;
                      const displayName = repoName ?? skillRef;
                      return (
                        <SidebarMenuItem key={index}>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <SidebarMenuButton className="w-full h-auto py-2">
                                <div className="flex flex-col items-start w-full min-w-0 gap-0.5">
                                  <div className="flex items-center w-full justify-between gap-2">
                                    <span className="truncate text-sm font-medium leading-tight">{displayName}</span>
                                    {versionBadge && (
                                      <span className="shrink-0 text-[10px] bg-muted px-1.5 py-0.5 rounded-sm text-muted-foreground font-mono">
                                        {versionBadge}
                                      </span>
                                    )}
                                  </div>
                                  {registry && (
                                    <span className="truncate w-full text-xs text-muted-foreground leading-tight" title={registry}>
                                      {registry}
                                    </span>
                                  )}
                                </div>
                              </SidebarMenuButton>
                            </TooltipTrigger>
                            <TooltipContent side="left">
                              <p className="max-w-xs break-all">{skillRef}</p>
                            </TooltipContent>
                          </Tooltip>
                        </SidebarMenuItem>
                      );
                    })}
                  </TooltipProvider>
                </SidebarMenu>
              </SidebarGroup>
            )}

        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}
