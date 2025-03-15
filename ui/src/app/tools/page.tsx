"use client";

import { useState, useEffect } from "react";
import { Search, Download, FunctionSquare, Filter, Info, AlertCircle, Server, Globe, Terminal, Trash2, Settings, ChevronDown, ChevronRight, MoreHorizontal, Plus } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { getToolDescription, getToolDisplayName, getToolIdentifier, isCommandMcpTool } from "@/lib/data";
import { Component, MCPServer, MCPServerConfig, Tool,   ToolConfig } from "@/types/datamodel";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { bulkSaveTools, getTools } from "../actions/tools";
import { DiscoverToolsDialog } from "@/components/create/DiscoverToolsDialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { createServer, deleteServer, getServers, refreshServerTools } from "../actions/servers";
import { AddServerDialog } from "@/components/AddServerDialog";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { SseServerParams, StdioServerParameters } from "@/lib/types";
import { create } from "domain";


// Extract category from tool identifier (e.g., "istio" from "kagent.tools.istio.RemoteClusters")
const getToolCategory = (tool: Tool): string => {
    const component = tool.component;
  const providerId = getToolIdentifier(component);
  const parts = providerId.split(".");
  
  if (parts.length >= 3 && parts[0] === "kagent" && parts[1] === "tools") {
    return parts[2]; // Return the category part (e.g., "istio")
  }
  
  // Handle other patterns
  if (component.provider) {
    const providerParts = component.provider.split(".");
    if (providerParts.length >= 3) {
      return providerParts[2]; // Try to extract category from provider
    }
  }
  
  return "other"; // Default category
};

// Function to format date
const formatDate = (dateString: string | null): string => {
  if (!dateString) return 'Never';
  
  try {
    const date = new Date(dateString);
    return new Intl.DateTimeFormat('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    }).format(date);
  } catch (e) {
    return 'Invalid date';
  }
};

export default function ToolsPage() {
  // State for active tab
  const [activeTab, setActiveTab] = useState<"tools" | "servers">("tools");
  
  // State for tools
  const [allTools, setAllTools] = useState<Tool[]>([]);
  const [searchTerm, setSearchTerm] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState<number | null>(null);
  const [showFilters, setShowFilters] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  
  // State for servers
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [expandedServers, setExpandedServers] = useState<Set<number>>(new Set());
  
  // Dialog states
  const [showDiscoverTools, setShowDiscoverTools] = useState(false);
  const [showAddServer, setShowAddServer] = useState(false);
  const [showConfirmDelete, setShowConfirmDelete] = useState<number | null>(null);
  
  // Category filter states
  const [categories, setCategories] = useState<Set<string>>(new Set());
  const [selectedCategories, setSelectedCategories] = useState<Set<string>>(new Set());

  // Fetch data on component mount
  useEffect(() => {
    fetchData();
  }, []);

  // Main function to fetch both tools and servers
  const fetchData = async () => {
    try {
      setIsLoading(true);
      setError(null);
      
      // Fetch servers first
      const serversResponse = await getServers();
      if (serversResponse.success && serversResponse.data) {
        setServers(serversResponse.data);
        
        // Expand all servers by default
        setExpandedServers(new Set(serversResponse.data.map(server => server.id).filter(id => id !== undefined) as number[]));
      } else {
        console.error("Failed to fetch servers:", serversResponse);
      }

      // Then fetch tools
      const toolsResponse = await getTools();
      if (toolsResponse.success && toolsResponse.data) {
        setAllTools(toolsResponse.data);
        
        // Extract unique categories
        const uniqueCategories = new Set<string>();
        toolsResponse.data.forEach(tool => {
          const category = getToolCategory(tool);
          uniqueCategories.add(category);
        });
        setCategories(uniqueCategories);
      } else {
        setError(toolsResponse.error || "Failed to fetch tools data.");
      }
    } catch (error) {
      console.error("Error fetching data:", error);
      setError("An error occurred while fetching data.");
    } finally {
      setIsLoading(false);
    }
  };

  // Handle when new tools are discovered (from dialog)
  const handleToolsDiscovered = async (discoveredTools: Tool[]) => {
    try {
      // Check for duplicates
      const newTools = discoveredTools.filter(
        newTool => !allTools.some(existingTool => 
          getToolIdentifier(existingTool.component) === getToolIdentifier(newTool.component)
        )
      );
      
      if (newTools.length > 0) {
        const response = await bulkSaveTools(newTools);

        
        if (!response.success) {
          throw new Error('Failed to save tools');
        }
        
        // Update local state
        setAllTools(prev => [...prev, ...newTools]);
        
        // Update categories
        const updatedCategories = new Set(categories);
        newTools.forEach(tool => {
          const category = getToolCategory(tool);
          updatedCategories.add(category);
        });
        setCategories(updatedCategories);
        
        setSuccess(`Successfully added ${newTools.length} new MCP tool${newTools.length !== 1 ? 's' : ''} to the database.`);
        
        // Refresh the data to ensure consistency
        fetchData();
      } else {
        setError("No new tools were discovered or all discovered tools already exist in the database.");
      }
    } catch (error) {
      console.error("Error saving tools:", error);
      setError(`Failed to save tools: ${error instanceof Error ? error.message : 'Unknown error'}`);
    } finally {
      // Clear messages after 5 seconds
      setTimeout(() => {
        setError(null);
        setSuccess(null);
      }, 5000);
    }
  };

  // Toggle server expansion
  const toggleServerExpansion = (serverId: number) => {
    setExpandedServers(prev => {
      const newSet = new Set(prev);
      if (newSet.has(serverId)) {
        newSet.delete(serverId);
      } else {
        newSet.add(serverId);
      }
      return newSet;
    });
  };

  // Handle server refresh
  const handleRefreshServer = async (serverId: number) => {
    try {
      setIsRefreshing(serverId);
      setError(null);
      
      const response = await refreshServerTools(serverId);
      
      if (response.status) {
        setSuccess(response.message || "Server refreshed successfully");
        fetchData(); // Refresh data to get updated tools
      } else {
        setError(response.message || "Failed to refresh server");
      }
    } catch (error) {
      console.error("Error refreshing server:", error);
      setError(`Failed to refresh server: ${error instanceof Error ? error.message : 'Unknown error'}`);
    } finally {
      setIsRefreshing(null);
      // Clear messages after 5 seconds
      setTimeout(() => {
        setError(null);
        setSuccess(null);
      }, 5000);
    }
  };

  // Handle server deletion
  const handleDeleteServer = async (serverId: number) => {
    try {
      setIsLoading(true);
      setError(null);
      
      const response = await deleteServer(serverId);
      
      if (response.status) {
        setSuccess(response.message || "Server deleted successfully");
        fetchData(); // Refresh data
      } else {
        setError(response.message || "Failed to delete server");
      }
    } catch (error) {
      console.error("Error deleting server:", error);
      setError(`Failed to delete server: ${error instanceof Error ? error.message : 'Unknown error'}`);
    } finally {
      setIsLoading(false);
      setShowConfirmDelete(null);
      // Clear messages after 5 seconds
      setTimeout(() => {
        setError(null);
        setSuccess(null);
      }, 5000);
    }
  };

  // Handle adding a new server
  const handleAddServer = async (server: MCPServerConfig) => {
    try {
      setIsLoading(true);
      setError(null);
      
      // API call to create server
      const response = await createServer(server);

      
      if (!response.success) {
        throw new Error(response.error || 'Failed to add server');
      }
      
      setSuccess("Server added successfully");
      setShowAddServer(false);
      fetchData(); // Refresh data
    } catch (error) {
      console.error("Error adding server:", error);
      setError(`Failed to add server: ${error instanceof Error ? error.message : 'Unknown error'}`);
    } finally {
      setIsLoading(false);
      // Clear messages after 5 seconds
      setTimeout(() => {
        setError(null);
        setSuccess(null);
      }, 5000);
    }
  };

  // Filter tools based on search term and category selections
  const filteredTools = allTools.filter(tool => {
    // Search matching
    const searchLower = searchTerm.toLowerCase();
    const matchesSearch =
      getToolDisplayName(tool.component)?.toLowerCase().includes(searchLower) ||
      getToolDescription(tool.component)?.toLowerCase().includes(searchLower) ||
      tool.component.provider?.toLowerCase().includes(searchLower) ||
      getToolIdentifier(tool.component)?.toLowerCase().includes(searchLower);
    
    // Category matching
    const toolCategory = getToolCategory(tool);
    const matchesCategory = selectedCategories.size === 0 || selectedCategories.has(toolCategory);

    return matchesSearch && matchesCategory;
  });

  // Group tools by category
  const toolsByCategory: Record<string, Tool[]> = {};
  filteredTools.forEach(tool => {
    const category = getToolCategory(tool);
    if (!toolsByCategory[category]) {
      toolsByCategory[category] = [];
    }
    toolsByCategory[category].push(tool);
  });

  // Group tools by server
  const toolsByServer: Record<number, Tool[]> = {};
  servers.forEach(server => {

    if (server.id){
    toolsByServer[server.id] = allTools.filter(tool => {
      return tool.server_id === server.id;
    });
}
  });

  // Filter handlers
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

  // Selection control functions
  const selectAllCategories = () => setSelectedCategories(new Set(categories));
  const clearCategories = () => setSelectedCategories(new Set());

  return (
    <div className="mt-12 mx-auto max-w-6xl px-6">
      <div className="flex justify-between items-center mb-8">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">Tools</h1>
        </div>
      </div>

      {/* Alerts */}
      {error && (
        <Alert variant="destructive" className="mb-6">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {success && (
        <Alert variant="default" className="mb-6 bg-green-50 border-green-200 text-green-800">
          <AlertCircle className="h-4 w-4 text-green-500" />
          <AlertTitle>Success</AlertTitle>
          <AlertDescription>{success}</AlertDescription>
        </Alert>
      )}

      {/* Tabs */}
      <Tabs value={activeTab} onValueChange={(value) => setActiveTab(value as "tools" | "servers")} className="mb-6">
        <TabsList className="grid w-full grid-cols-2">
          <TabsTrigger value="tools" className="flex items-center gap-2">
            <FunctionSquare className="h-4 w-4" />
            Tools Library
          </TabsTrigger>
          <TabsTrigger value="servers" className="flex items-center gap-2">
            <Server className="h-4 w-4" />
            MCP Servers
          </TabsTrigger>
        </TabsList>

        {/* Tools Tab Content */}
        <TabsContent value="tools" className="mt-0">
          {/* Search and filter controls */}
          <div className="flex gap-2 mb-4">
            <div className="relative flex-1">
              <Search className="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
              <Input placeholder="Search tools by name, description or provider..." value={searchTerm} onChange={(e) => setSearchTerm(e.target.value)} className="pl-10" />
            </div>
            <Button variant="outline" size="icon" onClick={() => setShowFilters(!showFilters)} className={showFilters ? "bg-secondary" : ""}>
              <Filter className="h-4 w-4" />
            </Button>
          </div>

          {/* Category filters UI */}
          {showFilters && (
            <div className="mb-6 p-4 border rounded-md bg-secondary/10">
              <h3 className="text-sm font-medium mb-3">Filter by Category</h3>
              <div className="flex flex-wrap gap-2 mb-3">
                {Array.from(categories)
                  .sort()
                  .map((category) => (
                    <Badge key={category} variant={selectedCategories.has(category) ? "default" : "outline"} className="cursor-pointer capitalize" onClick={() => handleToggleCategory(category)}>
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

          {/* Tools list counter */}
          <div className="flex justify-between items-center mb-4">
            <h2 className="text-xl font-semibold">Available Tools</h2>
            <div className="text-sm text-muted-foreground">{filteredTools.length} tools found</div>
          </div>

          {/* Tools list grouped by category */}
          {isLoading ? (
            <div className="flex flex-col items-center justify-center h-[200px] border rounded-lg bg-secondary/5">
              <div className="animate-pulse h-6 w-6 rounded-full bg-primary/10 mb-4"></div>
              <p className="text-muted-foreground">Loading tools...</p>
            </div>
          ) : Object.keys(toolsByCategory).length > 0 ? (
            <ScrollArea className="h-[650px] pr-4 -mr-4">
              <div className="space-y-8">
                {Object.entries(toolsByCategory)
                  .sort(([a], [b]) => a.localeCompare(b)) // Sort categories alphabetically
                  .map(([category, tools]) => (
                    <div key={category}>
                      <div className="flex items-center gap-2 mb-3 pb-2 border-b">
                        <h3 className="text-lg font-semibold capitalize">{category}</h3>
                        <Badge variant="outline" className="bg-blue-50 text-blue-700">
                          {tools.length} tool{tools.length !== 1 ? "s" : ""}
                        </Badge>
                      </div>

                      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        {tools
                          .sort((a, b) => {
                            // Sort by name within category
                            const aName = getToolDisplayName(a) || "";
                            const bName = getToolDisplayName(b) || "";
                            return aName.localeCompare(bName);
                          })
                          .map((tool) => (
                            <div key={getToolIdentifier(tool.component)} className="p-4 border rounded-md hover:bg-secondary/5 transition-colors">
                              <div className="flex items-start justify-between gap-2">
                                <div className="flex items-start gap-2">
                                  <FunctionSquare className="h-5 w-5 text-blue-500 mt-0.5" />
                                  <div>
                                    <div className="font-medium">{getToolDisplayName(tool.component)}</div>
                                    <div className="text-sm text-muted-foreground mt-1">{getToolDescription(tool.component)}</div>
                                    <div className="text-xs text-muted-foreground mt-2 flex items-center">
                                      <Server className="h-3 w-3 mr-1" />
                                      {servers.find(s => s.id === tool.server_id)?.component.label || "Built-in tool"}
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
                                      <p className="font-mono text-xs">{getToolIdentifier(tool.component)}</p>
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
            <div className="flex flex-col items-center justify-center h-[200px] text-center p-4 border rounded-lg bg-secondary/5">
              <Search className="h-12 w-12 text-muted-foreground mb-4 opacity-20" />
              <h3 className="font-medium text-lg">No tools found</h3>
              <p className="text-muted-foreground mt-1">Try adjusting your search or filters, or discover new tools.</p>
              <Button onClick={() => setShowDiscoverTools(true)} className="mt-4 bg-blue-500 hover:bg-blue-600 text-white">
                <Download className="h-4 w-4 mr-2" />
                Discover MCP Tools
              </Button>
            </div>
          )}
        </TabsContent>

        {/* Servers Tab Content */}
        <TabsContent value="servers" className="mt-0">
          <div className="flex justify-between items-center mb-6">
            <Button onClick={() => setShowAddServer(true)} className="border-blue-500 text-blue-600 hover:bg-blue-50" variant="outline">
              <Plus className="h-4 w-4 mr-2" />
              Add Server
            </Button>
          </div>

          {isLoading ? (
            <div className="flex flex-col items-center justify-center h-[200px] border rounded-lg bg-secondary/5">
              <div className="animate-pulse h-6 w-6 rounded-full bg-primary/10 mb-4"></div>
              <p className="text-muted-foreground">Loading servers...</p>
            </div>
          ) : servers.length > 0 ? (
            <div className="space-y-4">
              {servers.map((server) => {
                if (!server.id) return null;
                console.log(JSON.stringify(server));

                const serverId: number = server.id;
                return <div key={server.id} className="border rounded-md overflow-hidden">
                  {/* Server Header */}
                  <div className="bg-secondary/10 p-4">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3 cursor-pointer" onClick={() => toggleServerExpansion(serverId)}>
                        {expandedServers.has(serverId) ? <ChevronDown className="h-5 w-5" /> : <ChevronRight className="h-5 w-5" />}

                        <div className="flex items-center gap-2">
                          {isCommandMcpTool(server) ? (
                            <Terminal className="h-5 w-5 text-violet-500" />
                          ) : (
                            <Globe className="h-5 w-5 text-green-500" />
                          )}
                          <div>
                            <div className="font-medium">{server.component.label || server.component.provider}</div>
                            <div className="text-xs text-muted-foreground flex items-center gap-2">
                              <span className="font-mono">{server.component.config.name}</span>
                              <Badge variant="outline" className="bg-blue-50 text-blue-700">
                                {(toolsByServer[serverId] || []).length} tool{(toolsByServer[serverId] || []).length !== 1 ? "s" : ""}
                              </Badge>
                              {server.last_connected && (
                                <span className="text-xs text-muted-foreground">
                                  Last updated: {formatDate(server.last_connected)}
                                </span>
                              )}
                            </div>
                          </div>
                        </div>
                      </div>

                      <div className="flex items-center gap-2">
                        <Button 
                          variant="ghost" 
                          size="sm" 
                          onClick={() => handleRefreshServer(serverId)} 
                          className="h-8 text-blue-600 hover:text-blue-700 hover:bg-blue-50"
                          disabled={isRefreshing === server.id}
                        >
                          {isRefreshing === server.id ? (
                            <>
                              <div className="h-4 w-4 rounded-full border-2 border-blue-600 border-t-transparent animate-spin mr-2" />
                              Refreshing...
                            </>
                          ) : (
                            'Refresh'
                          )}
                        </Button>

                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8">
                              <MoreHorizontal className="h-4 w-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem className="text-red-600" onClick={() => setShowConfirmDelete(serverId)}>
                              <Trash2 className="h-4 w-4 mr-2" />
                              Remove Server
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </div>
                  </div>

                  {/* Server Tools List */}
                  {expandedServers.has(serverId) && (
                    <div className="p-4">
                      {(toolsByServer[serverId] || []).length > 0 ? (
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                          {toolsByServer[serverId]
                            .sort((a, b) => {
                              const aName = getToolDisplayName(a.component) || "";
                              const bName = getToolDisplayName(b.component) || "";
                              return aName.localeCompare(bName);
                            })
                            .map((tool) => (
                              <div key={getToolIdentifier(tool.component)} className="p-3 border rounded-md hover:bg-secondary/5 transition-colors">
                                <div className="flex items-start gap-2">
                                  <FunctionSquare className="h-4 w-4 text-blue-500 mt-0.5" />
                                  <div>
                                    <div className="font-medium text-sm">{getToolDisplayName(tool.component)}</div>
                                    <div className="text-xs text-muted-foreground mt-1">{getToolDescription(tool.component)}</div>
                                    <div className="text-xs text-muted-foreground mt-1 font-mono">{getToolIdentifier(tool.component)}</div>
                                  </div>
                                </div>
                              </div>
                            ))}
                        </div>
                      ) : (
                        <div className="text-center p-4 text-sm text-muted-foreground">
                          No tools available for this server.
                          <div className="mt-2">
                            <Button 
                              variant="outline" 
                              size="sm" 
                              onClick={() => handleRefreshServer(serverId)} 
                              disabled={isRefreshing === serverId}
                              className="text-blue-600"
                            >
                              Refresh to discover tools
                            </Button>
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </div>
})}
            </div>
          ) : (
            <div className="flex flex-col items-center justify-center h-[200px] text-center p-4 border rounded-lg bg-secondary/5">
              <Server className="h-12 w-12 text-muted-foreground mb-4 opacity-20" />
              <h3 className="font-medium text-lg">No servers connected</h3>
              <p className="text-muted-foreground mt-1 mb-4">Add an MCP server to discover and use tools.</p>
              <Button onClick={() => setShowAddServer(true)} className="bg-blue-500 hover:bg-blue-600 text-white">
                <Plus className="h-4 w-4 mr-2" />
                Add Server
              </Button>
            </div>
          )}
        </TabsContent>
      </Tabs>

      {/* Dialogs */}
      <DiscoverToolsDialog 
        open={showDiscoverTools} 
        onOpenChange={setShowDiscoverTools} 
        onShowSelectTools={handleToolsDiscovered}
      />
      
      {/* Add server dialog component */}
      <AddServerDialog
        open={showAddServer}
        onOpenChange={setShowAddServer}
        onAddServer={handleAddServer}
      />
      
      {/* Confirm delete dialog */}
      <ConfirmDialog
        open={showConfirmDelete !== null}
        onOpenChange={() => setShowConfirmDelete(null)}
        title="Delete Server"
        description="Are you sure you want to delete this server? This will also delete all associated tools and cannot be undone."
        onConfirm={() => showConfirmDelete !== null && handleDeleteServer(showConfirmDelete)}
      />
    </div>
  );
}