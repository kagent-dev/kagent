"use client";

import { useState, useEffect } from "react";
import { Server, Globe, Trash2, ChevronDown, ChevronRight, MoreHorizontal, Plus, FunctionSquare } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { getToolDescription, getToolDisplayName, getToolIdentifier } from "@/lib/data";
import { ToolServer, Tool, ToolServerConfig, Component } from "@/types/datamodel";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { createServer, deleteServer, getServers, refreshServerTools, getServerTools } from "../actions/servers";
import { AddServerDialog } from "@/components/AddServerDialog";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import Link from "next/link";
import { toast } from "sonner";

// Format date function
const formatDate = (dateString: string | null): string => {
  if (!dateString) return "Never";

  try {
    const date = new Date(dateString);
    return new Intl.DateTimeFormat("en-US", {
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    }).format(date);
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
  } catch (e) {
    return "Invalid date";
  }
};

export default function ServersPage() {
  // State for servers and tools
  const [servers, setServers] = useState<ToolServer[]>([]);
  const [serverTools, setServerTools] = useState<Record<number, Tool[]>>({});
  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState<number | null>(null);
  const [expandedServers, setExpandedServers] = useState<Set<number>>(new Set());
  const [loadingServerTools, setLoadingServerTools] = useState<Set<number>>(new Set());

  // Dialog states
  const [showAddServer, setShowAddServer] = useState(false);
  const [showConfirmDelete, setShowConfirmDelete] = useState<number | null>(null);

  // Fetch data on component mount
  useEffect(() => {
    fetchServers();
  }, []);

  // Fetch servers
  const fetchServers = async () => {
    try {
      setIsLoading(true);

      // Fetch servers
      const serversResponse = await getServers();
      if (serversResponse.success && serversResponse.data) {
        setServers(serversResponse.data);

        // Initially expand all servers
        const serverIds = serversResponse.data.map((server) => server.id).filter((id): id is number => id !== undefined);

        setExpandedServers(new Set(serverIds));
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

  // Fetch tools for a specific server when expanded
  const fetchServerTools = async (serverId: number) => {
    if (serverTools[serverId] || !expandedServers.has(serverId)) return;

    try {
      setLoadingServerTools((prev) => new Set([...prev, serverId]));

      const response = await getServerTools(serverId);

      if (response.success && response.data) {
        setServerTools((prev) => ({
          ...prev,
          [serverId]: response.data || [],
        }));
      } else {
        console.error(`Failed to fetch tools for server ${serverId}:`, response);
        toast.error(response.error || `Failed to fetch tools for server ${serverId}`);
      }
    } catch (error) {
      console.error(`Error fetching tools for server ${serverId}:`, error);
      toast.error(`Failed to fetch tools for server ${serverId}`);
    } finally {
      setLoadingServerTools((prev) => {
        const newSet = new Set(prev);
        newSet.delete(serverId);
        return newSet;
      });
    }
  };

  // Effect to load tools when a server is expanded
  useEffect(() => {
    expandedServers.forEach((serverId) => {
      fetchServerTools(serverId);
    });
  }, [expandedServers]);

  // Toggle server expansion
  const toggleServerExpansion = (serverId: number) => {
    setExpandedServers((prev) => {
      const newSet = new Set(prev);
      if (newSet.has(serverId)) {
        newSet.delete(serverId);
      } else {
        newSet.add(serverId);
        // Fetch tools for this server if we haven't already
        fetchServerTools(serverId);
      }
      return newSet;
    });
  };

  // Handle server refresh
  const handleRefreshServer = async (serverId: number) => {
    try {
      setIsRefreshing(serverId);

      const response = await refreshServerTools(serverId);

      if (response) {
        toast.success("Tools refreshed successfully");

        // Refresh the tools for this specific server
        const toolsResponse = await getServerTools(serverId);
        if (toolsResponse.success && toolsResponse.data) {
          setServerTools((prev) => ({
            ...prev,
            [serverId]: toolsResponse.data || [],
          }));
        }
      } else {
        toast.error("Failed to refresh tools");
      }
    } catch (error) {
      console.error("Error refreshing server:", error);
      toast.error("Failed to refresh server");
    } finally {
      setIsRefreshing(null);
    }
  };

  // Handle server deletion
  const handleDeleteServer = async (serverId: number) => {
    try {
      setIsLoading(true);

      const response = await deleteServer(serverId);

      if (response.success) {
        toast.success("Server deleted successfully");

        // Remove the server tools from state
        setServerTools((prev) => {
          const newServerTools = { ...prev };
          delete newServerTools[serverId];
          return newServerTools;
        });

        // Refresh servers list
        fetchServers();
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
  const handleAddServer = async (server: Component<ToolServerConfig>) => {
    try {
      setIsLoading(true);

      const response = await createServer(server);

      if (!response.success) {
        throw new Error(response.error || "Failed to add server");
      }

      toast.success("Server added successfully");
      setShowAddServer(false);
      fetchServers();
    } catch (error) {
      console.error("Error adding server:", error);
      toast.error(`Failed to add server: ${error instanceof Error ? error.message : "Unknown error"}`);
    } finally {
      setIsLoading(false);
    }
  };

  // Get tool count for a server
  const getToolCount = (serverId: number): number => {
    return serverTools[serverId]?.length || 0;
  };

  return (
    <div className="mt-12 mx-auto max-w-6xl px-6">
      <div className="flex justify-between items-center mb-6">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">Tool Servers</h1>
          <Link href="/tools" className="text-blue-600 hover:text-blue-800 text-sm">
            View Tools Library →
          </Link>
        </div>
        {servers.length > 0 && (
          <Button onClick={() => setShowAddServer(true)} className="border-blue-500 text-blue-600 hover:bg-blue-50" variant="outline">
            <Plus className="h-4 w-4 mr-2" />
            Add Server
          </Button>
        )}
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
            const serverId: number = server.id;
            const isExpanded = expandedServers.has(serverId);
            const isLoadingTools = loadingServerTools.has(serverId);
            const toolCount = getToolCount(serverId);

            return (
              <div key={server.id} className="border rounded-md overflow-hidden">
                {/* Server Header */}
                <div className="bg-secondary/10 p-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3 cursor-pointer" onClick={() => toggleServerExpansion(serverId)}>
                      {isExpanded ? <ChevronDown className="h-5 w-5" /> : <ChevronRight className="h-5 w-5" />}
                      <div className="flex items-center gap-2">
                        <Globe className="h-5 w-5 text-green-500" />
                        <div>
                          <div className="font-medium">{server.component.label || server.component.provider}</div>
                          <div className="text-xs text-muted-foreground flex items-center gap-2">
                            <span className="font-mono">{server.component.label}</span>
                            <Badge variant="outline" className="bg-blue-50 text-blue-700">
                              {isExpanded ? `${toolCount} tool${toolCount !== 1 ? "s" : ""}` : ""}
                            </Badge>
                            {server.last_connected && <span className="text-xs text-muted-foreground">Last updated: {formatDate(server.last_connected)}</span>}
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
                        disabled={isRefreshing === serverId}
                      >
                        {isRefreshing === serverId ? (
                          <>
                            <div className="h-4 w-4 rounded-full border-2 border-blue-600 border-t-transparent animate-spin mr-2" />
                            Refreshing...
                          </>
                        ) : (
                          "Refresh"
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
                {isExpanded && (
                  <div className="p-4">
                    {isLoadingTools ? (
                      <div className="flex justify-center items-center p-4">
                        <div className="h-5 w-5 rounded-full border-2 border-blue-600 border-t-transparent animate-spin mr-2" />
                        <span className="text-sm text-muted-foreground">Loading tools...</span>
                      </div>
                    ) : serverTools[serverId]?.length > 0 ? (
                      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                        {serverTools[serverId]
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
                          <Button variant="outline" size="sm" onClick={() => handleRefreshServer(serverId)} disabled={isRefreshing === serverId} className="text-blue-600">
                            Refresh to discover tools
                          </Button>
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      ) : (
        <div className="flex flex-col items-center justify-center h-[300px] text-center p-4 border rounded-lg bg-secondary/5">
          <Server className="h-12 w-12 text-muted-foreground mb-4 opacity-20" />
          <h3 className="font-medium text-lg">No servers connected</h3>
          <p className="text-muted-foreground mt-1 mb-4">Add a tool server to discover and use tools.</p>
          <Button onClick={() => setShowAddServer(true)} className="bg-blue-500 hover:bg-blue-600 text-white">
            <Plus className="h-4 w-4 mr-2" />
            Add Server
          </Button>
        </div>
      )}

      {/* Add server dialog */}
      <AddServerDialog open={showAddServer} onOpenChange={setShowAddServer} onAddServer={handleAddServer} />

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
