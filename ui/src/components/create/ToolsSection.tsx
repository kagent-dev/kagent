import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogTitle, DialogHeader, DialogFooter, DialogDescription } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Plus, FunctionSquare, X, Settings2 } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useState, useEffect } from "react";
import { getToolDescription, getToolDisplayName, getToolIdentifier, isMcpTool } from "@/lib/data";
import { Label } from "../ui/label";
import { SelectToolsDialog } from "./SelectToolsDialog";
import { Component, ToolConfig } from "@/types/datamodel";

interface ToolsSectionProps {
  allTools: Component<ToolConfig>[];
  selectedTools: Component<ToolConfig>[];
  setSelectedTools: (tools: Component<ToolConfig>[]) => void;
  isSubmitting: boolean;
}

export const ToolsSection = ({ allTools, selectedTools, setSelectedTools, isSubmitting }: ToolsSectionProps) => {
  const [showToolSelector, setShowToolSelector] = useState(false);
  const [configTool, setConfigTool] = useState<Component<ToolConfig> | null>(null);
  const [showConfig, setShowConfig] = useState(false);
  const [toolConfigMap, setToolConfigMap] = useState<Record<string, unknown>>({});

  // Initialize toolConfigMap when selectedTools change
  useEffect(() => {
    const newToolConfigMap: Record<string, unknown> = {};
    selectedTools.forEach((tool) => {
      const toolId = getToolIdentifier(tool);
      newToolConfigMap[toolId] = { ...tool.config };
    });
    setToolConfigMap(newToolConfigMap);
  }, [selectedTools]);

  const openConfigDialog = (tool: Component<ToolConfig>) => {
    // Create a deep copy of the tool to avoid reference issues
    const toolCopy = JSON.parse(JSON.stringify(tool));
    setConfigTool(toolCopy);
    setShowConfig(true);
  };

  const handleConfigSave = () => {
    if (!configTool) return;

    const toolId = getToolIdentifier(configTool);

    // Update the toolConfigMap
    setToolConfigMap((prev) => ({
      ...prev,
      [toolId]: { ...configTool.config },
    }));

    // Update the selectedTools array with the new config
    const updatedTools = selectedTools.map((tool) => {
      if (getToolIdentifier(tool) === toolId) {
        return {
          ...tool,
          config: { ...configTool.config },
        };
      }
      return tool;
    });

    setSelectedTools(updatedTools);
    setShowConfig(false);
    setConfigTool(null);
  };

  const handleToolSelect = (newSelectedTools: Component<ToolConfig>[]) => {
    setSelectedTools(newSelectedTools);
    setShowToolSelector(false);
  };

  const handleRemoveTool = (tool: Component<ToolConfig>) => {
    const toolId = getToolIdentifier(tool);
    const updatedTools = selectedTools.filter((t) => getToolIdentifier(t) !== toolId);

    // Also remove from the config map
    const updatedConfigMap = { ...toolConfigMap };
    delete updatedConfigMap[toolId];
    setToolConfigMap(updatedConfigMap);

    setSelectedTools(updatedTools);
  };

  const handleConfigChange = (field: string, value: string) => {
    if (!configTool) return;

    setConfigTool((prevTool) => {
      if (!prevTool) return null;
      return {
        ...prevTool,
        config: {
          ...prevTool.config,
          [field]: value,
        },
      };
    });
  };

  const renderConfigDialog = () => {
    if (!configTool) return null;
    const configObj = configTool.config;

    if (!configObj || Object.keys(configObj).length === 0) {
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
            <DialogTitle>Configure {getToolDisplayName(configTool)}</DialogTitle>
            <DialogDescription>
              Configure the settings for <span className="text-primary">{getToolDisplayName(configTool)}</span>. These settings will be used when the tool is executed.
            </DialogDescription>
          </DialogHeader>

          <div className="py-4">
            <div className="space-y-4">
              {Object.keys(configObj)
                .filter((k) => k !== "description")
                .map((field: string) => (
                  <div key={field} className="space-y-2">
                    <Label htmlFor={field} className="flex items-center">
                      {field}
                    </Label>
                    <Input id={field} type="text" value={configObj[field as keyof ToolConfig] || ""} onChange={(e) => handleConfigChange(field, e.target.value)} />
                  </div>
                ))}
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
      {selectedTools.map((tool: Component<ToolConfig>) => {
        const displayName = getToolDisplayName(tool);
        const displayDescription = getToolDescription(tool);
        const toolIdentifier = getToolIdentifier(tool);
        return (
          <Card key={toolIdentifier}>
            <CardContent className="p-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center text-xs">
                  <div className="inline-flex space-x-2 items-center">
                    <FunctionSquare className={`h-4 w-4 ${isMcpTool(tool) ? "text-blue-400" : "text-yellow-500"}`} />
                    <div className="inline-flex flex-col space-y-1">
                      <span className="">{displayName}</span>
                      <span className="text-muted-foreground max-w-2xl">{displayDescription}</span>
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  {!isMcpTool(tool) && (
                    <Button variant="outline" size="sm" onClick={() => openConfigDialog(tool)} disabled={isSubmitting || !tool.config || Object.keys(tool.config).length === 0}>
                      <Settings2 className="h-4 w-4" />
                    </Button>
                  )}
                  <Button variant="ghost" size="sm" onClick={() => handleRemoveTool(tool)} disabled={isSubmitting}>
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
              <FunctionSquare className="h-12 w-12  mb-4" />
              <h4 className="text-lg font-medium  mb-2">No tools selected</h4>
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
      <SelectToolsDialog open={showToolSelector} onOpenChange={setShowToolSelector} availableTools={allTools} selectedTools={selectedTools} onToolsSelected={handleToolSelect} />
    </div>
  );
};
