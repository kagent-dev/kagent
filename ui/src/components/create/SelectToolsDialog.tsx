import { useState, useEffect, useMemo } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Search, Check, Download, X, Filter, Tag } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { getToolDescription, getToolDisplayName, getToolIdentifier, isMcpTool } from "@/lib/data";
import { Component, ToolConfig } from "@/types/datamodel";

interface SelectToolsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  availableTools: Component<ToolConfig>[];
  selectedTools: Component<ToolConfig>[];
  onToolsSelected: (selectedTools: Component<ToolConfig>[]) => void;
  initialSelection?: Component<ToolConfig>[];
}

export const SelectToolsDialog = ({ open, onOpenChange, availableTools, selectedTools, onToolsSelected, initialSelection = [] }: SelectToolsDialogProps) => {
  const [searchTerm, setSearchTerm] = useState("");
  const [activeTab, setActiveTab] = useState("all");
  const [localSelectedTools, setLocalSelectedTools] = useState<Component<ToolConfig>[]>([]);
  const [newlyDiscoveredTools, setNewlyDiscoveredTools] = useState<Component<ToolConfig>[]>([]);
  const [providers, setProviders] = useState<Set<string>>(new Set());
  const [selectedProviders, setSelectedProviders] = useState<Set<string>>(new Set());
  const [showFilters, setShowFilters] = useState(false);

  // Initialize local selection and extract unique providers when dialog opens
  useEffect(() => {
    if (open) {
      // Start with currently selected tools
      const initialTools = [...selectedTools];

      // Add any newly discovered tools that were passed in
      if (initialSelection.length > 0) {
        setNewlyDiscoveredTools(initialSelection);

        // Pre-select all newly discovered tools
        initialSelection.forEach((newTool) => {
          const newToolId = getToolIdentifier(newTool);
          if (!initialTools.some((t) => getToolIdentifier(t) === newToolId)) {
            initialTools.push(newTool);
          }
        });
      } else {
        setNewlyDiscoveredTools([]);
      }

      setLocalSelectedTools(initialTools);
      setSearchTerm("");

      // Extract unique providers
      const uniqueProviders = new Set<string>();
      availableTools.forEach((tool) => {
        if (tool.provider) {
          uniqueProviders.add(tool.provider);
        }
      });
      setProviders(uniqueProviders);
      setSelectedProviders(new Set());

      // Set initial tab based on if there are newly discovered tools
      setActiveTab(initialSelection.length > 0 ? "new" : "all");
    }
  }, [open, selectedTools, initialSelection, availableTools]);

  // Filter tools based on search, tab, and provider filters
  const filteredTools = useMemo(() => {
    return availableTools.filter((tool) => {
      const displayName = getToolDisplayName(tool) || "";
      const displayDescription = getToolDescription(tool) || "";
      const searchLower = searchTerm.toLowerCase();
      const matchesSearch = displayName.toLowerCase().includes(searchLower) || displayDescription.toLowerCase().includes(searchLower) || tool.provider.toLowerCase().includes(searchLower);

      const matchesTab =
        activeTab === "all" ||
        (activeTab === "selected" && localSelectedTools.some((t) => getToolIdentifier(t) === getToolIdentifier(tool))) ||
        (activeTab === "new" && newlyDiscoveredTools.some((t) => getToolIdentifier(t) === getToolIdentifier(tool))) ||
        (activeTab === "mcp" && isMcpTool(tool));

      const matchesProvider = selectedProviders.size === 0 || selectedProviders.has(tool.provider);

      return matchesSearch && matchesTab && matchesProvider;
    });
  }, [availableTools, searchTerm, activeTab, localSelectedTools, newlyDiscoveredTools, selectedProviders]);

  // Sort tools to show newly discovered tools first, then alphabetically
  const sortedTools = useMemo(() => {
    return [...filteredTools].sort((a, b) => {
      const aIsNew = newlyDiscoveredTools.some((t) => getToolIdentifier(t) === getToolIdentifier(a));
      const bIsNew = newlyDiscoveredTools.some((t) => getToolIdentifier(t) === getToolIdentifier(b));

      if (aIsNew && !bIsNew) return -1;
      if (!aIsNew && bIsNew) return 1;

      // Secondary sort by name
      const aName = getToolDisplayName(a) || "";
      const bName = getToolDisplayName(b) || "";
      return aName.localeCompare(bName);
    });
  }, [filteredTools, newlyDiscoveredTools]);

  const handleToggleTool = (tool: Component<ToolConfig>) => {
    const toolId = getToolIdentifier(tool);
    const isSelected = localSelectedTools.some((t) => getToolIdentifier(t) === toolId);

    if (isSelected) {
      setLocalSelectedTools(localSelectedTools.filter((t) => getToolIdentifier(t) !== toolId));
    } else {
      setLocalSelectedTools([...localSelectedTools, tool]);
    }
  };

  const handleSave = () => {
    onToolsSelected(localSelectedTools);
    onOpenChange(false);
  };

  const handleCancel = () => {
    onOpenChange(false);
  };

  const handleToggleProvider = (provider: string) => {
    const newSelection = new Set(selectedProviders);
    if (newSelection.has(provider)) {
      newSelection.delete(provider);
    } else {
      newSelection.add(provider);
    }
    setSelectedProviders(newSelection);
  };

  const handleSelectAll = () => {
    setLocalSelectedTools([...availableTools]);
  };

  const handleSelectNone = () => {
    setLocalSelectedTools([]);
  };

  const renderToolItem = (tool: Component<ToolConfig>) => {
    const displayName = getToolDisplayName(tool) || "Unnamed Tool";
    const displayDescription = getToolDescription(tool) || "No description available";
    const toolId = getToolIdentifier(tool);
    const isSelected = localSelectedTools.some((t) => getToolIdentifier(t) === toolId);
    const isNewlyDiscovered = newlyDiscoveredTools.some((t) => getToolIdentifier(t) === toolId);

    return (
      <div
        key={toolId}
        className={`p-4 rounded-lg cursor-pointer border transition-all ${
          isSelected ? "bg-primary/10 border-primary/30" : isNewlyDiscovered ? "bg-blue-500/5 border-blue-500/20 hover:bg-blue-500/10" : "border-border hover:bg-secondary/50"
        }`}
      >
        <div className="flex items-start justify-between gap-4">
          <div className="flex-1">
            <div className="font-medium flex items-center flex-wrap gap-2">
              <span className="mr-1">{displayName}</span>
              <div className="flex flex-wrap gap-1">
                {isMcpTool(tool) && (
                  <Badge variant="outline" className="bg-blue-500/10 text-blue-500 border-blue-500/20 hover:bg-blue-500/20">
                    MCP
                  </Badge>
                )}
                {isNewlyDiscovered && (
                  <Badge variant="outline" className="bg-green-400/10 text-green-400 border-green-400/20 hover:bg-green-400/20">
                    <Download className="h-3 w-3 mr-1" />
                    New
                  </Badge>
                )}
                <Badge variant="outline" className="bg-gray-100 text-gray-500 border-gray-200">
                  {tool.provider}
                </Badge>
              </div>
            </div>
            <div className="text-muted-foreground mt-2">{displayDescription}</div>
            <div className="text-xs text-muted-foreground mt-1 opacity-70">{toolId}</div>
          </div>
          <Button
            variant={isSelected ? "secondary" : "outline"}
            size="sm"
            className={`mt-0 ${isSelected ? "bg-primary text-primary-foreground hover:bg-primary/90" : ""}`}
            onClick={(e) => {
              e.stopPropagation();
              handleToggleTool(tool);
            }}
          >
            {isSelected ? (
              <>
                <Check className="w-4 h-4 mr-1" /> Selected
              </>
            ) : (
              "Select"
            )}
          </Button>
        </div>
      </div>
    );
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(isOpen) => {
        // Only allow closing if we're not coming from discovery
        if (!isOpen && newlyDiscoveredTools.length > 0) {
          // If trying to close after discovery without saving, keep tools pre-selected
          onToolsSelected(localSelectedTools);
        }
        onOpenChange(isOpen);
      }}
    >
      <DialogContent className="max-w-4xl max-h-[85vh] h-auto">
        <DialogHeader>
          <DialogTitle className="text-xl">{newlyDiscoveredTools.length > 0 ? "Select Discovered Tools" : "Select Tools"}</DialogTitle>
        </DialogHeader>

        {/* Search and filter area */}
        <div className="space-y-4">
          <div className="flex gap-2">
            <div className="relative flex-1">
              <Search className="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
              <Input placeholder="Search tools by name, description or provider..." value={searchTerm} onChange={(e) => setSearchTerm(e.target.value)} className="pl-10" />
            </div>
            <Button variant="outline" size="icon" onClick={() => setShowFilters(!showFilters)} className={showFilters ? "bg-secondary" : ""}>
              <Filter className="h-4 w-4" />
            </Button>
          </div>

          {showFilters && (
            <div className="bg-secondary/50 p-4 rounded-lg border">
              <div className="mb-2 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Tag className="h-4 w-4" />
                  <span className="font-medium">Filter by Provider</span>
                </div>
                <div className="flex gap-2">
                  <Button variant="ghost" size="sm" onClick={() => setSelectedProviders(new Set(providers))}>
                    All
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => setSelectedProviders(new Set())}>
                    None
                  </Button>
                </div>
              </div>
              <div className="flex flex-wrap gap-2 mt-2">
                {Array.from(providers).map((provider) => (
                  <Badge key={provider} variant={selectedProviders.has(provider) ? "default" : "outline"} className="cursor-pointer" onClick={() => handleToggleProvider(provider)}>
                    {provider}
                    {selectedProviders.has(provider) && <X className="ml-1 h-3 w-3" />}
                  </Badge>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Notification for newly discovered tools */}
        {newlyDiscoveredTools.length > 0 && (
          <div className="bg-blue-500/10 p-4 rounded-lg border border-blue-500/20">
            <h3 className="text-sm font-medium flex items-center mb-2">
              <Download className="h-4 w-4 mr-2 text-blue-400" />
              Newly Discovered Tools
            </h3>
            <p className="text-sm">
              {newlyDiscoveredTools.length} MCP tool{newlyDiscoveredTools.length !== 1 ? "s" : ""} discovered. These tools are pre-selected and sorted to the top of the list.
            </p>
          </div>
        )}

        {/* Tabs and selection actions */}
        <div className="flex items-center justify-between">
          <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
            <TabsList>
              <TabsTrigger value="all">
                All Tools
                <Badge variant="outline" className="ml-1 bg-background">
                  {availableTools.length}
                </Badge>
              </TabsTrigger>
              <TabsTrigger value="selected">
                Selected
                <Badge variant="outline" className="ml-1 bg-background">
                  {localSelectedTools.length}
                </Badge>
              </TabsTrigger>
              {newlyDiscoveredTools.length > 0 && (
                <TabsTrigger value="new">
                  New
                  <Badge variant="outline" className="ml-1 bg-background">
                    {newlyDiscoveredTools.length}
                  </Badge>
                </TabsTrigger>
              )}
              <TabsTrigger value="mcp">
                MCP Tools
                <Badge variant="outline" className="ml-1 bg-background">
                  {availableTools.filter((tool) => isMcpTool(tool)).length}
                </Badge>
              </TabsTrigger>
            </TabsList>
          </Tabs>

          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={handleSelectAll}>
              Select All
            </Button>
            <Button variant="ghost" size="sm" onClick={handleSelectNone}>
              Clear
            </Button>
          </div>
        </div>

        {/* Tools list */}
        <ScrollArea className="h-[400px] pr-4 -mr-4 overflow-y-auto">
          {sortedTools.length > 0 ? (
            <div className="space-y-3">{sortedTools.map(renderToolItem)}</div>
          ) : (
            <div className="flex flex-col items-center justify-center h-[200px] text-center p-4">
              <Search className="h-12 w-12 text-muted-foreground mb-4 opacity-20" />
              <h3 className="font-medium text-lg">No tools found</h3>
              <p className="text-muted-foreground mt-1">Try adjusting your search or filters to find what you&apos;re looking for.</p>
            </div>
          )}
        </ScrollArea>

        {/* Footer with actions */}
        <DialogFooter className="mt-4 pt-4 border-t">
          <div className="flex justify-between w-full items-center">
            <div className="text-sm flex items-center">
              <Badge variant="outline" className="mr-2">
                {localSelectedTools.length} tool{localSelectedTools.length !== 1 ? "s" : ""} selected
              </Badge>
              {newlyDiscoveredTools.length > 0 && (
                <span className="text-green-400 flex items-center">
                  <Download className="h-3 w-3 mr-1" />
                  {newlyDiscoveredTools.filter((newTool) => localSelectedTools.some((selectedTool) => getToolIdentifier(selectedTool) === getToolIdentifier(newTool))).length} new
                </span>
              )}
            </div>
            <div className="flex gap-2">
              {newlyDiscoveredTools.length === 0 && (
                <Button variant="outline" onClick={handleCancel}>
                  Cancel
                </Button>
              )}
              <Button className="bg-violet-500 hover:bg-violet-600 text-white" onClick={handleSave}>
                {newlyDiscoveredTools.length > 0 ? "Add Selected Tools" : "Save Selection"}
              </Button>
            </div>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
