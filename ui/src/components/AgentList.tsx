"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { AgentGrid } from "@/components/AgentGrid";
import { AgentListView } from "@/components/AgentListView";
import { Plus, LayoutGrid, List } from "lucide-react";
import KagentLogo from "@/components/kagent-logo";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { ErrorState } from "./ErrorState";
import { Button } from "./ui/button";
import { LoadingState } from "./LoadingState";
import { getAgents } from "@/app/actions/agents";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { cn } from "@/lib/utils";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import type { AgentResponse } from "@/types";

const AGENTS_VIEW_KEY = "kagent-agents-view";
type AgentsView = "grid" | "list";

function readStoredView(): AgentsView {
  if (typeof window === "undefined") {
    return "grid";
  }
  const v = window.localStorage.getItem(AGENTS_VIEW_KEY);
  return v === "list" ? "list" : "grid";
}

export default function AgentList() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const selectedNamespace = searchParams.get("namespace")?.trim() || "";
  const [agents, setAgents] = useState<AgentResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [view, setView] = useState<AgentsView>("grid");
  const latestFetchRequestId = useRef(0);

  const fetchAgents = useCallback(async () => {
    const requestId = latestFetchRequestId.current + 1;
    latestFetchRequestId.current = requestId;

    try {
      setLoading(true);
      const result = await getAgents(selectedNamespace ? { namespace: selectedNamespace } : {});
      if (requestId !== latestFetchRequestId.current) {
        return;
      }
      if (result.error) {
        throw new Error(result.error || "Failed to fetch agents");
      }
      setAgents(result.data || []);
      setError("");
    } catch (err) {
      if (requestId !== latestFetchRequestId.current) {
        return;
      }
      setError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      if (requestId === latestFetchRequestId.current) {
        setLoading(false);
      }
    }
  }, [selectedNamespace]);

  useEffect(() => {
    const id = requestAnimationFrame(() => {
      setView(readStoredView());
    });
    return () => cancelAnimationFrame(id);
  }, []);

  useEffect(() => {
    void fetchAgents();
  }, [fetchAgents]);

  const setViewAndPersist = useCallback((next: AgentsView) => {
    setView(next);
    try {
      window.localStorage.setItem(AGENTS_VIEW_KEY, next);
    } catch {
      // ignore private mode / quota
    }
  }, []);

  const handleNamespaceChange = useCallback(
    (nextNamespace: string) => {
      const namespace = nextNamespace.trim();
      if (!namespace) {
        router.push("/agents");
        return;
      }
      router.push(`/agents?namespace=${encodeURIComponent(namespace)}`);
    },
    [router],
  );

  const createHref = selectedNamespace
    ? `/agents/new?namespace=${encodeURIComponent(selectedNamespace)}`
    : "/agents/new";

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
        description={
          selectedNamespace ? (
            <>
              Showing agents in namespace <code>{selectedNamespace}</code>.
            </>
          ) : (
            "Showing agents across all namespaces."
          )
        }
        className="mb-8"
        end={
          <div className="flex w-full min-w-0 flex-col gap-2 sm:w-auto sm:flex-row sm:items-center sm:justify-end">
            <div className="w-full sm:w-72">
              <NamespaceCombobox
                value={selectedNamespace}
                onValueChange={handleNamespaceChange}
                includeAllNamespaces
                autoSelectDefault={false}
                ariaLabel="Namespace"
                placeholder="All namespaces"
              />
            </div>
            {agents && agents.length > 0 ? (
              <div
                className="flex w-full min-w-0 items-center justify-end gap-1 rounded-lg border border-border/60 bg-muted/20 p-1 sm:w-auto"
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
            ) : null}
          </div>
        }
      />

      {agents?.length === 0 ? (
        <div className="rounded-xl border border-border/60 bg-card/30 py-12 text-center shadow-sm">
          <KagentLogo className="mx-auto mb-4 h-16 w-16" />
          <h2 className="mb-2 text-lg font-medium tracking-tight">
            {selectedNamespace
              ? `No agents found in namespace "${selectedNamespace}".`
              : "No agents yet"}
          </h2>
          <p className="mb-6 text-pretty text-sm text-muted-foreground">
            {selectedNamespace
              ? "Create an agent in this namespace or switch namespaces."
              : "Create an agent to run it in your cluster and wire models and tools in one place."}
          </p>
          <Button asChild size="lg" className="min-w-[12rem]">
            <Link href={createHref}>
              <Plus className="mr-2 h-4 w-4" aria-hidden />
              New Agent
            </Link>
          </Button>
        </div>
      ) : view === "list" ? (
        <AgentListView agentResponse={agents || []} onAgentsChanged={fetchAgents} />
      ) : (
        <AgentGrid agentResponse={agents || []} onAgentsChanged={fetchAgents} />
      )}
    </AppPageFrame>
  );
}
