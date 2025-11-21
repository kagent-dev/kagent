"use client";

import * as React from "react";
import { useState, useEffect } from "react";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Plus, X, FunctionSquare, Loader2 } from "lucide-react";
import type { Tool, ToolNodeData, AgentResponse, ToolsResponse } from "@/types";
import { SelectToolsDialog } from "@/components/create/SelectToolsDialog";
import { getAgents } from "@/app/actions/agents";
import { getTools } from "@/app/actions/tools";
import { 
  isAgentTool, 
  isMcpTool, 
  getToolIdentifier, 
  getToolDisplayName, 
  getToolDescription 
} from "@/lib/toolUtils";
import KagentLogo from "@/components/kagent-logo";
import { k8sRefUtils } from "@/lib/k8sUtils";

interface ToolPropertyEditorProps {
  data: ToolNodeData;
  onUpdate: (data: Record<string, unknown>) => void;
}

export function ToolPropertyEditor({ data, onUpdate }: ToolPropertyEditorProps) {
  const nodeData = data as ToolPropertyEditorProps['data'];
  const [showToolSelector, setShowToolSelector] = useState(false);
  const [availableAgents, setAvailableAgents] = useState<AgentResponse[]>([]);
  const [availableTools, setAvailableTools] = useState<ToolsResponse[]>([]);
  const [loadingData, setLoadingData] = useState(true);
  
  const handleChange = (field: string, value: unknown) => {
    onUpdate({ ...nodeData, [field]: value });
  };

  // Fetch available tools and agents
  useEffect(() => {
    const fetchData = async () => {
      setLoadingData(true);
      
      try {
        const [agentsResponse, toolsResponse] = await Promise.all([
          getAgents(),
          getTools()
        ]);

        // Handle agents
        if (!agentsResponse.error && agentsResponse.data) {
          setAvailableAgents(agentsResponse.data);
        } else {
          console.error("Failed to fetch agents:", agentsResponse.error);
        }
        setAvailableTools(toolsResponse);
      } catch (error) {
        console.error("Failed to fetch data:", error);
      } finally {
        setLoadingData(false);
      }
    };

    fetchData();
  }, []);

  const handleToolSelect = (newSelectedTools: Tool[]) => {
    handleChange('tools', newSelectedTools);
    setShowToolSelector(false);
  };

  const handleRemoveTool = (toolIdentifier: string, mcpToolName?: string) => {
    const selectedTools = nodeData.tools || [];
    let updatedTools: Tool[] = [];

    if (mcpToolName) {
      // Remove specific MCP tool
      updatedTools = selectedTools.map((tool: Tool) => {
        if (getToolIdentifier(tool) === toolIdentifier && isMcpTool(tool)) {
          const mcpTool = tool as Tool;
          const updatedToolNames = mcpTool.mcpServer?.toolNames.filter(
            (name: string) => name !== mcpToolName
          ) || [];

          if (updatedToolNames.length === 0) {
            return null;
          }

          return {
            ...tool,
            mcpServer: {
              ...mcpTool.mcpServer!,
              toolNames: updatedToolNames,
            },
          };
        }
        return tool;
      }).filter((tool): tool is Tool => tool !== null);
    } else {
      // Remove entire tool/agent
      updatedTools = selectedTools.filter(
        (tool: Tool) => getToolIdentifier(tool) !== toolIdentifier
      );
    }

    handleChange('tools', updatedTools);
  };

  const renderSelectedTools = () => {
    const selectedTools = nodeData.tools || [];
    
    return (
      <div className="space-y-2">
        {selectedTools.flatMap((agentTool: Tool) => {
          const parentToolIdentifier = getToolIdentifier(agentTool);
    
          if (isMcpTool(agentTool)) {
            const mcpTool = agentTool as Tool;
            return mcpTool.mcpServer?.toolNames.map((mcpToolName: string) => {
              const toolIdentifierForDisplay = `${parentToolIdentifier}::${mcpToolName}`;
              const displayName = mcpToolName;

              // Get tool description from DB
              let displayDescription = "Description not available.";
              const toolFromDB = availableTools.find(server => {
                const { name } = k8sRefUtils.fromRef(server.server_name);
                return name === mcpTool.mcpServer?.name && server.id === mcpToolName;
              });

              if (toolFromDB) {
                displayDescription = toolFromDB.description;
              }

              const Icon = FunctionSquare;
              const iconColor = "text-blue-400";

              return (
                <Card key={toolIdentifierForDisplay}>
                  <CardContent className="p-3">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center text-xs min-w-0 flex-1">
                        <div className="inline-flex space-x-2 items-center min-w-0">
                          <Icon className={`h-4 w-4 flex-shrink-0 ${iconColor}`} />
                          <div className="inline-flex flex-col space-y-1 min-w-0">
                            <span className="font-medium truncate">{displayName}</span>
                            <span className="text-muted-foreground truncate">{displayDescription}</span>
                          </div>
                        </div>
                      </div>
                      <div className="flex items-center gap-2 flex-shrink-0">
                        <Button 
                          variant="ghost" 
                          size="sm" 
                          onClick={() => handleRemoveTool(parentToolIdentifier, mcpToolName)}
                          className="h-6 w-6 p-0"
                        >
                          <X className="h-3 w-3" />
                        </Button>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              );
            }) || [];
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

            return [(
              <Card key={parentToolIdentifier}>
                <CardContent className="p-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center text-xs min-w-0 flex-1">
                      <div className="inline-flex space-x-2 items-center min-w-0">
                        <CurrentIcon className={`h-4 w-4 flex-shrink-0 ${currentIconColor}`} />
                        <div className="inline-flex flex-col space-y-1 min-w-0">
                          <span className="font-medium truncate">{displayName}</span>
                          <span className="text-muted-foreground truncate">{displayDescription}</span>
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2 flex-shrink-0">
                      <Button 
                        variant="ghost" 
                        size="sm" 
                        onClick={() => handleRemoveTool(parentToolIdentifier)}
                        className="h-6 w-6 p-0"
                      >
                        <X className="h-3 w-3" />
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
  };

  return (
    <div className="space-y-4">
      <div>
        <Label className="text-xs mb-2 block">Tools & Agents</Label>
          <Button
            type="button"
            variant="outline"
          className="w-full justify-start text-sm h-9"
          onClick={() => setShowToolSelector(true)}
          disabled={loadingData}
        >
          {loadingData ? (
            <>
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              Loading...
            </>
          ) : (
            <>
              <Plus className="w-4 h-4 mr-2" />
              {nodeData.tools && nodeData.tools.length > 0
                ? `${nodeData.tools.length} tool(s) selected`
                : 'Select Tools & Agents'}
            </>
          )}
          </Button>
        </div>
        
      {nodeData.tools && nodeData.tools.length > 0 && (
        <div>
          <Label className="text-xs mb-2 block">Selected Tools</Label>
          {renderSelectedTools()}
        </div>
      )}

      <SelectToolsDialog
        open={showToolSelector}
        onOpenChange={setShowToolSelector}
        availableTools={availableTools}
        selectedTools={nodeData.tools || []}
        onToolsSelected={handleToolSelect}
        availableAgents={availableAgents}
        loadingAgents={loadingData}
      />
    </div>
  );
}
