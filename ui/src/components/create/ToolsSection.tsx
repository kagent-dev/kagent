import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogTitle, DialogHeader, DialogFooter, DialogDescription } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Plus, FunctionSquare, X, Settings2 } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useState, useEffect } from "react";
import { getToolDescription, getToolDisplayName, getToolIdentifier, getToolProvider, isAgentTool, isBuiltinTool, isMcpTool, isSameTool, isMcpProvider } from "@/lib/toolUtils";
import { Label } from "@/components/ui/label";
import { SelectToolsDialog } from "./SelectToolsDialog";
import { Tool, Component, ToolConfig, AgentResponse } from "@/types/datamodel";
import { getTeams } from "@/app/actions/teams";
import { Textarea } from "@/components/ui/textarea";
import KagentLogo from "../kagent-logo";
import { Badge } from "@/components/ui/badge";

interface ToolsSectionProps {
  allTools: Component<ToolConfig>[];
  selectedTools: Tool[];
  setSelectedTools: (tools: Tool[]) => void;
  isSubmitting: boolean;
  onBlur?: () => void;
  currentAgentName?: string;
}

export const ToolsSection = ({ allTools, selectedTools, setSelectedTools, isSubmitting, onBlur, currentAgentName }: ToolsSectionProps) => {
  const [showToolSelector, setShowToolSelector] = useState(false);
  const [configTool, setConfigTool] = useState<Tool | null>(null);
  const [showConfig, setShowConfig] = useState(false);
  const [availableAgents, setAvailableAgents] = useState<AgentResponse[]>([]);
  const [loadingAgents, setLoadingAgents] = useState(true);

  useEffect(() => {
    const fetchAgents = async () => {
      setLoadingAgents(true);
      const response = await getTeams();
      if (response.success && response.data) {
        const filteredAgents = currentAgentName
          ? response.data.filter((agentResp: AgentResponse) => agentResp.agent.metadata.name !== currentAgentName)
          : response.data;
        setAvailableAgents(filteredAgents);
      } else {
        console.error("Failed to fetch agents:", response.error);
      }
      setLoadingAgents(false);
    };

    fetchAgents();
  }, [currentAgentName]);

  const openConfigDialog = (agentTool: Tool) => {
    const toolCopy = JSON.parse(JSON.stringify(agentTool)) as Tool;
    setConfigTool(toolCopy);
    setShowConfig(true);
  };

  const handleConfigSave = () => {
    if (!configTool) return;

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
        if (getToolIdentifier(tool) === parentToolIdentifier && isMcpTool(tool) && tool.mcpServer) {
          const newToolNames = tool.mcpServer.toolNames.filter(name => name !== mcpToolNameToRemove);
          if (newToolNames.length === 0) {
            return null; 
          }
          return {
            ...tool,
            mcpServer: {
              ...tool.mcpServer,
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
      } else if (isBuiltinTool(prevTool)) {
        return {
          ...prevTool,
          builtin: {
            ...prevTool.builtin,
            config: {
              ...prevTool.builtin?.config,
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

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let configObj: Record<string, any> = {};
    let configTitle = "Configure Tool";

    if (isBuiltinTool(configTool) && configTool.builtin) {
      configObj = configTool.builtin.config || {};
      configTitle = `Configure ${configTool.builtin.name}`;
    } else if (isMcpTool(configTool) && configTool.mcpServer) {
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
                  const value = configObj[field];
                  const isObject = typeof value === "object" && value !== null;

                  return (
                    <div key={field} className="space-y-2">
                      <Label htmlFor={field} className="flex items-center">
                        {field}
                      </Label>
                      {isObject ? (
                        <Textarea
                          id={field}
                          value={JSON.stringify(value, null, 2)}
                          onChange={(e) => {
                            try {
                              const parsed = JSON.parse(e.target.value);
                              handleConfigChange(field, parsed);
                            } catch (err) {
                              console.error("Invalid JSON", err);
                            }
                          }}
                          rows={4}
                        />
                      ) : (
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
      {selectedTools.map((agentTool: Tool) => {
        const parentToolIdentifier = getToolIdentifier(agentTool);

        if (isMcpTool(agentTool) && agentTool.mcpServer && agentTool.mcpServer.toolNames && agentTool.mcpServer.toolNames.length > 0) {
          // For MCP tools, show all tool names under a single entry
          const displayName = agentTool.mcpServer.toolServer;
          const toolNames = agentTool.mcpServer.toolNames;

          let displayDescription = "Description not available.";
          const mcpToolDef = allTools.find(def =>
            isMcpProvider(def.provider) &&
            (def.config as ToolConfig & { tool?: { name: string, description?: string } })?.tool?.name === toolNames[0]
          );

          if (mcpToolDef) {
            displayDescription = getToolDescription(mcpToolDef);
          }

          return (
            <div key={parentToolIdentifier} className="flex items-center justify-between p-3 border rounded-md bg-muted/30">
              <div className="flex items-center gap-2 flex-1 overflow-hidden">
                <FunctionSquare className="h-4 w-4 flex-shrink-0 text-blue-400" />
                <div className="flex-1 overflow-hidden">
                  <p className="text-sm font-medium truncate">{displayName}</p>
                  <p className="text-xs text-muted-foreground truncate">{displayDescription}</p>
                  <div className="flex flex-wrap gap-1 mt-1">
                    {toolNames.map((toolName) => (
                      <Badge key={toolName} variant="secondary" className="text-xs">
                        {toolName}
                      </Badge>
                    ))}
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8"
                  onClick={() => openConfigDialog(agentTool)}
                >
                  <Settings2 className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8"
                  onClick={() => handleRemoveTool(parentToolIdentifier)}
                >
                  <X className="h-4 w-4" />
                </Button>
              </div>
            </div>
          );
        } else if (isBuiltinTool(agentTool) && agentTool.builtin) {
          const displayName = agentTool.builtin.label || agentTool.builtin.name;
          const displayDescription = agentTool.builtin.description || "No description available.";

          return (
            <div key={parentToolIdentifier} className="flex items-center justify-between p-3 border rounded-md bg-muted/30">
              <div className="flex items-center gap-2 flex-1 overflow-hidden">
                <FunctionSquare className="h-4 w-4 flex-shrink-0 text-yellow-500" />
                <div className="flex-1 overflow-hidden">
                  <p className="text-sm font-medium truncate">{displayName}</p>
                  <p className="text-xs text-muted-foreground truncate">{displayDescription}</p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8"
                  onClick={() => openConfigDialog(agentTool)}
                >
                  <Settings2 className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8"
                  onClick={() => handleRemoveTool(parentToolIdentifier)}
                >
                  <X className="h-4 w-4" />
                </Button>
              </div>
            </div>
          );
        } else if (isAgentTool(agentTool) && agentTool.agent) {
          const displayName = agentTool.agent.ref;
          const displayDescription = agentTool.agent.description || "No description available.";

          return (
            <div key={parentToolIdentifier} className="flex items-center justify-between p-3 border rounded-md bg-muted/30">
              <div className="flex items-center gap-2 flex-1 overflow-hidden">
                <KagentLogo className="h-4 w-4 flex-shrink-0 text-green-500" />
                <div className="flex-1 overflow-hidden">
                  <p className="text-sm font-medium truncate">{displayName}</p>
                  <p className="text-xs text-muted-foreground truncate">{displayDescription}</p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8"
                  onClick={() => openConfigDialog(agentTool)}
                >
                  <Settings2 className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8"
                  onClick={() => handleRemoveTool(parentToolIdentifier)}
                >
                  <X className="h-4 w-4" />
                </Button>
              </div>
            </div>
          );
        }
        return null;
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

      {renderConfigDialog()}
      <SelectToolsDialog
        open={showToolSelector}
        onOpenChange={setShowToolSelector}
        availableTools={allTools}
        availableAgents={availableAgents}
        selectedTools={selectedTools}
        onToolsSelected={handleToolSelect}
        loadingAgents={loadingAgents}
      />
    </div>
  );
};
