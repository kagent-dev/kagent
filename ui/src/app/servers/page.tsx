"use client";

import { useState, useEffect } from "react";
import { Server, Globe, Trash2, ChevronDown, ChevronRight, MoreHorizontal, Plus, FunctionSquare } from "lucide-react";
import { Button } from "@/components/ui/button";
import { getToolDescription, getToolDisplayName, getToolIdentifier, isMcpServerUsedByAgents } from "@/lib/toolUtils";
import { ToolServer, ToolServerWithTools, AgentResponse } from "@/types/datamodel";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { createServer, deleteServer, getServers } from "../actions/servers";
import { getTeams } from "../actions/teams";
import { AddServerDialog } from "@/components/AddServerDialog";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import Link from "next/link";
import { toast } from "sonner";

export default function ServersPage() {
  // State for servers and tools
  const [servers, setServers] = useState<ToolServerWithTools[]>([]);
  const [agents, setAgents] = useState<AgentResponse[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [expandedServers, setExpandedServers] = useState<Set<string>>(new Set());

  // Dialog states
  const [showAddServer, setShowAddServer] = useState(false);
  const [showConfirmDelete, setShowConfirmDelete] = useState<string | null>(null);
  const [openDropdownMenu, setOpenDropdownMenu] = useState<string | null>(null);

  // Fetch data on component mount
  useEffect(() => {
    fetchData();
  }, []);

  // Fetch servers and agents
  const fetchData = async () => {
    try {
      setIsLoading(true);

      // Fetch servers
      const serversResponse = await getServers();
      if (serversResponse.success && serversResponse.data) {
        setServers(serversResponse.data);

        // Initially expand all servers
        const serverNames = serversResponse.data.map((server) => server.name).filter((name): name is string => name !== undefined);
        setExpandedServers(new Set(serverNames));
      } else {
        console.error("Failed to fetch servers:", serversResponse);
        toast.error(serversResponse.error || "Failed to fetch servers data.");
      }

      // Fetch agents
      const agentsResponse = await getTeams();
      if (agentsResponse.success && agentsResponse.data) {
        setAgents(agentsResponse.data);
      } else {
        console.error("Failed to fetch agents:", agentsResponse);
        toast.error(agentsResponse.error || "Failed to fetch agents data.");
      }
    } catch (error) {
      console.error("Error fetching data:", error);
      toast.error("An error occurred while fetching data.");
    } finally {
      setIsLoading(false);
    }
  };

  // Handle server deletion
  const handleDeleteServer = async (serverName: string) => {
    try {
      setIsLoading(true);

      const response = await deleteServer(serverName);

      if (response.success) {
        toast.success("Server deleted successfully");
        fetchData();
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

  // Handle server deletion request
  const handleDeleteRequest = (serverName: string) => {
    setShowConfirmDelete(serverName);
  };

  // Handle adding a new server
  const handleAddServer = async (server: ToolServer) => {
    try {
      setIsLoading(true);

      const response = await createServer(server);

      if (!response.success) {
        throw new Error(response.error || "Failed to add server");
      }

      toast.success("Server added successfully");
      setShowAddServer(false);
      fetchData();
    } catch (error) {
      console.error("Error adding server:", error);
      const errorMessage = error instanceof Error ? error.message : "Unknown error";
      toast.error(`Failed to add server: ${errorMessage}`);
      throw error; // Re-throw to be caught by the dialog
    } finally {
      setIsLoading(false);
    }
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
          <Button onClick={() => setShowAddServer(true)} variant="default">
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
            if (!server.name) return null;
            const serverName: string = server.name;
            const isExpanded = expandedServers.has(serverName);

            return (
              <div key={server.name} className="border rounded-md overflow-hidden">
                {/* Server Header */}
                <div className="bg-secondary/10 p-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3 cursor-pointer">
                      {isExpanded ? <ChevronDown className="h-5 w-5" /> : <ChevronRight className="h-5 w-5" />}
                      <div className="flex items-center gap-2">
                        <Globe className="h-5 w-5 text-green-500" />
                        <div>
                          <div className="font-medium">{server.name}</div>
                          <div className="text-xs text-muted-foreground flex items-center gap-2">
                            <span className="font-mono">{server.name}</span>
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
                               handleDeleteRequest(serverName);
                             }}
                           >
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
                    {server.discoveredTools && server.discoveredTools.length > 0 ? (
                      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                        {server.discoveredTools
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
                      <div className="text-center p-4 text-sm text-muted-foreground">No tools available for this server.</div>
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
          <Button onClick={() => setShowAddServer(true)} variant="default">
            <Plus className="h-4 w-4 mr-2" />
            Add Server
          </Button>
        </div>
      )}

      {/* Add server dialog */}
      <AddServerDialog 
        open={showAddServer} 
        onOpenChange={setShowAddServer} 
        onAddServer={handleAddServer}
      />

      {/* Confirm delete dialog */}
      <ConfirmDialog
        open={showConfirmDelete !== null}
        onOpenChange={(open) => !open && setShowConfirmDelete(null)}
        title="Delete Server"
        description={
          <div className="space-y-2">
            <p>Are you sure you want to delete the server &apos;{showConfirmDelete}&apos;? This action cannot be undone.</p>
            {showConfirmDelete && isMcpServerUsedByAgents(showConfirmDelete, agents).length > 0 && (
              <div className="mt-4">
                <p className="font-medium text-amber-600">Warning: This server is currently being used by the following agents:</p>
                <ul className="list-disc list-inside mt-2 space-y-1">
                  {isMcpServerUsedByAgents(showConfirmDelete, agents).map((agent) => (
                    <li key={agent.agent.metadata.name} className="text-sm">
                      {agent.agent.metadata.name}
                    </li>
                  ))}
                </ul>
                <p className="mt-2 text-sm text-muted-foreground">Deleting this server will remove the tools from these agents and may affect their functionality.</p>
              </div>
            )}
          </div>
        }
        confirmLabel="Delete"
        onConfirm={() => showConfirmDelete && handleDeleteServer(showConfirmDelete)}
      />
    </div>
  );
}
