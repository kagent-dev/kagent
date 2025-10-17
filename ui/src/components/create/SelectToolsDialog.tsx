import { useState, useEffect, useMemo } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Search, Filter, ChevronDown, ChevronRight, AlertCircle, PlusCircle, XCircle, FunctionSquare, LucideIcon } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import type { AgentResponse, Tool, ToolsResponse } from "@/types";
import ProviderFilter from "./ProviderFilter";
import Link from "next/link";
import { getToolResponseDisplayName, getToolResponseDescription, getToolResponseCategory, getToolResponseIdentifier, isAgentTool, isAgentResponse, isMcpTool, toolResponseToAgentTool, groupMcpToolsByServer } from "@/lib/toolUtils";
import { toast } from "sonner";
import KagentLogo from "../kagent-logo";
import { k8sRefUtils } from "@/lib/k8sUtils";

// Maximum number of tools that can be selected
const MAX_TOOLS_LIMIT = 20;

interface SelectToolsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  availableTools: ToolsResponse[];
  selectedTools: Tool[];
  onToolsSelected: (tools: Tool[]) => void;
  availableAgents: AgentResponse[];
  loadingAgents: boolean;
}

// Helper function to get display info for a tool or agent
const getItemDisplayInfo = (item: ToolsResponse | AgentResponse): {
  displayName: string;
  description?: string;
  identifier: string;
  providerText?: string;
  Icon: React.ElementType | LucideIcon;
  iconColor: string;
  isAgent: boolean;
} => {

  if (isAgentResponse(item)) {
    const agentResp = item as AgentResponse;
    const displayName = k8sRefUtils.toRef(agentResp.agent.metadata.namespace || "", agentResp.agent.metadata.name);
    return {
      displayName,
      description: agentResp.agent.spec.description,
      identifier: `agent-${displayName}`,
      providerText: "Agent",
      Icon: KagentLogo,
      iconColor: "text-green-500",
      isAgent: true
    };
  } else {
    const tool = item as ToolsResponse;
    return {
      displayName: getToolResponseDisplayName(tool),
      description: getToolResponseDescription(tool),
      identifier: getToolResponseIdentifier(tool),
      providerText: getToolResponseCategory(tool),
      Icon: FunctionSquare,
      iconColor: "text-blue-400",
      isAgent: false
    };
  }
};

export const SelectToolsDialog: React.FC<SelectToolsDialogProps> = ({ open, onOpenChange, availableTools, selectedTools, onToolsSelected, availableAgents, loadingAgents }) => {
  const [searchTerm, setSearchTerm] = useState("");
  const [localSelectedTools, setLocalSelectedTools] = useState<Tool[]>([]);
  const [categories, setCategories] = useState<Set<string>>(new Set());
  const [selectedCategories, setSelectedCategories] = useState<Set<string>>(new Set());
  const [showFilters, setShowFilters] = useState(false);
  const [expandedCategories, setExpandedCategories] = useState<{ [key: string]: boolean }>({});

  // Initialize state when dialog opens
  useEffect(() => {
    if (open) {
      setLocalSelectedTools(selectedTools);
      setSearchTerm("");

      const uniqueCategories = new Set<string>();
      const categoryCollapseState: { [key: string]: boolean } = {};
      
      // Process available tools to extract categories
      availableTools.forEach((tool) => {
          const category = getToolResponseCategory(tool);
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
    return localSelectedTools.reduce((acc, tool) => {
      if (tool.mcpServer && tool.mcpServer.toolNames && tool.mcpServer.toolNames.length > 0) {
        return acc + tool.mcpServer.toolNames.length;
      }
      return acc + 1;
    }, 0);
  }, [localSelectedTools]);

  const isLimitReached = actualSelectedCount >= MAX_TOOLS_LIMIT;

  // Filter tools based on search and category selections
  const filteredAvailableItems = useMemo(() => {
    const searchLower = searchTerm.toLowerCase();

    // Flatten all tools from all servers
    const allTools: Array<{ tool: ToolsResponse; server: ToolsResponse }> = [];
    availableTools.forEach((tool) => {
      allTools.push({ tool, server: tool });
    });

    const tools = allTools.filter(({ tool, server }) => {
      const toolName = getToolResponseDisplayName(tool).toLowerCase();
      const toolDescription = getToolResponseDescription(tool).toLowerCase();
      const toolProvider = server.server_name?.toLowerCase() || "";

      const matchesSearch = toolName.includes(searchLower) || toolDescription.includes(searchLower) || toolProvider.includes(searchLower);

      const toolCategory = getToolResponseCategory(tool);
      const matchesCategory = selectedCategories.size === 0 || selectedCategories.has(toolCategory);
      return matchesSearch && matchesCategory;
    });

    // Filter agents if "Agents" category is selected or no category is selected
    const agentCategorySelected = selectedCategories.size === 0 || selectedCategories.has("Agents");
    const agents = agentCategorySelected ? availableAgents.filter(agentResp => {
        const agentRef = k8sRefUtils.toRef(agentResp.agent.metadata.namespace || "", agentResp.agent.metadata.name).toLowerCase();
        const agentDesc = agentResp.agent.spec.description?.toLowerCase();
        return agentRef.includes(searchLower) || agentDesc.includes(searchLower);
      })
    : [];

    return { tools, agents };
  }, [availableTools, availableAgents, searchTerm, selectedCategories]);

  // Group available tools and agents by category
  const groupedAvailableItems = useMemo(() => {
    const groups: { [key: string]: Array< ToolsResponse | AgentResponse> } = {};
    
    const sortedTools = [...filteredAvailableItems.tools].sort((a, b) => {
      return getToolResponseDisplayName(a.tool).localeCompare(getToolResponseDisplayName(b.tool));
    });
    
    sortedTools.forEach(({ tool }) => {
      const category = getToolResponseCategory(tool);
      if (!groups[category]) {
        groups[category] = [];
      }
      groups[category].push(tool);
    });

    // Add agents to the "Agents" category
    if (filteredAvailableItems.agents.length > 0) {
      groups["Agents"] = filteredAvailableItems.agents.sort((a, b) => {
        const aRef = k8sRefUtils.toRef(a.agent.metadata.namespace || "", a.agent.metadata.name)
        const bRef = k8sRefUtils.toRef(b.agent.metadata.namespace || "", b.agent.metadata.name)
        return aRef.localeCompare(bRef)
      });
    }
    
    // Sort categories alphabetically
    return Object.entries(groups).sort(([catA], [catB]) => catA.localeCompare(catB))
           .reduce((acc, [key, value]) => { acc[key] = value; return acc; }, {} as typeof groups);
           
  }, [filteredAvailableItems]);

  const isItemSelected = (item: ToolsResponse | AgentResponse): boolean => {
    let identifier: string;
    if (isAgentResponse(item)) {
      const agentResp = item as AgentResponse;
      identifier = `agent-${k8sRefUtils.toRef(agentResp.agent.metadata.namespace || "", agentResp.agent.metadata.name)}`;
    } else {
      const tool = item as ToolsResponse;
      identifier = getToolResponseIdentifier(tool);
    }
    
    return localSelectedTools.some(tool => {
      if (isAgentTool(tool)) {
        const compare = identifier.replace('agent-', '');
        return tool.agent?.name === compare || tool.agent?.name === compare.split('/').pop();
      } else if (isMcpTool(tool)) {
        const mcpTool = tool as Tool;
        return mcpTool.mcpServer?.name === identifier.split('-')[1];
      }
      return false;
    });
  };

  const handleAddItem = (item: ToolsResponse | AgentResponse) => {
    let toolToAdd: Tool;

    if (isAgentResponse(item)) {
      const agentResp = item as AgentResponse;
      const agentRef = k8sRefUtils.toRef(agentResp.agent.metadata.namespace || "", agentResp.agent.metadata.name);
      toolToAdd = {
        type: "Agent",
        agent: {
          name: agentRef,
          kind: "Agent",
          apiGroup: "kagent.dev",
        }
      };
    } else {
      const tool = item as ToolsResponse;
      toolToAdd = toolResponseToAgentTool(tool, tool.server_name);
    }

    if (actualSelectedCount + 1 <= MAX_TOOLS_LIMIT) {
      setLocalSelectedTools(prev => [...prev, toolToAdd]);
    } else {
      console.warn(`Cannot add tool. Limit reached. Current: ${actualSelectedCount}, Limit: ${MAX_TOOLS_LIMIT}`);
    }
  };

  const handleRemoveTool = (toolToRemove: Tool) => {
    setLocalSelectedTools(prev => prev.filter(tool => tool !== toolToRemove));
  };

  const handleSave = () => {
    const { groupedTools, errors } = groupMcpToolsByServer(localSelectedTools);
    
    if (errors.length > 0) {
      const errorList = errors.join('\n- ');
      toast.warning(`Tools skipped:\n- ${errorList}`);
    }
    
    onToolsSelected(groupedTools);
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
  const clearAllSelectedTools = () => setLocalSelectedTools([]);

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
                    const itemsSelectedInCategory = items.reduce((count, item) => {
                      return count + (isItemSelected(item) ? 1 : 0);
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
                                      // Find and remove the tool
                                      if ('agent' in item) {
                                        const agentResp = item as AgentResponse;
                                        const agentRef = k8sRefUtils.toRef(agentResp.agent.metadata.namespace || "", agentResp.agent.metadata.name);
                                        const toolToRemove = localSelectedTools.find(tool => 
                                          isAgentTool(tool) && tool.agent?.name === agentRef
                                        );
                                        if (toolToRemove) handleRemoveTool(toolToRemove);
                                      } else {
                                        const tool = item as ToolsResponse;
                                        const toolToRemove = localSelectedTools.find(t =>  {
                                          const mcpTool = t as Tool;
                                          return mcpTool.mcpServer?.name === tool.server_name && mcpTool.mcpServer?.toolNames?.includes(tool.id)
                                        });
                                        if (toolToRemove) handleRemoveTool(toolToRemove);
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
              {localSelectedTools.length > 0 ? (
                <div className="space-y-2">
                  {localSelectedTools.flatMap((tool) => {
                    if (tool.mcpServer && tool.mcpServer.toolNames && tool.mcpServer.toolNames.length > 0) {
                      return tool.mcpServer.toolNames.map((toolName: string) => {
                        const foundServer = availableTools.find(
                          server => server.server_name === tool.mcpServer?.name
                        );
                        const specificDescription = foundServer?.description;
                        
                        return (
                        <div key={`${tool.mcpServer?.name}-${toolName}`} className="flex items-center justify-between p-3 border rounded-md bg-muted/30 min-w-0">
                          <div className="flex items-center gap-2 flex-1 overflow-hidden">
                            <FunctionSquare className="h-4 w-4 flex-shrink-0 text-blue-400" />
                            <div className="flex-1 overflow-hidden">
                              <p className="text-sm font-medium truncate">{toolName}</p>
                              {specificDescription && (
                                <p className="text-xs text-muted-foreground truncate">{specificDescription}</p>
                              )}
                            </div>
                          </div>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6 ml-2 flex-shrink-0"
                            onClick={() => {
                              // For MCP tools, we need to remove the specific tool from the toolNames array
                              // or remove the entire entry if it's the last tool
                              const updatedTool = {
                                ...tool,
                                mcpServer: {
                                  ...tool.mcpServer!,
                                  toolNames: tool.mcpServer!.toolNames!.filter(name => name !== toolName)
                                }
                              };
                              
                              if (updatedTool.mcpServer.toolNames.length === 0) {
                                // Remove the entire entry if no tools left
                                handleRemoveTool(tool);
                              } else {
                                // Update the entry with the remaining tools
                                setLocalSelectedTools((prev) => 
                                  prev.map(t => t === tool ? updatedTool : t)
                                );
                              }
                            }}
                          >
                            <XCircle className="h-4 w-4" />
                          </Button>
                        </div>
                        );
                      });
                    } else {
                      const matchedAgent = isAgentTool(tool)
                        ? availableAgents.find(a => {
                            const ref = k8sRefUtils.toRef(a.agent.metadata.namespace || "", a.agent.metadata.name);
                            return ref === tool.agent?.name || a.agent.metadata.name === tool.agent?.name;
                          })
                        : undefined;

                      const matchedTool = !isAgentTool(tool)
                        ? availableTools.find(s => s.server_name === tool.mcpServer?.name)
                        : undefined;

                      const { displayName, description, Icon, iconColor } = getItemDisplayInfo(
                        (matchedAgent as AgentResponse) || (matchedTool as ToolsResponse)
                      );
                      
                      return [( 
                        <div key={displayName} className="flex items-center justify-between p-3 border rounded-md bg-muted/30 min-w-0">
                          <div className="flex items-center gap-2 flex-1 overflow-hidden">
                            <Icon className={`h-4 w-4 flex-shrink-0 ${iconColor}`} />
                            <div className="flex-1 overflow-hidden">
                              <p className="text-sm font-medium truncate">{displayName}</p>
                              {description && <p className="text-xs text-muted-foreground truncate">{description}</p>}
                            </div>
                          </div>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6 ml-2 flex-shrink-0"
                            onClick={() => handleRemoveTool(tool)}
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
