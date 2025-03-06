import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogTitle, DialogHeader, DialogFooter } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Plus, FunctionSquare, X, Settings2, Download } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useState } from "react";
import { getToolDescription, getToolDisplayName, getToolIdentifier, isMcpTool } from "@/lib/data";
import { Label } from "../ui/label";
import { DiscoverToolsDialog } from "./DiscoverToolsDialog";
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
  const [showDiscoverTools, setShowDiscoverTools] = useState(false);
  const [configTool, setConfigTool] = useState<Component<ToolConfig> | null>(null);
  const [showConfig, setShowConfig] = useState(false);
  const [discoveredToolsForSelection, setDiscoveredToolsForSelection] = useState<Component<ToolConfig>[]>([]);

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const handleConfigSave = (toolProvider: string, newConfig: any) => {
    if (!configTool) return;

    // TODO: this might not be correct (the getToolident)
    const updatedTools = selectedTools.map((tool) => (getToolIdentifier(tool) === toolProvider ? { ...tool, config: newConfig } : tool));
    setSelectedTools(updatedTools);
    setShowConfig(false);
    setConfigTool(null);
  };

  const handleToolSelect = (newSelectedTools: Component<ToolConfig>[]) => {
    setSelectedTools(newSelectedTools);
    setShowToolSelector(false);
    setDiscoveredToolsForSelection([]);
  };

  const handleRemoveTool = (toolProvider: string) => {
    const updatedTools = selectedTools.filter((tool) => getToolIdentifier(tool) !== toolProvider);
    setSelectedTools(updatedTools);
  };

  const handleShowSelectTools = (discoveredTools: Component<ToolConfig>[]) => {
    // This will be called from the DiscoverToolsDialog when tools are found
    const newTools = discoveredTools.filter((tool) => !selectedTools.some((selected) => getToolIdentifier(selected) === getToolIdentifier(tool)));

    if (newTools.length > 0) {
      setDiscoveredToolsForSelection(newTools);
      setShowToolSelector(true);
    }
  };

  const renderConfigDialog = () => {
    if (!configTool) return null;

    const configObj = configTool.config;

    return (
      <Dialog open={showConfig} onOpenChange={setShowConfig}>
        <DialogContent className="bg-[#2A2A2A] border-[#3A3A3A] text-white">
          <DialogHeader>
            <DialogTitle>Configure {getToolDisplayName(configTool)}</DialogTitle>
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
                    <Input
                      id={field}
                      type="text"
                      value={configObj[field as keyof typeof configObj] || ""}
                      onChange={(e) => {
                        const newConfig = {
                          ...configTool.config,
                          [field]: e.target.value,
                        };

                        setConfigTool({
                          ...configTool,
                          config: newConfig,
                        });
                      }}
                      className={`bg-[#1A1A1A] border-[#3A3A3A]`}
                    />
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
              <Button className="bg-violet-500 hover:bg-violet-600 disabled:opacity-50" onClick={() => handleConfigSave(configTool.provider, configTool.config)}>
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
        return (
          <Card key={`${getToolIdentifier(tool)}`} className="bg-[#1A1A1A] border-[#3A3A3A]">
            <CardContent className="p-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center text-xs spac">
                  <div className="inline-flex space-x-2 items-center">
                    <FunctionSquare className={`h-4 w-4 ${isMcpTool(tool) ? "text-blue-400" : "text-yellow-500"}`} />
                    <div className="inline-flex flex-col space-y-1">
                      <span className="text-white">{displayName}</span>
                      <span className="text-white/50 max-w-2xl">{displayDescription}</span>
                      {isMcpTool(tool) && <span className="text-blue-400/70 text-xs">MCP Tool</span>}
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  {!isMcpTool(tool) && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setConfigTool(tool);
                        setShowConfig(true);
                      }}
                      disabled={isSubmitting}
                    >
                      <Settings2 className="h-4 w-4" />
                    </Button>
                  )}
                  <Button variant="ghost" size="sm" onClick={() => handleRemoveTool(tool.provider)} disabled={isSubmitting} className="text-white/50">
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

  // Calculate available tools for selection
  const availableToolsForSelection =
    discoveredToolsForSelection.length > 0 ? [...allTools, ...discoveredToolsForSelection.filter((dTool) => !allTools.some((aTool) => aTool.provider === dTool.provider))] : allTools;

  return (
    <div className="space-y-4">
      {selectedTools.length > 0 && (
        <div className="flex justify-between items-center">
          <h3 className="text-sm font-medium text-white/70">Selected Tools</h3>
          <div className="flex gap-2">
            <Button
              onClick={() => setShowDiscoverTools(true)}
              disabled={isSubmitting}
              variant="outline"
              className="text-white/70 border bg-transparent hover:bg-text-white/90 hover:text-white/90 border-white/70 hover:border-white/90"
            >
              <Download className="h-4 w-4 mr-2" />
              Discover MCP Tools
            </Button>
            <Button
              onClick={() => {
                setDiscoveredToolsForSelection([]);
                setShowToolSelector(true);
              }}
              disabled={isSubmitting}
              variant="outline"
              className="text-white/70 border bg-transparent hover:bg-text-white/90 hover:text-white/90 border-white/70 hover:border-white/90"
            >
              <Plus className="h-4 w-4 mr-2" />
              Add Tools
            </Button>
          </div>
        </div>
      )}

      <ScrollArea>
        {selectedTools.length === 0 ? (
          <Card className="bg-[#1A1A1A] border-[#3A3A3A]">
            <CardContent className="p-8 flex flex-col items-center justify-center text-center">
              <FunctionSquare className="h-12 w-12 text-white/20 mb-4" />
              <h4 className="text-lg font-medium text-white mb-2">No tools selected</h4>
              <p className="text-white/50 text-sm mb-4">Add tools to enhance your agent</p>
              <div className="flex flex-col sm:flex-row gap-3">
                <Button onClick={() => setShowDiscoverTools(true)} disabled={isSubmitting} variant="secondary" className="flex items-center">
                  <Download className="h-4 w-4 mr-2" />
                  Discover MCP Tools
                </Button>
                <Button
                  onClick={() => {
                    setDiscoveredToolsForSelection([]);
                    setShowToolSelector(true);
                  }}
                  disabled={isSubmitting}
                  variant="secondary"
                  className="flex items-center"
                >
                  <Plus className="h-4 w-4 mr-2" />
                  Add Tools
                </Button>
              </div>
            </CardContent>
          </Card>
        ) : (
          renderSelectedTools()
        )}
      </ScrollArea>

      {renderConfigDialog()}
      <DiscoverToolsDialog open={showDiscoverTools} onOpenChange={setShowDiscoverTools} onShowSelectTools={handleShowSelectTools} />
      <SelectToolsDialog
        open={showToolSelector}
        onOpenChange={setShowToolSelector}
        availableTools={availableToolsForSelection}
        selectedTools={selectedTools}
        onToolsSelected={handleToolSelect}
        initialSelection={discoveredToolsForSelection}
      />
    </div>
  );
};
