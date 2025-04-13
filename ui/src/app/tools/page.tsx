"use client";

import { useState, useEffect } from "react";
import { Search, FunctionSquare, Filter, Info, AlertCircle, Server } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { getToolDescription, getToolDisplayName, getToolIdentifier } from "@/lib/toolUtils";
import {  Component, ToolConfig, ToolServerConfiguration } from "@/types/datamodel";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { getTools } from "../actions/tools";
import { getServers } from "../actions/servers";
import Link from "next/link";

// Extract category from tool identifier
const getToolCategory = (component: Component<ToolConfig>): string => {
  const providerId = getToolIdentifier(component);
  const parts = providerId.split(".");

  if (parts.length >= 3 && parts[0] === "kagent" && parts[1] === "tools") {
    return parts[2];
  }

  if (component.provider) {
    const providerParts = component.provider.split(".");
    if (providerParts.length >= 3) {
      return providerParts[2];
    }
  }

  return "other";
};

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
              toolsFromServers.push(tool.component);
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
      
      // Extract unique categories
      const uniqueCategories = new Set<string>();
      consolidatedTools.forEach(tool => {
        uniqueCategories.add(getToolCategory(tool));
      });

      // Update state with consolidated data
      setToolsData({
        tools: consolidatedTools,
        serversMap,
        categories: uniqueCategories,
        isLoading: false,
        error: null
      });
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
  const filteredTools = toolsData.tools.filter(tool => {
    const searchLower = searchTerm.toLowerCase();
    const matchesSearch =
      getToolDisplayName(tool)?.toLowerCase().includes(searchLower) ||
      getToolDescription(tool)?.toLowerCase().includes(searchLower) ||
      tool.provider?.toLowerCase().includes(searchLower) ||
      getToolIdentifier(tool)?.toLowerCase().includes(searchLower);

    const toolCategory = getToolCategory(tool);
    const matchesCategory = selectedCategories.size === 0 || selectedCategories.has(toolCategory);

    return matchesSearch && matchesCategory;
  });

  // Group tools by category
  const toolsByCategory: Record<string, Component<ToolConfig>[]> = {};
  filteredTools.forEach(tool => {
    const category = getToolCategory(tool);
    if (!toolsByCategory[category]) {
      toolsByCategory[category] = [];
    }
    toolsByCategory[category].push(tool);
  });

  return (
    <div className="mt-12 mx-auto max-w-6xl px-6">
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
      {showFilters && (
        <div className="mb-6 p-4 border rounded-md bg-secondary/10">
          <h3 className="text-sm font-medium mb-3">Filter by Category</h3>
          <div className="flex flex-wrap gap-2 mb-3">
            {Array.from(toolsData.categories)
              .sort()
              .map(category => (
                <Badge 
                  key={category} 
                  variant={selectedCategories.has(category) ? "default" : "outline"} 
                  className="cursor-pointer capitalize" 
                  onClick={() => handleToggleCategory(category)}
                >
                  {category}
                </Badge>
              ))}
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={clearCategories}>
              Clear All
            </Button>
            <Button variant="ghost" size="sm" onClick={selectAllCategories}>
              Select All
            </Button>
          </div>
        </div>
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
        <ScrollArea className="h-[650px] pr-4 -mr-4">
          <div className="space-y-8">
            {Object.entries(toolsByCategory)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([category, categoryTools]) => (
                <div key={category}>
                  <div className="flex items-center gap-2 mb-3 pb-2 border-b">
                    <h3 className="text-lg font-semibold uppercase">{category}</h3>
                    <Badge variant="outline" className="bg-blue-50 text-blue-700">
                      {categoryTools.length} tool{categoryTools.length !== 1 ? "s" : ""}
                    </Badge>
                  </div>

                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {categoryTools
                      .sort((a, b) => {
                        const aName = getToolDisplayName(a) || "";
                        const bName = getToolDisplayName(b) || "";
                        return aName.localeCompare(bName);
                      })
                      .map(tool => (
                        <div 
                          key={getToolIdentifier(tool)} 
                          className="p-4 border rounded-md hover:bg-secondary/5 transition-colors"
                        >
                          <div className="flex items-start justify-between gap-2">
                            <div className="flex items-start gap-2">
                              <FunctionSquare className="h-5 w-5 text-blue-500 mt-0.5" />
                              <div>
                                <div className="font-medium">{getToolDisplayName(tool)}</div>
                                <div className="text-sm text-muted-foreground mt-1">
                                  {getToolDescription(tool)}
                                </div>
                                <div className="text-xs text-muted-foreground mt-2 flex items-center">
                                  <Server className="h-3 w-3 mr-1" />
                                  {tool.label}
                                </div>
                              </div>
                            </div>

                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Button variant="ghost" size="icon" className="h-8 w-8">
                                    <Info className="h-4 w-4" />
                                  </Button>
                                </TooltipTrigger>
                                <TooltipContent side="left" className="max-w-sm">
                                  <p className="font-mono text-xs">{getToolIdentifier(tool)}</p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                        </div>
                      ))}
                  </div>
                </div>
              ))}
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