"use client";

import { useState, useEffect } from "react";
import { AlertCircle, Server, Globe, Terminal, Trash2, ChevronDown, ChevronRight, MoreHorizontal, Plus, FunctionSquare } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { getToolDescription, getToolDisplayName, getToolIdentifier, isCommandMcpTool } from "@/lib/data";
import { MCPServer, MCPServerConfig, Tool } from "@/types/datamodel";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { createServer, deleteServer, getServers, refreshServerTools } from "../actions/servers";
import { AddServerDialog } from "@/components/AddServerDialog";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { getTools } from "../actions/tools";
import Link from "next/link";

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
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [tools, setTools] = useState<Tool[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [expandedServers, setExpandedServers] = useState<Set<number>>(new Set());

  // Dialog states
  const [showAddServer, setShowAddServer] = useState(false);
  const [showConfirmDelete, setShowConfirmDelete] = useState<number | null>(null);

  // Fetch data on component mount
  useEffect(() => {
    fetchData();
  }, []);

  // Fetch servers and tools
  const fetchData = async () => {
    try {
      setIsLoading(true);
      setError(null);

      // Fetch servers
      const serversResponse = await getServers();
      if (serversResponse.success && serversResponse.data) {
        setServers(serversResponse.data);
        setExpandedServers(new Set(serversResponse.data.map((server) => server.id).filter((id) => id !== undefined) as number[]));
      } else {
        console.error("Failed to fetch servers:", serversResponse);
        setError(serversResponse.error || "Failed to fetch servers data.");
      }

      // Fetch tools for server association
      const toolsResponse = await getTools();
      if (toolsResponse.success && toolsResponse.data) {
        setTools(toolsResponse.data);
      }
    } catch (error) {
      console.error("Error fetching data:", error);
      setError("An error occurred while fetching data.");
    } finally {
      setIsLoading(false);
    }
  };

  // Toggle server expansion
  const toggleServerExpansion = (serverId: number) => {
    setExpandedServers((prev) => {
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
        fetchData();
      } else {
        setError(response.message || "Failed to refresh server");
      }
    } catch (error) {
      console.error("Error refreshing server:", error);
      setError(`Failed to refresh server: ${error instanceof Error ? error.message : "Unknown error"}`);
    } finally {
      setIsRefreshing(null);
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

      if (response.success) {
        setSuccess("Server deleted successfully");
        fetchData();
      } else {
        setError(response.error || "Failed to delete server");
      }
    } catch (error) {
      console.error("Error deleting server:", error);
      setError(`Failed to delete server: ${error instanceof Error ? error.message : "Unknown error"}`);
    } finally {
      setIsLoading(false);
      setShowConfirmDelete(null);
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

      const response = await createServer(server);

      if (!response.success) {
        throw new Error(response.error || "Failed to add server");
      }

      setSuccess("Server added successfully");
      setShowAddServer(false);
      fetchData();
    } catch (error) {
      console.error("Error adding server:", error);
      setError(`Failed to add server: ${error instanceof Error ? error.message : "Unknown error"}`);
    } finally {
      setIsLoading(false);
      setTimeout(() => {
        setError(null);
        setSuccess(null);
      }, 5000);
    }
  };

  // Group tools by server
  const toolsByServer: Record<number, Tool[]> = {};
  servers.forEach((server) => {
    if (server.id) {
      toolsByServer[server.id] = tools.filter((tool) => tool.server_id === server.id);
    }
  });

  return (
    <div className="mt-12 mx-auto max-w-6xl px-6">
      <div className="flex justify-between items-center mb-6">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">MCP Servers</h1>
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

            return (
              <div key={server.id} className="border rounded-md overflow-hidden">
                {/* Server Header */}
                <div className="bg-secondary/10 p-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3 cursor-pointer" onClick={() => toggleServerExpansion(serverId)}>
                      {expandedServers.has(serverId) ? <ChevronDown className="h-5 w-5" /> : <ChevronRight className="h-5 w-5" />}
                      <div className="flex items-center gap-2">
                        {isCommandMcpTool(server) ? <Terminal className="h-5 w-5 text-violet-500" /> : <Globe className="h-5 w-5 text-green-500" />}
                        <div>
                          <div className="font-medium">{server.component.label || server.component.provider}</div>
                          <div className="text-xs text-muted-foreground flex items-center gap-2">
                            <span className="font-mono">{server.component.config.name}</span>
                            <Badge variant="outline" className="bg-blue-50 text-blue-700">
                              {(toolsByServer[serverId] || []).length} tool{(toolsByServer[serverId] || []).length !== 1 ? "s" : ""}
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
          <p className="text-muted-foreground mt-1 mb-4">Add an MCP server to discover and use tools.</p>
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
