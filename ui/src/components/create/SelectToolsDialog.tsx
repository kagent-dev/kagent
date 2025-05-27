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
        
        // For MCP tools, check if we already have a tool from the same server
        if (isMcpTool(toolToAdd) && toolToAdd.mcpServer) {
            const existingToolIndex = localSelectedComponents.findIndex(entry => 
                isMcpTool(entry.toolInstance) && 
                entry.toolInstance.mcpServer?.toolServer === toolToAdd.mcpServer?.toolServer
            );

            if (existingToolIndex !== -1) {
                // Add the new tool name to the existing MCP server entry
                const existingTool = localSelectedComponents[existingToolIndex].toolInstance;
                if (isMcpTool(existingTool) && existingTool.mcpServer) {
                    const updatedTool = {
                        ...existingTool,
                        mcpServer: {
                            ...existingTool.mcpServer,
                            toolNames: [...new Set([...existingTool.mcpServer.toolNames, ...toolToAdd.mcpServer.toolNames])]
                        }
                    };
                    setLocalSelectedComponents(prev => {
                        const newComponents = [...prev];
                        newComponents[existingToolIndex] = {
                            ...newComponents[existingToolIndex],
                            toolInstance: updatedTool
                        };
                        return newComponents;
                    });
                    return;
                }
            }
        }
        
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
    setSelectedCategories(prev => {
      const newSet = new Set(prev);
      if (newSet.has(category)) {
        newSet.delete(category);
      } else {
        newSet.add(category);
      }
      return newSet;
    });
  };

  const toggleCategory = (category: string) => {
    setExpandedCategories(prev => ({ ...prev, [category]: !prev[category] }));
  };

  const selectAllCategories = () => setSelectedCategories(new Set(categories));
  const clearCategories = () => setSelectedCategories(new Set());
  const clearAllSelectedTools = () => setLocalSelectedComponents([]);

  const highlightMatch = (text: string, highlight: string) => {
    if (!highlight) return text;
    const parts = text.split(new RegExp(`(${highlight})`, 'gi'));
    return parts.map((part, i) => 
      part.toLowerCase() === highlight.toLowerCase() ? 
        <span key={i} className="bg-yellow-200">{part}</span> : 
        part
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl h-[80vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>Select Tools</DialogTitle>
          <DialogDescription>
            Choose the tools and agents that your agent will have access to.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-1 gap-4 overflow-hidden">
          {/* Left panel - Available tools */}
          <div className="w-1/2 flex flex-col">
            <div className="flex items-center gap-2 mb-4">
              <div className="relative flex-1">
                <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="Search tools..."
                  value={searchTerm}
                  onChange={(e) => setSearchTerm(e.target.value)}
                  className="pl-8"
                />
              </div>
              <Button
                variant="outline"
                size="icon"
                onClick={() => setShowFilters(!showFilters)}
                className={showFilters ? "bg-muted" : ""}
              >
                <Filter className="h-4 w-4" />
              </Button>
            </div>

            {showFilters && (
              <div className="mb-4 p-4 border rounded-lg">
                <div className="flex items-center justify-between mb-2">
                  <h3 className="text-sm font-medium">Categories</h3>
                  <div className="flex gap-2">
                    <Button variant="outline" size="sm" onClick={selectAllCategories}>
                      All
                    </Button>
                    <Button variant="outline" size="sm" onClick={clearCategories}>
                      None
                    </Button>
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  {Array.from(categories).sort().map((category) => (
                    <Button
                      key={category}
                      variant={selectedCategories.has(category) ? "default" : "outline"}
                      size="sm"
                      className="justify-start"
                      onClick={() => handleToggleCategoryFilter(category)}
                    >
                      {category}
                    </Button>
                  ))}
                </div>
              </div>
            )}

            <ScrollArea className="flex-1 pr-4">
              {Object.entries(groupedAvailableItems).map(([category, items]) => (
                <div key={category} className="mb-4">
                  <Button
                    variant="ghost"
                    className="w-full justify-between px-2 py-1"
                    onClick={() => toggleCategory(category)}
                  >
                    <span className="font-medium">{category}</span>
                    {expandedCategories[category] ? (
                      <ChevronDown className="h-4 w-4" />
                    ) : (
                      <ChevronRight className="h-4 w-4" />
                    )}
                  </Button>
                  {expandedCategories[category] && (
                    <div className="space-y-1 mt-1">
                      {items.map((item) => {
                        const { displayName, description, identifier, providerText, Icon, iconColor, isAgent } = getItemDisplayInfo(item);
                        const isSelected = isItemSelected(item);
                        return (
                          <div
                            key={identifier}
                            className={`flex items-center gap-2 p-2 rounded-md cursor-pointer hover:bg-muted ${
                              isSelected ? "bg-muted" : ""
                            }`}
                            onClick={() => isSelected ? handleRemoveToolById(identifier) : handleAddItem(item)}
                          >
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
                                handleRemoveToolById(identifier);
                              }}
                            >
                              <XCircle className="h-4 w-4" />
                            </Button>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              ))}
            </ScrollArea>
          </div>

          {/* Right panel - Selected tools */}
          <div className="w-1/2 flex flex-col">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-medium">Selected Tools</h3>
              {localSelectedComponents.length > 0 && (
                <Button variant="ghost" size="sm" onClick={clearAllSelectedTools}>
                  Clear All
                </Button>
              )}
            </div>

            <ScrollArea className="flex-1">
              {localSelectedComponents.length > 0 ? (
                <div className="space-y-2">
                  {localSelectedComponents.map((entry) => {
                    const { displayName, description, identifier: toolInstanceIdentifier, providerText, Icon, iconColor, isAgent } = getItemDisplayInfo(entry.toolInstance);
                    return [
                      <div key={toolInstanceIdentifier} className="flex items-center gap-2 p-2 rounded-md bg-muted">
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
                    ];
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
