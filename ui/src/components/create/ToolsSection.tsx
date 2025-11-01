import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
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
}

export const ToolsSection = ({ selectedTools, setSelectedTools, isSubmitting, onBlur, currentAgentName }: ToolsSectionProps) => {
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
          return {
            ...mcpTool,
            mcpServer: {
              ...mcpTool.mcpServer,
              toolNames: newToolNames,
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
            const displayName = mcpToolName;

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

            return (
              <Card key={toolIdentifierForDisplay}>
                <CardContent className="p-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center text-xs">
                      <div className="inline-flex space-x-2 items-center">
                        <Icon className={`h-4 w-4 ${iconColor}`} />
                        <div className="inline-flex flex-col space-y-1">
                          <span className="">{displayName}</span>
                          <span className="text-muted-foreground max-w-2xl">{displayDescription}</span>
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Button variant="ghost" size="sm" onClick={() => handleRemoveTool(parentToolIdentifier, mcpToolName)} disabled={isSubmitting}>
                        <X className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                </CardContent>
              </Card>
            );
          });
        } else {
          const displayName = getToolDisplayName(agentTool);
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
                <div className="flex items-center justify-between">
                  <div className="flex items-center text-xs">
                    <div className="inline-flex space-x-2 items-center">
                      <CurrentIcon className={`h-4 w-4 ${currentIconColor}`} />
                      <div className="inline-flex flex-col space-y-1">
                        <span className="">{displayName}</span>
                        <span className="text-muted-foreground max-w-2xl">{displayDescription}</span>
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
      />
    </div>
  );
};
