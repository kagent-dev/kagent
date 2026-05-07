"use client";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AgentGrid } from "@/components/AgentGrid";
import { AgentListView } from "@/components/AgentListView";
import { Plus, LayoutGrid, List } from "lucide-react";
import KagentLogo from "@/components/kagent-logo";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { ErrorState } from "./ErrorState";
import { Button } from "./ui/button";
import { LoadingState } from "./LoadingState";
import { useAgents } from "./AgentsProvider";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { cn } from "@/lib/utils";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

const AGENTS_VIEW_KEY = "kagent-agents-view";
const ALL_NAMESPACES = "__all__";
type AgentsView = "grid" | "list";

function readStoredView(): AgentsView {
  if (typeof window === "undefined") {
    return "grid";
  }
  const v = window.localStorage.getItem(AGENTS_VIEW_KEY);
  return v === "list" ? "list" : "grid";
}

export default function AgentList() {
  const { agents, loading, error } = useAgents();
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [view, setView] = useState<AgentsView>("grid");
  const namespaceFilter = searchParams.get("namespace") || ALL_NAMESPACES;

  useEffect(() => {
    const id = requestAnimationFrame(() => {
      setView(readStoredView());
    });
    return () => cancelAnimationFrame(id);
  }, []);

  const setViewAndPersist = useCallback((next: AgentsView) => {
    setView(next);
    try {
      window.localStorage.setItem(AGENTS_VIEW_KEY, next);
    } catch {
      // ignore private mode / quota
    }
  }, []);

  const onNamespaceChange = useCallback(
    (namespace: string) => {
      const q = new URLSearchParams(searchParams.toString());
      if (namespace === ALL_NAMESPACES) {
        q.delete("namespace");
      } else {
        q.set("namespace", namespace);
      }

      const query = q.toString();
      router.replace(query ? `${pathname}?${query}` : pathname, { scroll: false });
    },
    [pathname, router, searchParams],
  );

  const namespaces = useMemo(() => {
    const uniqueNamespaces = new Set(
      (agents || []).map((item) => item.agent.metadata.namespace || "").filter(Boolean),
    );
    return Array.from(uniqueNamespaces).sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }));
  }, [agents]);

  const filteredAgents = useMemo(() => {
    if (!agents || namespaceFilter === ALL_NAMESPACES) {
      return agents || [];
    }

    return agents.filter((item) => (item.agent.metadata.namespace || "") === namespaceFilter);
  }, [agents, namespaceFilter]);

  if (error) {
    return <ErrorState message={error} />;
  }

  if (loading) {
    return <LoadingState />;
  }

  return (
    <AppPageFrame ariaLabelledBy="agents-page-title" mainClassName="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <PageHeader
        titleId="agents-page-title"
        title="Agents"
        className="mb-8"
        end={
          agents && agents.length > 0 ? (
            <div className="flex w-full min-w-0 flex-col gap-3 sm:w-auto sm:flex-row sm:items-center">
              <div className="w-full sm:w-56">
                <label className="sr-only" htmlFor="agents-namespace-filter">
                  Namespace
                </label>
                <Select value={namespaceFilter} onValueChange={onNamespaceChange}>
                  <SelectTrigger id="agents-namespace-filter" aria-label="Namespace filter">
                    <SelectValue placeholder="All namespaces" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={ALL_NAMESPACES}>All namespaces</SelectItem>
                    {namespaces.map((namespace) => (
                      <SelectItem key={namespace} value={namespace}>
                        {namespace}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div
                className="flex w-full min-w-0 items-center justify-end gap-1 rounded-lg border border-border/60 bg-muted/20 p-1"
                role="group"
                aria-label="Layout"
              >
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className={cn(
                    "h-8 gap-1.5 px-2.5 text-muted-foreground",
                    view === "grid" && "bg-card text-foreground shadow-sm",
                  )}
                  aria-pressed={view === "grid"}
                  aria-label="Show agents as cards"
                  onClick={() => setViewAndPersist("grid")}
                >
                  <LayoutGrid className="h-4 w-4 shrink-0" aria-hidden />
                  <span className="hidden sm:inline" aria-hidden>
                    Cards
                  </span>
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className={cn(
                    "h-8 gap-1.5 px-2.5 text-muted-foreground",
                    view === "list" && "bg-card text-foreground shadow-sm",
                  )}
                  aria-pressed={view === "list"}
                  aria-label="Show agents as a list"
                  onClick={() => setViewAndPersist("list")}
                >
                  <List className="h-4 w-4 shrink-0" aria-hidden />
                  <span className="hidden sm:inline" aria-hidden>
                    List
                  </span>
                </Button>
              </div>
            </div>
          ) : null
        }
      />

      {agents?.length === 0 ? (
        <div className="rounded-xl border border-border/60 bg-card/30 py-12 text-center shadow-sm">
          <KagentLogo className="mx-auto mb-4 h-16 w-16" />
          <h2 className="mb-2 text-lg font-medium tracking-tight">No agents yet</h2>
          <p className="mb-6 text-pretty text-sm text-muted-foreground">Create an agent to run it in your cluster and wire models and tools in one place.</p>
          <Button asChild size="lg" className="min-w-[12rem]">
            <Link href="/agents/new">
              <Plus className="mr-2 h-4 w-4" aria-hidden />
              New Agent
            </Link>
          </Button>
        </div>
      ) : filteredAgents.length === 0 ? (
        <div className="rounded-xl border border-border/60 bg-card/30 py-12 text-center shadow-sm">
          <KagentLogo className="mx-auto mb-4 h-16 w-16" />
          <h2 className="mb-2 text-lg font-medium tracking-tight">No agents in this namespace</h2>
          <p className="mb-6 text-pretty text-sm text-muted-foreground">
            No agents match the <span className="font-mono">{namespaceFilter}</span> namespace filter.
          </p>
          <Button type="button" variant="outline" onClick={() => onNamespaceChange(ALL_NAMESPACES)}>
            Show all namespaces
          </Button>
        </div>
      ) : view === "list" ? (
        <AgentListView agentResponse={filteredAgents} />
      ) : (
        <AgentGrid agentResponse={filteredAgents} />
      )}
    </AppPageFrame>
  );
}
