import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogTitle, DialogHeader, DialogFooter, DialogDescription } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Plus, FunctionSquare, X, Settings2 } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useState } from "react";
import { getToolDescription, getToolDisplayName, getToolIdentifier, getToolProvider, isInlineTool, isMcpTool, isSameTool } from "@/lib/toolUtils";
import { Label } from "@/components/ui/label";
import { SelectToolsDialog } from "./SelectToolsDialog";
import { AgentTool, Component, ToolConfig } from "@/types/datamodel";
import { componentToAgentTool, findComponentForAgentTool } from "@/lib/toolUtils";
import { Textarea } from "@/components/ui/textarea";

interface ToolsSectionProps {
  allTools: Component<ToolConfig>[];
  selectedTools: AgentTool[];
  setSelectedTools: (tools: AgentTool[]) => void;
  isSubmitting: boolean;
  onBlur?: () => void;
}

export const ToolsSection = ({ allTools, selectedTools, setSelectedTools, isSubmitting, onBlur }: ToolsSectionProps) => {
  const [showToolSelector, setShowToolSelector] = useState(false);
  const [configTool, setConfigTool] = useState<AgentTool | null>(null);
  const [showConfig, setShowConfig] = useState(false);

  const selectedToolComponents = selectedTools.map(agentTool => {
    const component = findComponentForAgentTool(agentTool, allTools);
    return component;
  }).filter(Boolean) as Component<ToolConfig>[];

  const openConfigDialog = (agentTool: AgentTool) => {
    // Create a deep copy of the tool to avoid reference issues
    const toolCopy = JSON.parse(JSON.stringify(agentTool)) as AgentTool;
    setConfigTool(toolCopy);
    setShowConfig(true);
  };

  const handleConfigSave = () => {
    if (!configTool) return;

    // Update the selectedTools array with the new config
    const updatedTools = selectedTools.map((tool) => {
      if (isSameTool(tool, configTool)) {
        return configTool;
      }
      return tool;
    });

    setSelectedTools(updatedTools);
    setShowConfig(false);
    setConfigTool(null);
  };

  const handleToolSelect = (newSelectedTools: Component<ToolConfig>[]) => {
    // Convert Component<ToolConfig>[] to AgentTool[]
    const agentTools = newSelectedTools.map(componentToAgentTool);
    
    // Ensure MCP tools have the correct toolServer field set to the label
    // The label contains the name of the ToolServer CRD.
    const updatedAgentTools = agentTools.map(tool => {
      if (isMcpTool(tool) && tool.mcpServer) {
        // Find the corresponding component
        const component = findComponentForAgentTool(tool, allTools);
        if (component && component.label) {
          return {
            ...tool,
            mcpServer: {
              ...tool.mcpServer,
              toolServer: component.label
            }
          };
        }
      }
      return tool;
    });
    
    setSelectedTools(updatedAgentTools);
    setShowToolSelector(false);

    if (onBlur) {
      onBlur();
    }
  };

  const handleRemoveTool = (tool: AgentTool) => {
    const updatedTools = selectedTools.filter((t) => getToolIdentifier(t) !== getToolIdentifier(tool));
    setSelectedTools(updatedTools);
  };

  const handleConfigChange = (field: string, value: string) => {
    if (!configTool) return;

    setConfigTool((prevTool) => {
      if (!prevTool) return null;

      if (isMcpTool(prevTool) && field === "toolServer") {
        return {
          ...prevTool,
          mcpServer: {
            ...prevTool.mcpServer,
            toolServer: value
          }
        };
      } else if (isInlineTool(prevTool)) {
        return {
          ...prevTool,
          inline: {
            ...prevTool.inline,
            config: {
              ...prevTool.inline?.config, 
              [field]: value,
            },
          },
        };
      }
      
      return prevTool;
    });
  };

  const renderConfigDialog = () => {
    if (!configTool) return null;

    // Get the appropriate config object based on the tool type
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let configObj: Record<string, any> = {};
    let configTitle = "Configure Tool";

    if (isInlineTool(configTool) && configTool.inline) {
      configObj = configTool.inline.config || {};
      configTitle = `Configure ${configTool.inline.provider}`;
    } else if (isMcpTool(configTool) && configTool.mcpServer) {
      // For McpServer tools, we might not have direct configuration options
      // Or we might need to structure it differently
      configObj = {
        toolServer: configTool.mcpServer.toolServer,
        toolNames: configTool.mcpServer.toolNames.join(", "),
      };
      configTitle = `Configure McpServer Tool: ${configTool.mcpServer.toolServer}`;
    }

    if (Object.keys(configObj).length === 0) {
      return null;
    }

    return (
      <Dialog
        open={showConfig}
        onOpenChange={(open) => {
          if (!open) {
            setShowConfig(false);
            setConfigTool(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{configTitle}</DialogTitle>
            <DialogDescription>
              Configure the settings for <span className="text-primary">{getToolProvider(configTool)}</span>. These settings will be used when the tool is executed.
            </DialogDescription>
          </DialogHeader>

          <div className="py-4">
            <div className="space-y-4">
              {Object.keys(configObj)
                .filter((k) => k !== "description")
                .map((field: string) => {
                  // Handle different types of values
                  const value = configObj[field];
                  const isObject = typeof value === "object" && value !== null;

                  return (
                    <div key={field} className="space-y-2">
                      <Label htmlFor={field} className="flex items-center">
                        {field}
                      </Label>
                      {isObject ? (
                        // For objects and arrays, show them as JSON
                        <Textarea
                          id={field}
                          value={JSON.stringify(value, null, 2)}
                          onChange={(e) => {
                            try {
                              const parsed = JSON.parse(e.target.value);
                              handleConfigChange(field, parsed);
                            } catch (err) {
                              // Handle JSON parse error if needed
                              console.error("Invalid JSON", err);
                            }
                          }}
                          rows={4}
                        />
                      ) : (
                        // For simple values, use regular input
                        <Input id={field} type="text" value={String(value || "")} onChange={(e) => handleConfigChange(field, e.target.value)} />
                      )}
                    </div>
                  );
                })}
            </div>
          </div>

          <DialogFooter>
            <div className="flex justify-end gap-2 w-full">
              <Button
                variant="ghost"
                onClick={() => {
                  setShowConfig(false);
                  setConfigTool(null);
                }}
              >
                Cancel
              </Button>
              <Button className="bg-violet-500 hover:bg-violet-600 disabled:opacity-50" onClick={handleConfigSave}>
                Save Configuration
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  };

  const renderSelectedTools = () => (
    <div className="space-y-2">
      {selectedTools.map((agentTool: AgentTool) => {
        const displayName = getToolDisplayName(agentTool);
        const displayDescription = getToolDescription(agentTool);

        return (
          <Card key={getToolIdentifier(agentTool)}>
            <CardContent className="p-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center text-xs">
                  <div className="inline-flex space-x-2 items-center">
                    <FunctionSquare className={`h-4 w-4 ${isMcpTool(agentTool) ? "text-blue-400" : "text-yellow-500"}`} />
                    <div className="inline-flex flex-col space-y-1">
                      <span className="">{displayName}</span>
                      <span className="text-muted-foreground max-w-2xl">{displayDescription}</span>
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  {!isMcpTool(agentTool) && (
                    <Button variant="outline" size="sm" onClick={() => openConfigDialog(agentTool)} disabled={isSubmitting}>
                      <Settings2 className="h-4 w-4" />
                    </Button>
                  )}
                  <Button variant="ghost" size="sm" onClick={() => handleRemoveTool(agentTool)} disabled={isSubmitting}>
                    <X className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>
        );
      })}
    </div>
  );

  return (
    <div className="space-y-4">
      {selectedTools.length > 0 && (
        <div className="flex justify-between items-center">
          <h3 className="text-sm font-medium">Selected Tools</h3>
          <Button
            onClick={() => {
              setShowToolSelector(true);
            }}
            disabled={isSubmitting}
            variant="outline"
            className="border bg-transparent"
          >
            <Plus className="h-4 w-4 mr-2" />
            Add Tools
          </Button>
        </div>
      )}

      <ScrollArea>
        {selectedTools.length === 0 ? (
          <Card className="">
            <CardContent className="p-8 flex flex-col items-center justify-center text-center">
              <FunctionSquare className="h-12 w-12 mb-4" />
              <h4 className="text-lg font-medium mb-2">No tools selected</h4>
              <p className="text-muted-foreground text-sm mb-4">Add tools to enhance your agent</p>
              <Button
                onClick={() => {
                  setShowToolSelector(true);
                }}
                disabled={isSubmitting}
                variant="default"
                className="flex items-center"
              >
                <Plus className="h-4 w-4 mr-2" />
                Add Tools
              </Button>
            </CardContent>
          </Card>
        ) : (
          renderSelectedTools()
        )}
      </ScrollArea>

      {renderConfigDialog()}
      <SelectToolsDialog 
        open={showToolSelector} 
        onOpenChange={setShowToolSelector} 
        availableTools={allTools} 
        selectedTools={selectedToolComponents}
        onToolsSelected={handleToolSelect} 
      />
    </div>
  );
};