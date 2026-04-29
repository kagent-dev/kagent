"use client";

import { useState, useMemo, useCallback, useEffect } from "react";
import Link from "next/link";
import {
  Server,
  Search,
  Trash2,
  ChevronDown,
  ChevronRight,
  MoreHorizontal,
  Plus,
  FunctionSquare,
  AlertCircle,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { ToolServerResponse, DiscoveredTool } from "@/types";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { deleteServer } from "@/app/actions/servers";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { toast } from "sonner";
import { useAgents } from "@/components/AgentsProvider";
import { getDiscoveredToolDescription, getDiscoveredToolDisplayName } from "@/lib/toolUtils";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

function setsEqualString(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) {
    return false;
  }
  for (const x of a) {
    if (!b.has(x)) {
      return false;
    }
  }
  return true;
}

type McpServersViewProps = {
  servers: ToolServerResponse[];
  isLoading: boolean;
  loadError: string | null;
  onRefresh: () => Promise<void>;
};

type DisplayServer = {
  server: ToolServerResponse;
  /** When searching, may be a subset of discovered tools. */
  tools: DiscoveredTool[];
};

/**
 * One screen: search MCP tool servers, expand to see tools, add/remove connections.
 */
export function McpServersView({ servers, isLoading, loadError, onRefresh }: McpServersViewProps) {
  const { refreshTools } = useAgents();
  const [searchQuery, setSearchQuery] = useState("");
  const [expandedServers, setExpandedServers] = useState<Set<string>>(new Set());
  const [showConfirmDelete, setShowConfirmDelete] = useState<string | null>(null);
  const [openDropdownMenu, setOpenDropdownMenu] = useState<string | null>(null);

  const q = searchQuery.trim().toLowerCase();

  const displayList = useMemo((): DisplayServer[] => {
    if (!q) {
      return servers
        .filter((s) => s.ref)
        .map((server) => ({
          server,
          tools: server.discoveredTools || [],
        }));
    }
    const out: DisplayServer[] = [];
    for (const s of servers) {
      if (!s.ref) {
        continue;
      }
      const refLower = s.ref.toLowerCase();
      const allTools = s.discoveredTools || [];
      if (refLower.includes(q)) {
        out.push({ server: s, tools: allTools });
        continue;
      }
      const tools = allTools.filter((t) => {
        const n = getDiscoveredToolDisplayName(t).toLowerCase();
        const d = (getDiscoveredToolDescription(t) || "").toLowerCase();
        const raw = (t.name || "").toLowerCase();
        return n.includes(q) || d.includes(q) || raw.includes(q);
      });
      if (tools.length) {
        out.push({ server: s, tools });
      }
    }
    return out;
  }, [servers, q]);

  // When search is active, expand every server that has matches so the list is scannable
  const displayRefs = useMemo(
    () => new Set(displayList.map((d) => d.server.ref).filter((r): r is string => Boolean(r))),
    [displayList],
  );

  useEffect(() => {
    if (!q) {
      return;
    }
    // Defer so this isn’t a synchronous setState in the effect body (react-hooks/set-state-in-effect).
    const id = requestAnimationFrame(() => {
      setExpandedServers((prev) => {
        if (setsEqualString(prev, displayRefs)) {
          return prev;
        }
        return new Set(displayRefs);
      });
    });
    return () => cancelAnimationFrame(id);
  }, [q, displayRefs]);

  const handleDeleteServer = async (serverName: string) => {
    const response = await deleteServer(serverName);
    if (!response.error) {
      toast.success("Server removed");
      await onRefresh();
      await refreshTools();
    } else {
      toast.error(response.error || "Failed to delete server");
    }
    setShowConfirmDelete(null);
  };

  const toggleServer = useCallback((serverName: string) => {
    setExpandedServers((prev) => {
      const newSet = new Set(prev);
      if (newSet.has(serverName)) {
        newSet.delete(serverName);
      } else {
        newSet.add(serverName);
      }
      return newSet;
    });
  }, []);

  const highlight = useCallback(
    (text: string | undefined | null) => {
      if (!text || !q) {
        return text;
      }
      const t = String(text);
      const lower = t.toLowerCase();
      const i = lower.indexOf(q);
      if (i < 0) {
        return t;
      }
      return (
        <>
          {t.slice(0, i)}
          <mark className="rounded bg-primary/20 px-0.5 text-foreground">{t.slice(i, i + q.length)}</mark>
          {t.slice(i + q.length)}
        </>
      );
    },
    [q],
  );

  if (isLoading) {
    return (
      <div
        className="flex h-[200px] flex-col items-center justify-center rounded-xl border border-border/60 bg-card/20"
        role="status"
        aria-live="polite"
        aria-busy="true"
      >
        <div className="mb-4 h-6 w-6 animate-pulse rounded-full bg-primary/10" />
        <p className="text-sm text-muted-foreground">Loading…</p>
      </div>
    );
  }

  const toolCount = displayList.reduce((n, d) => n + d.tools.length, 0);

  return (
    <div>
      {loadError ? (
        <Alert variant="destructive" className="mb-4">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{loadError}</AlertDescription>
        </Alert>
      ) : null}

      <div className="mb-4 flex flex-col gap-3 sm:mb-6 sm:flex-row sm:items-center sm:gap-3">
        <div className="relative min-w-0 flex-1">
          <label htmlFor="mcp-search" className="sr-only">
            Search servers and tools
          </label>
          <Search
            className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden
          />
          <Input
            id="mcp-search"
            name="mcpSearch"
            type="search"
            autoComplete="off"
            placeholder="Filter by server ref, tool name, or description…"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-10"
          />
        </div>
        <Button asChild className="w-full shrink-0 sm:w-auto" size="lg">
          <Link href="/mcp/new" className="inline-flex w-full sm:w-auto">
            <Plus className="mr-2 h-4 w-4" aria-hidden />
            Add server
          </Link>
        </Button>
      </div>

      <p className="mb-4 text-end text-sm text-muted-foreground" role="status" aria-live="polite">
        {displayList.length} server{displayList.length !== 1 ? "s" : ""} · {toolCount} tool{toolCount !== 1 ? "s" : ""}
        {q ? " (filtered)" : ""}
      </p>

      {displayList.length > 0 ? (
        <ul className="list-none space-y-4" aria-label="MCP tool servers">
          {displayList.map(({ server, tools: rowTools }) => {
            if (!server.ref) {
              return null;
            }
            const serverName: string = server.ref;
            const isExpanded = expandedServers.has(serverName);
            return (
              <li key={server.ref} className="overflow-hidden rounded-xl border border-border/60 bg-card/30 shadow-sm">
                <div className="bg-secondary/10 p-4">
                  <div className="flex items-center justify-between">
                    <div
                      className="flex min-w-0 flex-1 cursor-pointer items-center gap-3"
                      onClick={() => toggleServer(serverName)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          toggleServer(serverName);
                        }
                      }}
                      role="button"
                      tabIndex={0}
                      aria-expanded={isExpanded}
                      aria-label={`${isExpanded ? "Collapse" : "Expand"} server ${serverName}, ${rowTools.length} tools`}
                    >
                      {isExpanded ? (
                        <ChevronDown className="h-5 w-5 shrink-0" aria-hidden />
                      ) : (
                        <ChevronRight className="h-5 w-5 shrink-0" aria-hidden />
                      )}
                      <div className="min-w-0 text-left">
                        <div className="font-medium break-words" translate="no">
                          {highlight(server.ref) || server.ref}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {rowTools.length} tool{rowTools.length !== 1 ? "s" : ""}
                        </div>
                      </div>
                    </div>
                    <DropdownMenu
                      open={openDropdownMenu === serverName}
                      onOpenChange={(isOpen) => setOpenDropdownMenu(isOpen ? serverName : null)}
                    >
                      <DropdownMenuTrigger asChild>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          aria-label={`Actions for server ${serverName}`}
                        >
                          <MoreHorizontal className="h-4 w-4" aria-hidden />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem
                          className="text-destructive focus:bg-destructive/10"
                          onSelect={(e) => {
                            e.preventDefault();
                            setOpenDropdownMenu(null);
                            setShowConfirmDelete(serverName);
                          }}
                        >
                          <Trash2 className="mr-2 h-4 w-4" />
                          Remove server
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </div>

                {isExpanded && (
                  <div className="p-4">
                    {rowTools.length > 0 ? (
                      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                        {rowTools
                          .sort((a, b) => (a.name || "").localeCompare(b.name || ""))
                          .map((tool) => {
                            const desc = getDiscoveredToolDescription(tool);
                            const showDesc = desc && desc !== "No description available";
                            return (
                              <div
                                key={tool.name}
                                className="rounded-md border border-border/60 p-3 transition-colors hover:bg-muted/30"
                              >
                                <div className="flex items-start gap-2.5">
                                  <FunctionSquare
                                    className="mt-0.5 size-4 min-h-4 min-w-4 shrink-0 self-start text-primary"
                                    aria-hidden
                                    strokeWidth={2}
                                  />
                                  <div className="min-w-0 flex-1">
                                    <div className="text-sm font-medium" translate="no">
                                      {highlight(getDiscoveredToolDisplayName(tool))}
                                    </div>
                                    {showDesc ? (
                                      <div className="mt-1 line-clamp-3 text-xs text-muted-foreground">
                                        {highlight(desc)}
                                      </div>
                                    ) : null}
                                  </div>
                                </div>
                              </div>
                            );
                          })}
                      </div>
                    ) : (
                      <p className="p-2 text-center text-sm text-muted-foreground">
                        No tools are registered for this server.
                      </p>
                    )}
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      ) : servers.length > 0 ? (
        <div className="flex min-h-[200px] flex-col items-center justify-center rounded-xl border border-dashed border-border/60 bg-card/20 p-8 text-center">
          <p className="text-sm text-muted-foreground">No servers or tools match that filter.</p>
          <Button
            type="button"
            variant="link"
            className="mt-2 h-auto p-0 text-sm"
            onClick={() => setSearchQuery("")}
          >
            Clear search
          </Button>
        </div>
      ) : (
        <div className="flex h-[min(40vh,320px)] flex-col items-center justify-center rounded-xl border border-dashed border-border/60 bg-card/20 p-6 text-center shadow-sm">
          <Server className="mb-4 h-12 w-12 text-muted-foreground opacity-20" aria-hidden />
          <h2 className="text-lg font-medium tracking-tight">No MCP servers yet</h2>
          <p className="mb-4 mt-1 max-w-sm text-pretty text-sm text-muted-foreground">
            Add a tool server to discover the tools it exposes. They will appear in agent pickers after the cluster
            updates.
          </p>
          <Button asChild type="button" size="lg">
            <Link href="/mcp/new" className="inline-flex">
              <Plus className="mr-2 h-4 w-4" aria-hidden />
              Add server
            </Link>
          </Button>
        </div>
      )}

      <ConfirmDialog
        open={showConfirmDelete !== null}
        onOpenChange={(open) => {
          if (!open) {
            setShowConfirmDelete(null);
          }
        }}
        title="Delete MCP server"
        description="This removes the tool server and its tools. Agent tool bindings can break until you update them."
        onConfirm={() => showConfirmDelete !== null && void handleDeleteServer(showConfirmDelete)}
      />
    </div>
  );
}
