import { useState, useEffect, useMemo } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Search, Filter, ChevronDown, ChevronRight, AlertCircle, PlusCircle, XCircle, FunctionSquare, LucideIcon } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Component, ToolConfig, AgentResponse, Tool } from "@/types/datamodel";
import ProviderFilter from "./ProviderFilter";
import Link from "next/link";
import { getToolCategory, getToolDisplayName, getToolDescription, getToolIdentifier, getToolProvider, isAgentTool, isMcpTool, isMcpProvider, componentToAgentTool } from "@/lib/toolUtils";
import KagentLogo from "../kagent-logo";
// Maximum number of tools that can be selected
const MAX_TOOLS_LIMIT = 20;

interface SelectedToolEntry {
  originalItemIdentifier: string;
  toolInstance: Tool;
}

interface SelectToolsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  availableTools: Component<ToolConfig>[];
  selectedTools: Tool[];
  onToolsSelected: (tools: Tool[]) => void;
  availableAgents: AgentResponse[];
  loadingAgents: boolean;
}

// Helper function to get display info for a tool or agent
const getItemDisplayInfo = (item: Component<ToolConfig> | AgentResponse | Tool): {
  displayName: string;
  description?: string;
  identifier: string;
  providerText?: string;
  Icon: React.ElementType | LucideIcon;
  iconColor: string;
  isAgent: boolean;
} => {
  let displayName: string;
  let description: string | undefined;
  let identifier: string;
  let providerText: string | undefined;
  let Icon: React.ElementType | LucideIcon = FunctionSquare;
  let iconColor = "text-yellow-500";
  let isAgent = false;

  if (!item || typeof item !== 'object') {
    // Handle null/undefined/non-object case
    displayName = "Unknown Item";
    identifier = `unknown-${Math.random().toString(36).substring(7)}`;
    return { displayName, description, identifier, providerText, Icon, iconColor, isAgent };
  }

  // Handle AgentResponse specifically (as it's not a Tool or Component)
  if ('agent' in item && item.agent && typeof item.agent === 'object' && 'metadata' in item.agent && item.agent.metadata) {
      const agentResp = item as AgentResponse;
      displayName = agentResp.agent.metadata.name;
      description = agentResp.agent.spec.description;
      // Use the same identifier format as AgentTool for consistency
      identifier = `agent-${displayName}`;
      providerText = "Agent";
      Icon = KagentLogo;
      iconColor = "text-green-500";
      isAgent = true;
  }
  // Handle Tool and Component<ToolConfig> types using toolUtils
  else {
      // Cast to the union type that toolUtils functions expect
      const toolOrComponent = item as Tool | Component<ToolConfig>;

      displayName = getToolDisplayName(toolOrComponent);
      description = getToolDescription(toolOrComponent);
      identifier = getToolIdentifier(toolOrComponent);
      providerText = getToolProvider(toolOrComponent);

      if (isAgentTool(toolOrComponent)) {
          Icon = KagentLogo;
          isAgent = true; 
      } else if (isMcpTool(toolOrComponent) || ('provider' in toolOrComponent && isMcpProvider(toolOrComponent.provider))) {
          // Check for MCP Tool or MCP Component
          iconColor = "text-blue-400";
          isAgent = false;
      } else {
          isAgent = false;
      }
  }

  return { displayName, description, identifier, providerText, Icon, iconColor, isAgent };
};

export const SelectToolsDialog: React.FC<SelectToolsDialogProps> = ({ open, onOpenChange, availableTools, selectedTools, onToolsSelected, availableAgents, loadingAgents }) => {
  const [searchTerm, setSearchTerm] = useState("");
  const [localSelectedComponents, setLocalSelectedComponents] = useState<SelectedToolEntry[]>([]);
  const [categories, setCategories] = useState<Set<string>>(new Set());
  const [selectedCategories, setSelectedCategories] = useState<Set<string>>(new Set());
  const [showFilters, setShowFilters] = useState(false);
  const [expandedCategories, setExpandedCategories] = useState<{ [key: string]: boolean }>({});

  // Initialize state when dialog opens
  useEffect(() => {
    if (open) {
      const initialSelectedEntries: SelectedToolEntry[] = selectedTools.map(tool => {
        const toolInfo = getItemDisplayInfo(tool);
        return {
          originalItemIdentifier: toolInfo.identifier,
          toolInstance: tool
        };
      });
      setLocalSelectedComponents(initialSelectedEntries);
      setSearchTerm("");

      const uniqueCategories = new Set<string>();
      const categoryCollapseState: { [key: string]: boolean } = {};
      availableTools.forEach((tool) => {
        const category = getToolCategory(tool);
        uniqueCategories.add(category);
        categoryCollapseState[category] = true;
      });

      if (availableAgents.length > 0) {
        uniqueCategories.add("Agents");
        categoryCollapseState["Agents"] = true;
      }

      setCategories(uniqueCategories);
      setSelectedCategories(new Set());
      setExpandedCategories(categoryCollapseState);
      setShowFilters(false);
    }
  }, [open, selectedTools, availableTools, availableAgents]);

  const actualSelectedCount = useMemo(() => {
    return localSelectedComponents.reduce((acc, entry) => {
      const tool = entry.toolInstance;
      if (tool.mcpServer && tool.mcpServer.toolNames && tool.mcpServer.toolNames.length > 0) {
        return acc + tool.mcpServer.toolNames.length;
      }
      return acc + 1;
    }, 0);
  }, [localSelectedComponents]);

  const isLimitReached = actualSelectedCount >= MAX_TOOLS_LIMIT;

  // Filter tools based on search and category selections
  const filteredAvailableItems = useMemo(() => {
    const searchLower = searchTerm.toLowerCase();
    const tools = availableTools.filter((tool) => {
      const toolName = getToolDisplayName(tool).toLowerCase();
      const toolDescription = getToolDescription(tool)?.toLowerCase() ?? "";
      const toolProvider = getToolProvider(tool)?.trim() || "";

      const matchesSearch = toolName.includes(searchLower) || toolDescription.includes(searchLower) || toolProvider.toLowerCase().includes(searchLower);

      const toolCategory = getToolCategory(tool);
      const matchesCategory = selectedCategories.size === 0 || selectedCategories.has(toolCategory);
      return matchesSearch && matchesCategory;
    });

    // Filter agents if "Agents" category is selected or no category is selected
    const agentCategorySelected = selectedCategories.size === 0 || selectedCategories.has("Agents");
    const agents = agentCategorySelected ? availableAgents.filter(agentResp => {
        const agentName = agentResp.agent.metadata.name.toLowerCase();
        const agentDesc = agentResp.agent.spec.description.toLowerCase();
        return agentName.includes(searchLower) || agentDesc.includes(searchLower);
      })
    : [];

    return { tools, agents };
  }, [availableTools, availableAgents, searchTerm, selectedCategories]);

  // Group available tools and agents by category
  const groupedAvailableItems = useMemo(() => {
    const groups: { [key: string]: (Component<ToolConfig> | AgentResponse)[] } = {};
    const sortedTools = [...filteredAvailableItems.tools].sort((a, b) => {
      return getToolDisplayName(a).localeCompare(getToolDisplayName(b));
    });
    sortedTools.forEach((tool) => {
      const category = getToolCategory(tool);
      if (!groups[category]) {
        groups[category] = [];
      }
      groups[category].push(tool);
    });

    // Add agents to the "Agents" category
    if (filteredAvailableItems.agents.length > 0) {
      groups["Agents"] = filteredAvailableItems.agents.sort((a, b) => 
        a.agent.metadata.name.localeCompare(b.agent.metadata.name)
      );
    }
    
    // Sort categories alphabetically
    return Object.entries(groups).sort(([catA], [catB]) => catA.localeCompare(catB))
           .reduce((acc, [key, value]) => { acc[key] = value; return acc; }, {} as typeof groups);
           
  }, [filteredAvailableItems]);

  const isItemSelected = (item: Component<ToolConfig> | AgentResponse): boolean => {
    const { identifier: availableItemIdentifier } = getItemDisplayInfo(item);
    return localSelectedComponents.some(entry => entry.originalItemIdentifier === availableItemIdentifier);
  };

  const handleAddItem = (item: Component<ToolConfig> | AgentResponse) => {
    const originalItemInfo = getItemDisplayInfo(item);
    const isSelectedByOriginalId = localSelectedComponents.some(entry => entry.originalItemIdentifier === originalItemInfo.identifier);

    if (isSelectedByOriginalId) return;

    let toolToAdd: Tool;
    let numEffectiveToolsInThisItem = 1;

    if ('agent' in item && item.agent && typeof item.agent === 'object' && 'metadata' in item.agent) {
        const agentResp = item as AgentResponse;
        toolToAdd = {
            type: "Agent",
            agent: {
                ref: agentResp.agent.metadata.name,
                description: agentResp.agent.spec.description
            }
        };
    } else {
        const component = item as Component<ToolConfig>;
        toolToAdd = componentToAgentTool(component);
        
        if (toolToAdd.mcpServer?.toolNames && toolToAdd.mcpServer.toolNames.length > 0) {
            numEffectiveToolsInThisItem = toolToAdd.mcpServer.toolNames.length;
        } else {
            numEffectiveToolsInThisItem = 1; 
        }
    }

    if (actualSelectedCount + numEffectiveToolsInThisItem <= MAX_TOOLS_LIMIT) {
        setLocalSelectedComponents((prev) => [
            ...prev,
            { originalItemIdentifier: originalItemInfo.identifier, toolInstance: toolToAdd }
        ]);
    } else {
        console.warn(`Cannot add tool. Limit reached or will be exceeded. Current: ${actualSelectedCount}, Adding: ${numEffectiveToolsInThisItem}, Limit: ${MAX_TOOLS_LIMIT}`);
    }
  };

  const handleRemoveToolById = (toolInstanceIdentifier: string) => {
    setLocalSelectedComponents((prev) => 
        prev.filter(entry => getItemDisplayInfo(entry.toolInstance).identifier !== toolInstanceIdentifier)
    );
  };

  const handleSave = () => {
    onToolsSelected(localSelectedComponents.map(entry => entry.toolInstance));
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
    if (!highlight || !text) return text;
    const parts = text.split(new RegExp(`(${highlight.replace(/[-\/\\^$*+?.()|[\]{}]/g, '\\$&')})`, 'gi'));
    return parts.map((part, i) =>
      part.toLowerCase() === highlight.toLowerCase() ? <mark key={i} className="bg-yellow-200 px-0 py-0 rounded">{part}</mark> : part
    );
  };

  return (
    <Dialog open={open} onOpenChange={handleCancel}>
      <DialogContent className="max-w-6xl max-h-[90vh] h-[85vh] flex flex-col p-0">
        <DialogHeader className="p-6 pb-4 border-b">
          <DialogTitle className="text-xl">Select Tools and Agents</DialogTitle>
          <DialogDescription className="text-sm text-muted-foreground">
            You can use tools and agents to create your agent. The tools are grouped by category. You can select a tool by clicking on it. To add your own tools, you can use the <Link href="/tools" className="text-violet-600 hover:text-violet-700">Tools</Link> page.
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
              {loadingAgents && (
                <div className="flex items-center justify-center h-full">
                  <p>Loading Agents...</p>
                </div>
              )}
              {!loadingAgents && Object.keys(groupedAvailableItems).length > 0 ? (
                <div className="space-y-3">
                  {Object.entries(groupedAvailableItems).map(([category, items]) => {
                    const itemsSelectedInCategory = items.reduce((count, availableItemInLoop) => {
                        const { identifier: availableItemInCatIdentifier } = getItemDisplayInfo(availableItemInLoop);
                        const selectedEntry = localSelectedComponents.find(
                            (entry) => entry.originalItemIdentifier === availableItemInCatIdentifier
                        );

                        if (selectedEntry) {
                            const tool = selectedEntry.toolInstance;
                            if (tool.mcpServer && 
                                tool.mcpServer.toolNames && 
                                tool.mcpServer.toolNames.length > 0) {
                                return count + tool.mcpServer.toolNames.length;
                            }
                            return count + 1;
                        }
                        return count;
                    }, 0);

                    return (
                      <div key={category} className="border rounded-lg overflow-hidden bg-card">
                        <div
                          className="flex items-center justify-between p-3 bg-secondary/50 cursor-pointer hover:bg-secondary/70"
                          onClick={() => toggleCategory(category)}
                        >
                          <div className="flex items-center gap-2">
                            {expandedCategories[category] ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
                            <h3 className="font-semibold capitalize text-sm">{highlightMatch(category, searchTerm)}</h3>
                            <Badge variant="secondary" className="font-mono text-xs">{items.length}</Badge>
                          </div>
                          <div className="flex items-center gap-2 text-xs text-muted-foreground">
                            {itemsSelectedInCategory > 0 && (
                               <Badge variant="outline">{itemsSelectedInCategory} selected</Badge>
                            )}
                          </div>
                        </div>

                        {expandedCategories[category] && (
                          <div className="divide-y border-t">
                            {items.map((item) => {
                              const { displayName, description, identifier, providerText } = getItemDisplayInfo(item);
                              const isSelected = isItemSelected(item);
                              const isDisabled = !isSelected && isLimitReached;

                              return (
                                <div
                                  key={identifier}
                                  className={`flex items-center justify-between p-3 pr-2 group min-w-0 ${isDisabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:bg-muted/50'}`}
                                  onClick={() => !isDisabled && handleAddItem(item)}
                                >
                                  <div className="flex-1 overflow-hidden pr-2">
                                    <p className="font-medium text-sm truncate overflow-hidden">{highlightMatch(displayName, searchTerm)}</p>
                                    {description && <p className="text-xs text-muted-foreground">{highlightMatch(description, searchTerm)}</p>}
                                    {providerText && <p className="text-xs text-muted-foreground/80 font-mono mt-1">{highlightMatch(providerText, searchTerm)}</p>}
                                  </div>
                                  {!isSelected && !isDisabled && (
                                     <Button variant="ghost" size="icon" className="h-7 w-7 opacity-0 group-hover:opacity-100 text-green-600 hover:text-green-700" >
                                       <PlusCircle className="h-4 w-4"/>
                                     </Button>
                                   )}
                                  {isSelected && (
                                    <Button variant="ghost" size="icon" className="h-7 w-7 text-destructive hover:text-destructive/80" onClick={(e) => {
                                      e.stopPropagation(); 
                                      // To remove here, we need the toolInstance's identifier
                                      // Find the entry, then get its toolInstance's ID
                                      const entryToRemove = localSelectedComponents.find(entry => entry.originalItemIdentifier === identifier);
                                      if (entryToRemove) {
                                        handleRemoveToolById(getItemDisplayInfo(entryToRemove.toolInstance).identifier);
                                      }
                                    }}>
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
            <div className="flex items-center justify-between">
              <h3 className="text-lg font-semibold">Selected ({actualSelectedCount}/{MAX_TOOLS_LIMIT})</h3>
              <Button variant="ghost" size="sm" onClick={clearAllSelectedTools} disabled={actualSelectedCount === 0}>
                Clear All
              </Button>
            </div>

            {isLimitReached && actualSelectedCount >= MAX_TOOLS_LIMIT && (
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
                  {localSelectedComponents.flatMap((entry) => {
                    const tool = entry.toolInstance;
                    if (tool.mcpServer && tool.mcpServer.toolNames && tool.mcpServer.toolNames.length > 0) {
                      const parentToolInfo = getItemDisplayInfo(tool);
                      return tool.mcpServer.toolNames.map((toolName) => (
                        <div key={`${parentToolInfo.identifier}-${toolName}`} className="flex items-center justify-between p-3 border rounded-md bg-muted/30 min-w-0">
                          <div className="flex items-center gap-2 flex-1 overflow-hidden">
                            <parentToolInfo.Icon className={`h-4 w-4 flex-shrink-0 ${parentToolInfo.iconColor}`} />
                            <div className="flex-1 overflow-hidden">
                              <p className="text-sm font-medium truncate">{toolName}</p>
                              {parentToolInfo.description && (
                                <p className="text-xs text-muted-foreground truncate">{parentToolInfo.description}</p>
                              )}
                            </div>
                          </div>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6 ml-2 flex-shrink-0"
                            onClick={() => {
                              handleRemoveToolById(parentToolInfo.identifier);
                            }}
                          >
                            <XCircle className="h-4 w-4" />
                          </Button>
                        </div>
                      ));
                    } else {
                      const { displayName, description, identifier: toolInstanceIdentifier, Icon, iconColor, isAgent } = getItemDisplayInfo(tool);
                      return [( 
                        <div key={toolInstanceIdentifier} className="flex items-center justify-between p-3 border rounded-md bg-muted/30 min-w-0">
                          <div className="flex items-center gap-2 flex-1 overflow-hidden">
                            {isAgent ? (
                              <KagentLogo className={`h-4 w-4 flex-shrink-0 ${iconColor}`} />
                            ) : (
                              <Icon className={`h-4 w-4 flex-shrink-0 ${iconColor}`} />
                            )}
                            <div className="flex-1 overflow-hidden">
                              <p className="text-sm font-medium truncate">{displayName}</p>
                              {description && <p className="text-xs text-muted-foreground truncate">{description}</p>}
                            </div>
                          </div>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6 ml-2 flex-shrink-0"
                            onClick={() => {
                              handleRemoveToolById(toolInstanceIdentifier);
                            }}
                          >
                            <XCircle className="h-4 w-4" />
                          </Button>
                        </div>
                      )];
                    }
                  })}
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-full text-center text-muted-foreground">
                  <PlusCircle className="h-10 w-10 mb-3 opacity-50" />
                  <p className="font-medium">No tools selected</p>
                  <p className="text-sm">Select tools or agents from the left panel.</p>
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
                Save Selection ({actualSelectedCount})
              </Button>
            </div>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
