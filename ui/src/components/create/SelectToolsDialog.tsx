import { useState, useEffect, useMemo } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Search, Filter, ChevronDown, ChevronRight, AlertCircle, PlusCircle, XCircle } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Component, ToolConfig } from "@/types/datamodel";
import ProviderFilter from "./ProviderFilter";
import Link from "next/link";
import { getToolDisplayName, getToolDescription, getToolIdentifier, getToolProvider } from "@/lib/toolUtils";

// Maximum number of tools that can be selected
const MAX_TOOLS_LIMIT = 10;

// Extract category from tool identifier
const getToolCategory = (tool: Component<ToolConfig>) => {
  if (tool.provider === "autogen_ext.tools.mcp.SseMcpToolAdapter") {
    return tool.label || "MCP Server";
  }

  const toolId = getToolIdentifier(tool);
  const parts = toolId.split(".");
  if (parts.length >= 3 && parts[1] === "tools") {
    return parts[2]; // e.g., kagent.tools.grafana -> grafana
  }
  if (parts.length >= 2) {
    return parts[1]; // e.g., kagent.builtin -> builtin
  }
  return "other"; // Default category
};

interface SelectToolsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  availableTools: Component<ToolConfig>[];
  selectedTools: Component<ToolConfig>[];
  onToolsSelected: (tools: Component<ToolConfig>[]) => void;
}

export const SelectToolsDialog: React.FC<SelectToolsDialogProps> = ({ open, onOpenChange, availableTools, selectedTools, onToolsSelected }) => {
  const [searchTerm, setSearchTerm] = useState("");
  const [localSelectedComponents, setLocalSelectedComponents] = useState<Component<ToolConfig>[]>([]);
  const [categories, setCategories] = useState<Set<string>>(new Set());
  const [selectedCategories, setSelectedCategories] = useState<Set<string>>(new Set());
  const [showFilters, setShowFilters] = useState(false);
  const [expandedCategories, setExpandedCategories] = useState<{ [key: string]: boolean }>({});

  // Initialize state when dialog opens
  useEffect(() => {
    if (open) {
      setLocalSelectedComponents(selectedTools);
      setSearchTerm("");

      const uniqueCategories = new Set<string>();
      const categoryCollapseState: { [key: string]: boolean } = {};
      availableTools.forEach((tool) => {
        const category = getToolCategory(tool);
        uniqueCategories.add(category);
        categoryCollapseState[category] = true;
      });

      setCategories(uniqueCategories);
      setSelectedCategories(new Set());
      setExpandedCategories(categoryCollapseState);
      setShowFilters(false);
    }
  }, [open, selectedTools, availableTools]);

  // Filter tools based on search and category selections
  const filteredAvailableTools = useMemo(() => {
    const searchLower = searchTerm.toLowerCase();
    return availableTools.filter((tool) => {
      const toolName = getToolDisplayName(tool).toLowerCase();
      const toolDescription = getToolDescription(tool)?.toLowerCase() ?? "";
      const toolProvider = getToolProvider(tool)?.trim();

      const matchesSearch = toolName.includes(searchLower) || toolDescription.includes(searchLower) || (toolProvider && toolProvider.toLowerCase().includes(searchLower));

      const toolCategory = getToolCategory(tool);
      const matchesCategory = selectedCategories.size === 0 || selectedCategories.has(toolCategory);
      return matchesSearch && matchesCategory;
    });
  }, [availableTools, searchTerm, selectedCategories]);

  // Group available tools by category
  const groupedAvailableTools = useMemo(() => {
    const groups: { [key: string]: Component<ToolConfig>[] } = {};
    const sortedTools = [...filteredAvailableTools].sort((a, b) => {
      return getToolDisplayName(a).localeCompare(getToolDisplayName(b));
    });
    sortedTools.forEach((tool) => {
      const category = getToolCategory(tool);
      if (!groups[category]) {
        groups[category] = [];
      }
      groups[category].push(tool);
    });
    // Sort categories alphabetically
    return Object.entries(groups).sort(([catA], [catB]) => catA.localeCompare(catB))
           .reduce((acc, [key, value]) => { acc[key] = value; return acc; }, {} as typeof groups);
  }, [filteredAvailableTools]);

  const selectedCount = localSelectedComponents.length;
  const isLimitReached = selectedCount >= MAX_TOOLS_LIMIT;

  const isToolSelected = (tool: Component<ToolConfig>) => localSelectedComponents.some((t) => getToolIdentifier(t) === getToolIdentifier(tool));

  // Handlers
  const handleAddTool = (tool: Component<ToolConfig>) => {
    if (!isLimitReached && !isToolSelected(tool)) {
      setLocalSelectedComponents((prev) => [...prev, tool]);
    }
  };

  const handleRemoveTool = (tool: Component<ToolConfig>) => {
    setLocalSelectedComponents((prev) => prev.filter((t) => getToolIdentifier(t) !== getToolIdentifier(tool)));
  };

  const handleSave = () => {
    onToolsSelected(localSelectedComponents);
    onOpenChange(false);
  };

  const handleCancel = () => {
    onOpenChange(false);
  };

  const handleToggleCategoryFilter = (category: string) => {
    const trimmedCategory = category.trim();
    if (!trimmedCategory) return;

    setSelectedCategories((prev) => {
      const newSelection = new Set(prev);
      if (newSelection.has(trimmedCategory)) {
        newSelection.delete(trimmedCategory);
      } else {
        newSelection.add(trimmedCategory);
      }
      return newSelection;
    });
  };

  const toggleCategory = (category: string) => {
    setExpandedCategories((prev) => ({ ...prev, [category]: !prev[category] }));
  };

  const selectAllCategories = () => setSelectedCategories(new Set(categories));
  const clearCategories = () => setSelectedCategories(new Set());

  const clearAllSelectedTools = () => setLocalSelectedComponents([]);

  // Helper to highlight search term
  const highlightMatch = (text: string, highlight: string) => {
    if (!highlight) return text;
    const parts = text.split(new RegExp(`(${highlight})`, 'gi'));
    return parts.map((part, i) =>
      part.toLowerCase() === highlight.toLowerCase() ? <mark key={i} className="bg-yellow-200 px-0 py-0 rounded">{part}</mark> : part
    );
  };

  return (
    <Dialog open={open} onOpenChange={handleCancel}>
      <DialogContent className="max-w-6xl max-h-[90vh] h-[85vh] flex flex-col p-0">
        <DialogHeader className="p-6 pb-4 border-b">
          <DialogTitle className="text-xl">Select Tools</DialogTitle>
          <DialogDescription className="text-sm text-muted-foreground">
            The tools are grouped by category. You can select a tool by clicking on it. To add your own tools, you can use the <Link href="/tools" className="text-violet-600 hover:text-violet-700">Tools</Link> page.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-1 overflow-hidden">
          {/* Left Panel: Available Tools */}
          <div className="w-1/2 border-r flex flex-col p-4 space-y-4">
            {/* Search and Filter Area */}
            <div className="flex items-center gap-2">
              <div className="relative flex-1">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                <Input placeholder="Search tools..." value={searchTerm} onChange={(e) => setSearchTerm(e.target.value)} className="pl-10 pr-4 py-2 h-10" />
              </div>
              {categories.size > 1 && (
                 <Button variant="outline" size="icon" onClick={() => setShowFilters(!showFilters)} className={showFilters ? "bg-secondary" : ""}>
                   <Filter className="h-4 w-4" />
                 </Button>
               )}
            </div>

            {showFilters && categories.size > 1 && (
              <ProviderFilter
                providers={categories}
                selectedProviders={selectedCategories}
                onToggleProvider={handleToggleCategoryFilter}
                onSelectAll={selectAllCategories}
                onSelectNone={clearCategories}
              />
            )}

            {/* Available Tools List */}
            <ScrollArea className="flex-1 -mr-4 pr-4">
              {Object.keys(groupedAvailableTools).length > 0 ? (
                <div className="space-y-3">
                  {Object.entries(groupedAvailableTools).map(([category, tools]) => {
                    const toolsSelectedInCategory = tools.filter(isToolSelected).length;
                    return (
                      <div key={category} className="border rounded-lg overflow-hidden bg-card">
                        <div
                          className="flex items-center justify-between p-3 bg-secondary/50 cursor-pointer hover:bg-secondary/70"
                          onClick={() => toggleCategory(category)}
                        >
                          <div className="flex items-center gap-2">
                            {expandedCategories[category] ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
                            <h3 className="font-semibold capitalize text-sm">{highlightMatch(category, searchTerm)}</h3>
                            <Badge variant="secondary" className="font-mono text-xs">{tools.length}</Badge>
                          </div>
                          <div className="flex items-center gap-2 text-xs text-muted-foreground">
                            {toolsSelectedInCategory > 0 && (
                               <Badge variant="outline">{toolsSelectedInCategory} selected</Badge>
                            )}
                          </div>
                        </div>

                        {expandedCategories[category] && (
                          <div className="divide-y border-t">
                            {tools.map((tool) => {
                              const isSelected = isToolSelected(tool);
                              const isDisabled = !isSelected && isLimitReached;
                              return (
                                <div
                                  key={getToolIdentifier(tool)}
                                  className={`flex items-center justify-between p-3 pr-2 group min-w-0 ${isDisabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:bg-muted/50'}`}
                                  onClick={() => !isDisabled && handleAddTool(tool)}
                                >
                                  <div className="flex-1 overflow-hidden pr-2">
                                    <p className="font-medium text-sm truncate overflow-hidden">{highlightMatch(getToolDisplayName(tool), searchTerm)}</p>
                                    {getToolDescription(tool) && <p className="text-xs text-muted-foreground">{highlightMatch(getToolDescription(tool) || "", searchTerm)}</p>}
                                    <p className="text-xs text-muted-foreground/80 font-mono mt-1">{highlightMatch(getToolProvider(tool), searchTerm)}</p>
                                  </div>
                                  {!isSelected && !isDisabled && (
                                     <Button variant="ghost" size="icon" className="h-7 w-7 opacity-0 group-hover:opacity-100 text-green-600 hover:text-green-700" >
                                       <PlusCircle className="h-4 w-4"/>
                                     </Button>
                                   )}
                                  {isSelected && (
                                    <Button variant="ghost" size="icon" className="h-7 w-7 text-destructive hover:text-destructive/80" onClick={(e) => {e.stopPropagation(); handleRemoveTool(tool);}}>
                                       <XCircle className="h-4 w-4"/>
                                     </Button>
                                  )}
                                </div>
                              );
                            })}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-[200px] text-center p-4 text-muted-foreground">
                  <Search className="h-10 w-10 mb-3 opacity-50" />
                  <p className="font-medium">No tools found</p>
                  <p className="text-sm">Try adjusting your search or filters.</p>
                </div>
              )}
            </ScrollArea>
          </div>

          {/* Right Panel: Selected Tools */}
          <div className="w-1/2 flex flex-col p-4 space-y-4">
            <div className="flex justify-between items-center">
              <h3 className="font-semibold text-base">Selected Tools ({selectedCount}/{MAX_TOOLS_LIMIT})</h3>
              <Button variant="ghost" size="sm" onClick={clearAllSelectedTools} disabled={selectedCount === 0}>
                Clear All
              </Button>
            </div>

            {isLimitReached && (
              <div className="bg-amber-50 border border-amber-200 rounded-md p-3 flex items-start gap-2 text-amber-800 text-sm">
                <AlertCircle className="h-5 w-5 text-amber-500 mt-0.5 flex-shrink-0" />
                <div>
                  Tool limit reached. Deselect a tool to add another.
                </div>
              </div>
            )}

            <ScrollArea className="flex-1 -mr-4 pr-4">
              {localSelectedComponents.length > 0 ? (
                <div className="space-y-2">
                  {localSelectedComponents.map((tool) => (
                     <div key={getToolIdentifier(tool)} className="flex items-center justify-between p-3 border rounded-md bg-card group min-w-0">
                       <div className="flex-1 overflow-hidden pr-2">
                          <p className="font-medium text-sm truncate overflow-hidden">{getToolDisplayName(tool)}</p>
                          {getToolDescription(tool) && <p className="text-xs text-muted-foreground">{getToolDescription(tool)}</p>}
                          <p className="text-xs text-muted-foreground/80 font-mono mt-1">{getToolProvider(tool)}</p>
                       </div>
                       <Button variant="ghost" size="icon" className="flex-shrink-0 h-7 w-7 text-destructive hover:text-destructive/80 opacity-50 group-hover:opacity-100" onClick={() => handleRemoveTool(tool)}>
                         <XCircle className="h-4 w-4"/>
                       </Button>
                     </div>
                  ))}
                </div>
              ) : (
                 <div className="flex flex-col items-center justify-center h-full text-center p-4 text-muted-foreground">
                   <p className="text-sm">No tools selected yet.</p>
                   <p className="text-xs mt-1">Click on a tool from the left panel to add it.</p>
                 </div>
              )}
            </ScrollArea>
          </div>
        </div>

        {/* Footer with actions */}
        <DialogFooter className="p-4 border-t mt-auto">
          <div className="flex justify-between w-full items-center">
            <div className="text-sm text-muted-foreground">
              Select up to {MAX_TOOLS_LIMIT} tools for your agent.
            </div>
            <div className="flex gap-2">
              <Button variant="outline" onClick={handleCancel}>Cancel</Button>
              <Button className="bg-violet-600 hover:bg-violet-700 text-white" onClick={handleSave}>
                Save Selection ({selectedCount})
              </Button>
            </div>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
