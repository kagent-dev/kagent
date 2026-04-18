import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Plus, FunctionSquare, X } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useState, useEffect } from "react";
import { isAgentTool, isMcpTool, getToolDescription, getToolIdentifier, getToolDisplayName, serverNamesMatch } from "@/lib/toolUtils";
import { SelectToolsDialog } from "./SelectToolsDialog";
import type { Tool, AgentResponse, ToolsResponse } from "@/types";
import { getAgents } from "@/app/actions/agents";
import { getTools } from "@/app/actions/tools";
import KagentLogo from "../kagent-logo";

interface ToolsSectionProps {
  selectedTools: Tool[];
  setSelectedTools: (tools: Tool[]) => void;
  isSubmitting: boolean;
  onBlur?: () => void;
  currentAgentName: string;
  currentAgentNamespace: string;
}

export const ToolsSection = ({ selectedTools, setSelectedTools, isSubmitting, onBlur, currentAgentName, currentAgentNamespace }: ToolsSectionProps) => {
  const [showToolSelector, setShowToolSelector] = useState(false);
  const [availableAgents, setAvailableAgents] = useState<AgentResponse[]>([]);
  const [loadingAgents, setLoadingAgents] = useState(true);
  const [availableTools, setAvailableTools] = useState<ToolsResponse[]>([]);

  useEffect(() => {
    const fetchData = async () => {
      setLoadingAgents(true);
      
      try {
        const [agentsResponse, toolsResponse] = await Promise.all([
          getAgents(),
          getTools()
        ]);

        // Handle agents
        if (!agentsResponse.error && agentsResponse.data) {
          const filteredAgents = currentAgentName
            ? agentsResponse.data.filter((agentResp: AgentResponse) => agentResp.agent.metadata.name !== currentAgentName)
            : agentsResponse.data;
          setAvailableAgents(filteredAgents);
        } else {
          console.error("Failed to fetch agents:", agentsResponse.error);
        }
        setAvailableTools(toolsResponse);
      } catch (error) {
        console.error("Failed to fetch data:", error);
      } finally {
        setLoadingAgents(false);
      }
    };

    fetchData();
  }, [currentAgentName]);

  const handleToolSelect = (newSelectedTools: Tool[]) => {
    setSelectedTools(newSelectedTools);
    setShowToolSelector(false);

    if (onBlur) {
      onBlur();
    }
  };

  const setRequireApprovalForMcpTool = (
    parentToolIdentifier: string,
    mcpToolName: string,
    requireApproval: boolean
  ) => {
    setSelectedTools(
      selectedTools.map((tool) => {
        if (getToolIdentifier(tool) !== parentToolIdentifier || !isMcpTool(tool)) {
          return tool;
        }
        const mcp = tool.mcpServer!;
        const names = new Set(mcp.requireApproval || []);
        if (requireApproval) {
          names.add(mcpToolName);
        } else {
          names.delete(mcpToolName);
        }
        const next = Array.from(names);
        return {
          ...tool,
          mcpServer: {
            ...mcp,
            ...(next.length > 0 ? { requireApproval: next } : { requireApproval: undefined }),
          },
        };
      })
    );
  };

  const handleRemoveTool = (parentToolIdentifier: string, mcpToolNameToRemove?: string) => {
    let updatedTools: Tool[];

    if (mcpToolNameToRemove) {
      updatedTools = selectedTools.map(tool => {
        if (getToolIdentifier(tool) === parentToolIdentifier && isMcpTool(tool)) {
          const mcpTool = tool as Tool;
          const newToolNames = mcpTool.mcpServer?.toolNames.filter(name => name !== mcpToolNameToRemove) || [];
          if (newToolNames.length === 0) {
            return null; 
          }
          const prevApproval = mcpTool.mcpServer?.requireApproval || [];
          const newRequireApproval = prevApproval.filter((n) => n !== mcpToolNameToRemove);
          return {
            ...mcpTool,
            mcpServer: {
              ...mcpTool.mcpServer,
              toolNames: newToolNames,
              ...(newRequireApproval.length > 0
                ? { requireApproval: newRequireApproval }
                : { requireApproval: undefined }),
            },
          };
        }
        return tool;
      }).filter(Boolean) as Tool[];
    } else {
      updatedTools = selectedTools.filter(t => getToolIdentifier(t) !== parentToolIdentifier);
    }
    setSelectedTools(updatedTools);
  };

  const renderSelectedTools = () => (
    <div className="space-y-2">
      {selectedTools.flatMap((agentTool: Tool) => {
        const parentToolIdentifier = getToolIdentifier(agentTool);

        if (isMcpTool(agentTool)) {
          const mcpTool = agentTool as Tool;
          return mcpTool.mcpServer?.toolNames.map((mcpToolName: string) => {
            const toolIdentifierForDisplay = `${parentToolIdentifier}::${mcpToolName}`;
            
            // Show server name with namespace for consistency
            const serverName = mcpTool.mcpServer?.name || "";
            const serverNamespace = mcpTool.mcpServer?.namespace || currentAgentNamespace;
            const serverDisplayName = `${serverNamespace}/${serverName}`;
            const displayName = `${mcpToolName} (${serverDisplayName})`;

            // The tools on agent resource don't have descriptions, so we need to 
            // get the descriptions from the db
            let displayDescription = "Description not available.";
            const toolFromDB = availableTools.find(server => {
              // Compare server names
              const serverMatch = serverNamesMatch(server.server_name, mcpTool.mcpServer?.name || "");
              // also check if the tool ID matches
              const toolIdMatch = server.id === mcpToolName;
              return serverMatch && toolIdMatch;
            });

            if (toolFromDB) {
              displayDescription = toolFromDB.description;
            }

            const Icon = FunctionSquare;
            const iconColor = "text-blue-400";
            const approvalSet = new Set(mcpTool.mcpServer?.requireApproval || []);
            const requiresApproval = approvalSet.has(mcpToolName);
            const approvalFieldId = `require-approval-${toolIdentifierForDisplay}`.replace(
              /[^a-zA-Z0-9_-]/g,
              "_"
            );

            return (
              <Card key={toolIdentifierForDisplay}>
                <CardContent className="space-y-1.5 p-3">
                  <div className="flex min-w-0 items-start gap-2">
                    <Icon className={`mt-0.5 h-4 w-4 shrink-0 ${iconColor}`} />
                    <div className="min-w-0 flex-1 text-xs">
                      <p className="font-medium leading-tight" title={displayName}>
                        <span className="line-clamp-2 break-words">{displayName}</span>
                      </p>
                      <p
                        className="mt-0.5 text-muted-foreground line-clamp-1 break-words leading-snug"
                        title={displayDescription}
                      >
                        {displayDescription}
                      </p>
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-8 w-8 shrink-0 p-0"
                      onClick={() => handleRemoveTool(parentToolIdentifier, mcpToolName)}
                      disabled={isSubmitting}
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                  <div className="flex min-w-0 items-center gap-2 border-t border-border/60 pt-1.5">
                    <Switch
                      id={approvalFieldId}
                      checked={requiresApproval}
                      disabled={isSubmitting}
                      onCheckedChange={(checked) =>
                        setRequireApprovalForMcpTool(parentToolIdentifier, mcpToolName, checked)
                      }
                    />
                    <Label
                      htmlFor={approvalFieldId}
                      className="min-w-0 cursor-pointer text-xs font-normal leading-snug"
                    >
                      <span className="line-clamp-2 sm:line-clamp-1">Require approval before this tool runs</span>
                    </Label>
                  </div>
                </CardContent>
              </Card>
            );
          });
        } else {
          const displayName = getToolDisplayName(agentTool, currentAgentNamespace);
          const displayDescription = getToolDescription(agentTool, availableTools);

          let CurrentIcon: React.ElementType;
          let currentIconColor: string;

          if (isAgentTool(agentTool)) {
            CurrentIcon = KagentLogo;
            currentIconColor = "text-green-500";
          } else {
            CurrentIcon = FunctionSquare;
            currentIconColor = "text-yellow-500";
          }

          return [( // flatMap expects an array
            <Card key={parentToolIdentifier}>
              <CardContent className="p-4">
                <div className="flex min-w-0 w-full items-center justify-between gap-2">
                  <div className="flex min-w-0 flex-1 items-center text-xs">
                    <div className="inline-flex min-w-0 flex-1 space-x-2 items-start">
                      <CurrentIcon className={`h-4 w-4 mt-0.5 shrink-0 ${currentIconColor}`} />
                      <div className="inline-flex min-w-0 flex-1 flex-col space-y-1">
                        <span className="truncate">{displayName}</span>
                        <span className="text-muted-foreground line-clamp-2 break-words">{displayDescription}</span>
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button variant="ghost" size="sm" onClick={() => handleRemoveTool(parentToolIdentifier)} disabled={isSubmitting}>
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          )];
        }
      })}
    </div>
  );

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground leading-relaxed">
        <span className="font-medium text-foreground">Require approval</span> appears for each{" "}
        <span className="font-medium text-foreground">MCP tool</span> (blue icon). It is not shown for agents used as tools (green icon); configure those sub-agents separately if they expose MCP tools.
      </p>
      {selectedTools.length > 0 && (
        <div className="flex justify-between items-center">
          <h3 className="text-sm font-medium">Selected Tools and Agents</h3>
          <Button
            onClick={() => {
              setShowToolSelector(true);
            }}
            disabled={isSubmitting}
            variant="outline"
            className="border bg-transparent"
          >
            <Plus className="h-4 w-4 mr-2" />
            Add Tools & Agents
          </Button>
        </div>
      )}

      <ScrollArea>
        {selectedTools.length === 0 ? (
          <Card className="">
            <CardContent className="p-8 flex flex-col items-center justify-center text-center">
              <KagentLogo className="h-12 w-12 mb-4" />
              <h4 className="text-lg font-medium mb-2">No tools or agents selected</h4>
              <p className="text-muted-foreground text-sm mb-4">Add tools or agents to enhance your agent</p>
              <Button
                onClick={() => {
                  setShowToolSelector(true);
                }}
                disabled={isSubmitting}
                variant="default"
                className="flex items-center"
              >
                <Plus className="h-4 w-4 mr-2" />
                Add Tools & Agents
              </Button>
            </CardContent>
          </Card>
        ) : (
          renderSelectedTools()
        )}
      </ScrollArea>

     <SelectToolsDialog
        open={showToolSelector}
        onOpenChange={setShowToolSelector}
        availableTools={availableTools}
        availableAgents={availableAgents}
        selectedTools={selectedTools}
        onToolsSelected={handleToolSelect}
        loadingAgents={loadingAgents}
        currentAgentNamespace={currentAgentNamespace}
      />
    </div>
  );
};
