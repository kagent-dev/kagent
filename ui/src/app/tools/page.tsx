"use client";

import { useState, useEffect, useMemo } from "react";
import { Search, FunctionSquare, Filter, Info, AlertCircle, Server, ChevronDown, ChevronRight } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { getToolCategory, getToolDescription, getToolDisplayName, getToolIdentifier, getToolProvider, isMcpProvider } from "@/lib/toolUtils";
import {  Component, ToolConfig, ToolServerConfiguration } from "@/types/datamodel";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { getTools } from "../actions/tools";
import { getServers } from "../actions/servers";
import Link from "next/link";
import CategoryFilter from "@/components/tools/CategoryFilter";
import McpIcon from "@/components/icons/McpIcon";

export default function ToolsPage() {
  // Consolidated state
  const [toolsData, setToolsData] = useState<{
    tools: Component<ToolConfig>[];
    serversMap: Map<string, { name: string; label: string; config: ToolServerConfiguration }>;
    categories: Set<string>;
    isLoading: boolean;
    error: string | null;
  }>({
    tools: [],                 // Normalized tools from both sources
    serversMap: new Map(),     // Map of server_id to server name/label
    categories: new Set(),     // Unique categories
    isLoading: true,
    error: null
  });
  
  // UI state
  const [searchTerm, setSearchTerm] = useState<string>("");
  const [showFilters, setShowFilters] = useState<boolean>(false);
  const [selectedCategories, setSelectedCategories] = useState<Set<string>>(new Set());
  const [expandedCategories, setExpandedCategories] = useState<{ [key: string]: boolean }>({});

  // Fetch data on component mount
  useEffect(() => {
    fetchData();
  }, []);

  // Fetch and consolidate tools data
  const fetchData = async () => {
    try {
      setToolsData(prev => ({ ...prev, isLoading: true, error: null }));

      // Fetch both data sources in parallel
      const [serversResponse, toolsResponse] = await Promise.all([
        getServers(),
        getTools()
      ]);

      // Process servers
      const serversMap = new Map<string, { name: string; label: string; config: ToolServerConfiguration }>();
      const toolsFromServers: Component<ToolConfig>[] = [];

      if (serversResponse.success && serversResponse.data) {
        serversResponse.data.forEach(server => {
          serversMap.set(server.name, {
            name: server.name,
            label: server.name,
            config: server.config
          });
          
          // Process discovered tools from this server
          if (server.discoveredTools && Array.isArray(server.discoveredTools)) {
            server.discoveredTools.forEach(tool => {
              const labeledTool = {
                ...tool.component,
                label: server.name
              };
              toolsFromServers.push(labeledTool);
            });
          }
        });
      }

      // Process DB tools
      let allTools: Component<ToolConfig>[] = [];
      if (toolsResponse.success && toolsResponse.data) {
        allTools = [...toolsResponse.data];
      }
      
      // Combine tools from both sources (prioritizing DB tools if there are duplicates)
      // This assumes getToolIdentifier returns a unique identifier for each tool
      const toolMap = new Map<string, Component<ToolConfig>>();
      
      // First add all DB tools
      allTools.forEach(tool => {
        const toolId = getToolIdentifier(tool);
        toolMap.set(toolId, tool);
      });
      
      // Then add server tools only if they don't already exist
      toolsFromServers.forEach(tool => {
        const toolId = getToolIdentifier(tool);
        if (!toolMap.has(toolId)) {
          toolMap.set(toolId, tool);
        }
      });
      
      // Convert map back to array
      const consolidatedTools = Array.from(toolMap.values());
      
      // Extract unique categories and initialize expanded state
      const uniqueCategories = new Set<string>();
      const initialExpandedState: { [key: string]: boolean } = {};
      consolidatedTools.forEach(tool => {
        const category = getToolCategory(tool);
        uniqueCategories.add(category);
        initialExpandedState[category] = true;
      });

      // Update state with consolidated data
      setToolsData({
        tools: consolidatedTools,
        serversMap,
        categories: uniqueCategories,
        isLoading: false,
        error: null
      });
      setExpandedCategories(initialExpandedState);

    } catch (error) {
      console.error("Error fetching data:", error);
      setToolsData(prev => ({
        ...prev,
        isLoading: false,
        error: "An error occurred while fetching data."
      }));
    }
  };

  // Category filter handlers
  const handleToggleCategory = (category: string) => {
    setSelectedCategories(prev => {
      const newSelection = new Set(prev);
      if (newSelection.has(category)) {
        newSelection.delete(category);
      } else {
        newSelection.add(category);
      }
      return newSelection;
    });
  };

  const selectAllCategories = () => setSelectedCategories(new Set(toolsData.categories));
  const clearCategories = () => setSelectedCategories(new Set());

  // Filter tools based on search and categories
  const filteredTools = useMemo(() => {
    return toolsData.tools.filter(tool => {
      const searchLower = searchTerm.toLowerCase();
      const matchesSearch =
        getToolDisplayName(tool)?.toLowerCase().includes(searchLower) ||
        getToolDescription(tool)?.toLowerCase().includes(searchLower) ||
        getToolProvider(tool)?.toLowerCase().includes(searchLower) ||
        getToolIdentifier(tool)?.toLowerCase().includes(searchLower);

      const toolCategory = getToolCategory(tool);
      const matchesCategory = selectedCategories.size === 0 || selectedCategories.has(toolCategory);

      return matchesSearch && matchesCategory;
    });
  }, [toolsData.tools, searchTerm, selectedCategories]);

  // Group tools by category
  const toolsByCategory = useMemo(() => {
    const groups: Record<string, Component<ToolConfig>[]> = {};
    const sortedTools = [...filteredTools].sort((a, b) => {
      const aName = getToolDisplayName(a) || "";
      const bName = getToolDisplayName(b) || "";
      return aName.localeCompare(bName);
    });

    sortedTools.forEach(tool => {
      const category = getToolCategory(tool);
      if (!groups[category]) {
        groups[category] = [];
      }
      groups[category].push(tool);
    });
    
    return Object.entries(groups).sort(([catA], [catB]) => catA.localeCompare(catB))
           .reduce((acc, [key, value]) => { acc[key] = value; return acc; }, {} as typeof groups);
  }, [filteredTools]);

  // Handler to toggle category expansion
  const toggleCategory = (category: string) => {
    setExpandedCategories((prev) => ({ ...prev, [category]: !prev[category] }));
  };

  // Helper to highlight search term
  const highlightMatch = (text: string | undefined | null, highlight: string) => {
    if (!text || !highlight) return text;
    const parts = text.split(new RegExp(`(${highlight})`, 'gi'));
    return parts.map((part, i) =>
      part.toLowerCase() === highlight.toLowerCase() ? <mark key={i} className="bg-yellow-200 px-0 py-0 rounded">{part}</mark> : part
    );
  };

  return (
    <div className="mt-12 mx-auto max-w-6xl px-6 pb-12">
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">Tools Library</h1>
        <Link href="/servers" className="text-blue-600 hover:text-blue-800 text-sm">
          Manage tool servers →
        </Link>
      </div>

      {/* Alerts */}
      {toolsData.error && (
        <Alert variant="destructive" className="mb-6">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{toolsData.error}</AlertDescription>
        </Alert>
      )}

      {/* Search and filter */}
      <div className="flex gap-2 mb-4">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
          <Input 
            placeholder="Search tools by name, description or provider..." 
            value={searchTerm} 
            onChange={(e) => setSearchTerm(e.target.value)} 
            className="pl-10" 
          />
        </div>
        <Button 
          variant="outline" 
          size="icon" 
          onClick={() => setShowFilters(!showFilters)} 
          className={showFilters ? "bg-secondary" : ""}
        >
          <Filter className="h-4 w-4" />
        </Button>
      </div>

      {/* Category filters */}
      {showFilters && toolsData.categories.size > 1 && (
        <CategoryFilter
          categories={toolsData.categories}
          selectedCategories={selectedCategories}
          onToggleCategory={handleToggleCategory}
          onSelectAll={selectAllCategories}
          onClearAll={clearCategories}
        />
      )}

      {/* Tools counter */}
      <div className="flex justify-end items-center mb-4">
        <div className="text-sm text-muted-foreground">
          {filteredTools.length} tool{filteredTools.length !== 1 ? "s" : ""} found
        </div>
      </div>

      {toolsData.isLoading ? (
        <div className="flex flex-col items-center justify-center h-[200px] border rounded-lg bg-secondary/5">
          <div className="animate-pulse h-6 w-6 rounded-full bg-primary/10 mb-4"></div>
          <p className="text-muted-foreground">Loading tools...</p>
        </div>
      ) : filteredTools.length > 0 ? (
        <ScrollArea className="h-[calc(100vh-300px)] pr-4 -mr-4">
          <div className="space-y-4">
            {Object.entries(toolsByCategory)
              .map(([category, categoryTools]) => {
                // Check if any tool in this category is an MCP tool
                const hasMcpTool = categoryTools.some(tool => isMcpProvider(tool.provider));
                return (
                  <div key={category} className="border rounded-lg overflow-hidden bg-card shadow-sm">
                    <div
                      className="flex items-center justify-between p-3 bg-secondary/50 cursor-pointer hover:bg-secondary/70"
                      onClick={() => toggleCategory(category)}
                    >
                      <div className="flex items-center gap-2">
                        {expandedCategories[category] ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
                        {hasMcpTool && <McpIcon className="w-3.5 h-3.5 text-purple-600" />}
                        <h3 className="font-semibold capitalize text-sm">{highlightMatch(category, searchTerm)}</h3>
                        <Badge variant="secondary" className="font-mono text-xs">{categoryTools.length}</Badge>
                      </div>
                    </div>

                    {expandedCategories[category] && (
                      <div className="divide-y border-t">
                        {categoryTools
                          .map(tool => (
                            <div
                              key={getToolIdentifier(tool)}
                              className="p-3 transition-colors hover:bg-muted/50"
                            >
                              <div className="flex items-start justify-between gap-3">
                                <div className="flex items-start gap-3 flex-1 min-w-0">
                                  <FunctionSquare className="h-5 w-5 text-blue-500 mt-0.5 flex-shrink-0" />
                                  <div className="flex-1 min-w-0">
                                    <div className="font-medium text-sm truncate">{highlightMatch(getToolDisplayName(tool), searchTerm)}</div>
                                    <div className="text-xs text-muted-foreground mt-1 line-clamp-2">
                                      {highlightMatch(getToolDescription(tool), searchTerm)}
                                    </div>
                                    <div className="text-xs text-muted-foreground/80 mt-1.5 flex items-center gap-1.5 font-mono">
                                       <Server className="h-3 w-3" />
                                       <span className="truncate">{highlightMatch(tool.label || 'Unknown Server', searchTerm)}</span>
                                    </div>
                                    <div className="text-xs text-muted-foreground/80 mt-1 flex items-center gap-1.5 font-mono">
                                      Provider: <span className="truncate">{highlightMatch(getToolProvider(tool), searchTerm)}</span>
                                    </div>
                                  </div>
                                </div>

                                <TooltipProvider delayDuration={200}>
                                  <Tooltip>
                                    <TooltipTrigger asChild>
                                      <Button variant="ghost" size="icon" className="h-7 w-7 flex-shrink-0">
                                        <Info className="h-4 w-4" />
                                      </Button>
                                    </TooltipTrigger>
                                    <TooltipContent side="left" className="max-w-xs">
                                      <p className="font-mono text-xs break-all">{getToolIdentifier(tool)}</p>
                                    </TooltipContent>
                                  </Tooltip>
                                </TooltipProvider>
                              </div>
                            </div>
                          ))}
                      </div>
                    )}
                  </div>
                );
              })}
          </div>
        </ScrollArea>
      ) : (
        <div className="flex flex-col items-center justify-center h-[300px] text-center p-4 border rounded-lg bg-secondary/5">
          <FunctionSquare className="h-12 w-12 text-muted-foreground mb-4 opacity-20" />
          <h3 className="font-medium text-lg">No tools found</h3>
          <p className="text-muted-foreground mt-1 mb-4">
            {searchTerm || selectedCategories.size > 0 
              ? "Try adjusting your search or filters to find tools." 
              : "Connect a server to discover tools."}
          </p>
          {searchTerm || selectedCategories.size > 0 ? (
            <div className="flex gap-3">
              <Button
                variant="outline"
                onClick={() => {
                  setSearchTerm("");
                  clearCategories();
                }}
              >
                Clear Filters
              </Button>
            </div>
          ) : (
            <Link href="/servers">
              <Button className="bg-blue-500 hover:bg-blue-600 text-white">
                <Server className="h-4 w-4 mr-2" />
                Manage Servers
              </Button>
            </Link>
          )}
        </div>
      )}
    </div>
  );
}