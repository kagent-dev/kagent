"use client";

import { useState, useEffect, useMemo } from "react";
import { Server, Trash2, ChevronDown, ChevronRight, MoreHorizontal, Plus, FunctionSquare, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ToolServerResponse, ToolServerCreateRequest } from "@/types";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { createServer, deleteServer, getServers, getToolServerTypes } from "../actions/servers";
import { AddServerDialog } from "@/components/AddServerDialog";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import Link from "next/link";
import { toast } from "sonner";
import { useAgents } from "@/components/AgentsProvider";

export default function ServersPage() {
  const { refreshTools } = useAgents();

  // State for servers and tools
  const [servers, setServers] = useState<ToolServerResponse[]>([]);
  const [toolServerTypes, setToolServerTypes] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [expandedServers, setExpandedServers] = useState<Set<string>>(new Set());
  const [searchTerm, setSearchTerm] = useState<string>("");

  // Dialog states
  const [showAddServer, setShowAddServer] = useState(false);
  const [showConfirmDelete, setShowConfirmDelete] = useState<string | null>(null);
  const [openDropdownMenu, setOpenDropdownMenu] = useState<string | null>(null);

  // Fetch data on component mount
  useEffect(() => {
    fetchServers();
    fetchToolServerTypes();
  }, []);

  // Auto-expand servers whose tools match the search term
  useEffect(() => {
    if (!searchTerm) return;
    const term = searchTerm.toLowerCase();
    const toExpand = new Set<string>();
    servers.forEach(server => {
      if (server.discoveredTools?.some(tool =>
        tool.name?.toLowerCase().includes(term) ||
        tool.description?.toLowerCase().includes(term)
      )) {
        if (server.ref) toExpand.add(server.ref);
      }
    });
    if (toExpand.size > 0) {
      setExpandedServers(prev => new Set([...prev, ...toExpand]));
    }
  }, [searchTerm, servers]);

  // Filter servers based on search term
  const filteredServers = useMemo(() => {
    if (!searchTerm) return servers;
    const term = searchTerm.toLowerCase();
    return servers.filter(server => {
      const matchesRef = server.ref?.toLowerCase().includes(term);
      const matchesTools = server.discoveredTools?.some(tool =>
        tool.name?.toLowerCase().includes(term) ||
        tool.description?.toLowerCase().includes(term)
      );
      return matchesRef || matchesTools;
    });
  }, [servers, searchTerm]);

  // Helper to highlight search term in text
  const highlightMatch = (text: string | undefined | null, highlight: string) => {
    if (!text || !highlight) return text;
    const parts = text.split(new RegExp(`(${highlight})`, 'gi'));
    return parts.map((part, i) =>
      part.toLowerCase() === highlight.toLowerCase()
        ? <mark key={i} className="bg-yellow-200 px-0 py-0 rounded">{part}</mark>
        : part
    );
  };

  // Fetch servers
  const fetchServers = async () => {
    try {
      setIsLoading(true);

      const serversResponse = await getServers();
      if (!serversResponse.error && serversResponse.data) {
        const sortedServers = [...serversResponse.data].sort((a, b) => {
          return (a.ref || '').localeCompare(b.ref || '');
        });
        setServers(sortedServers);

        // Start with all servers collapsed
        setExpandedServers(new Set());
      } else {
        console.error("Failed to fetch servers:", serversResponse);
        toast.error(serversResponse.error || "Failed to fetch servers data.");
      }
    } catch (error) {
      console.error("Error fetching servers:", error);
      toast.error("An error occurred while fetching servers.");
    } finally {
      setIsLoading(false);
    }
  };

  const fetchToolServerTypes = async () => {
    try {
      setIsLoading(true);

      const toolServerTypes = await getToolServerTypes();
      if (!toolServerTypes.error && toolServerTypes.data) {
        setToolServerTypes(toolServerTypes.data);
      } else {
        console.error("Failed to fetch tool server types:", toolServerTypes);
        toast.error(toolServerTypes.error || "Failed to fetch tool server types.");
      }
    } catch (error) {
      console.error("Error fetching supported tool server types:", error);
      toast.error("An error occurred while fetching supported tool server types.");
    } finally {
      setIsLoading(false);
    }
  }

  // Handle server deletion
  const handleDeleteServer = async (serverName: string) => {
    try {
      setIsLoading(true);

      const response = await deleteServer(serverName);

      if (!response.error) {
        toast.success("Server deleted successfully");
        await fetchServers();
        await refreshTools();
      } else {
        toast.error(response.error || "Failed to delete server");
      }
    } catch (error) {
      console.error("Error deleting server:", error);
      toast.error("Failed to delete server");
    } finally {
      setIsLoading(false);
      setShowConfirmDelete(null);
    }
  };

  // Handle adding a new server
  const handleAddServer = async (serverRequest: ToolServerCreateRequest) => {
    try {
      setIsLoading(true);

      const response = await createServer(serverRequest);

      if (response.error) {
        throw new Error(response.error || "Failed to add server");
      }

      toast.success("Server added successfully");
      setShowAddServer(false);
      await fetchServers();
      await refreshTools();
    } catch (error) {
      console.error("Error adding server:", error);
      const errorMessage = error instanceof Error ? error.message : "Unknown error";
      toast.error(`Failed to add server: ${errorMessage}`);
      throw error; // Re-throw to be caught by the dialog
    } finally {
      setIsLoading(false);
    }
  };

  const toggleServer = (serverName: string) => {
    setExpandedServers(prev => {
      const newSet = new Set(prev);
      if (newSet.has(serverName)) {
        newSet.delete(serverName);
      } else {
        newSet.add(serverName);
      }
      return newSet;
    });
  };

  return (
    <div className="mt-12 mx-auto max-w-6xl px-6 pb-12">
      <div className="flex justify-between items-center mb-6">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">MCP Servers</h1>
          <Link href="/tools" className="text-blue-600 hover:text-blue-800 text-sm">
            View Tools →
          </Link>
        </div>
        {servers.length > 0 && (
          <Button onClick={() => setShowAddServer(true)} variant="default">
            <Plus className="h-4 w-4 mr-2" />
            Add MCP Server
          </Button>
        )}
      </div>

      {/* Search bar */}
      <div className="relative flex-1 mb-4">
        <Search className="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Search servers by name or tool..."
          value={searchTerm}
          onChange={(e) => setSearchTerm(e.target.value)}
          className="pl-10"
        />
      </div>

      {/* Result count */}
      <div className="flex justify-end items-center mb-4">
        <div className="text-sm text-muted-foreground">
          {filteredServers.length} server{filteredServers.length !== 1 ? "s" : ""} found
        </div>
      </div>

      {isLoading ? (
        <div className="flex flex-col items-center justify-center h-[200px] border rounded-lg bg-secondary/5">
          <div className="animate-pulse h-6 w-6 rounded-full bg-primary/10 mb-4"></div>
          <p className="text-muted-foreground">Loading servers...</p>
        </div>
      ) : filteredServers.length === 0 && servers.length > 0 ? (
        <div className="flex flex-col items-center justify-center h-[300px] text-center p-4 border rounded-lg bg-secondary/5">
          <Server className="h-12 w-12 text-muted-foreground mb-4 opacity-20" />
          <h3 className="font-medium text-lg">No servers found</h3>
          <p className="text-muted-foreground mt-1 mb-4">
            Try adjusting your search to find servers.
          </p>
          <Button variant="outline" onClick={() => setSearchTerm("")}>
            Clear Search
          </Button>
        </div>
      ) : filteredServers.length > 0 ? (
        <ScrollArea className="h-[calc(100vh-350px)] pr-4 -mr-4">
          <div className="space-y-4">
            {filteredServers.map((server) => {
              if (!server.ref) return null;
              const serverName: string = server.ref;
              const isExpanded = expandedServers.has(serverName);

              return (
                <div key={server.ref} className="border rounded-md overflow-hidden">
                  {/* Server Header */}
                  <div className="bg-secondary/10 p-4">
                    <div className="flex items-center justify-between">
                      <div
                        className="flex items-center gap-3 cursor-pointer"
                        onClick={() => toggleServer(serverName)}
                      >
                        {isExpanded ? <ChevronDown className="h-5 w-5" /> : <ChevronRight className="h-5 w-5" />}
                        <div className="flex items-center gap-2">
                          <div>
                            <div className="font-medium">{highlightMatch(server.ref, searchTerm)}</div>
                            <div className="text-xs text-muted-foreground flex items-center gap-2">
                              <span className="font-mono">{highlightMatch(server.ref, searchTerm)}</span>
                            </div>
                          </div>
                        </div>
                      </div>

                      <div className="flex items-center gap-2">
                        <DropdownMenu
                          open={openDropdownMenu === serverName}
                          onOpenChange={(isOpen) => setOpenDropdownMenu(isOpen ? serverName : null)}
                        >
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8">
                              <MoreHorizontal className="h-4 w-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                             <DropdownMenuItem
                               className="text-red-600 focus:text-red-700 focus:bg-red-50"
                               onSelect={(e) => {
                                 e.preventDefault();
                                 setOpenDropdownMenu(null);
                                 setShowConfirmDelete(serverName);
                               }}
                             >
                               <Trash2 className="h-4 w-4 mr-2" />
                               Remove MCP Server
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </div>
                  </div>

                  {/* Server Tools List */}
                  {isExpanded && (
                    <div className="p-4">
                      {server.discoveredTools && server.discoveredTools.length > 0 ? (
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                          {server.discoveredTools
                            .sort((a, b) => {
                              const aName = a.name || "";
                              const bName = b.name || "";
                              return aName.localeCompare(bName);
                            })
                            .map((tool) => (
                              <div key={tool.name} className="p-3 border rounded-md hover:bg-secondary/5 transition-colors">
                                <div className="flex items-start gap-2">
                                  <FunctionSquare className="h-4 w-4 text-blue-500 mt-0.5" />
                                  <div>
                                    <div className="font-medium text-sm">{highlightMatch(tool.name, searchTerm)}</div>
                                    <div className="text-xs text-muted-foreground mt-1">{highlightMatch(tool.description, searchTerm)}</div>
                                  </div>
                                </div>
                              </div>
                            ))}
                        </div>
                      ) : (
                        <div className="text-center p-4 text-sm text-muted-foreground">No tools available for this MCP server.</div>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </ScrollArea>
      ) : (
        <div className="flex flex-col items-center justify-center h-[300px] text-center p-4 border rounded-lg bg-secondary/5">
          <Server className="h-12 w-12 text-muted-foreground mb-4 opacity-20" />
          <h3 className="font-medium text-lg">No MCP servers connected</h3>
          <p className="text-muted-foreground mt-1 mb-4">Add an MCP server to discover and use tools.</p>
          <Button onClick={() => setShowAddServer(true)} variant="default">
            <Plus className="h-4 w-4 mr-2" />
            Add MCP Server
          </Button>
        </div>
      )}

      {/* Add server dialog */}
      <AddServerDialog
        open={showAddServer}
        supportedToolServerTypes={toolServerTypes}
        onOpenChange={setShowAddServer}
        onAddServer={handleAddServer}
      />

      {/* Confirm delete dialog */}
      <ConfirmDialog
        open={showConfirmDelete !== null}
        onOpenChange={(open) => {
          if (!open) {
            setShowConfirmDelete(null);
          }
        }}
        title="Delete MCP Server"
        description="Are you sure you want to delete this MCP server? This will also delete all associated tools and cannot be undone."
        onConfirm={() => showConfirmDelete !== null && handleDeleteServer(showConfirmDelete)}
      />
    </div>
  );
}
