"use client";

import { useCallback, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import type { Agent, AgentResponse } from "@/types";
import { DeleteButton } from "@/components/DeleteAgentButton";
import { MemoriesDialog } from "@/components/MemoriesDialog";
import KagentLogo from "@/components/kagent-logo";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { countAgentToolBindings } from "@/lib/countAgentTools";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { cn } from "@/lib/utils";
import { ArrowDown, ArrowUp, Brain, MoreHorizontal, Pencil, Trash2 } from "lucide-react";

interface AgentListViewProps {
  agentResponse: AgentResponse[];
}

type SortKey = "name" | "type" | "providerModel" | "toolCount" | "skillsCount" | "state";
type SortDir = "asc" | "desc";

function countSkills(agent: Agent): number {
  const s = agent.spec?.skills;
  const refs = s?.refs?.length ?? 0;
  const gits = s?.gitRefs?.length ?? 0;
  return refs + gits;
}

function compareNumbers(a: number, b: number, dir: SortDir): number {
  const d = a - b;
  return dir === "asc" ? d : -d;
}

function typeLabel(type: string | undefined): string {
  switch (type) {
    case "BYO":
      return "BYO";
    case "Sandbox":
      return "Sandbox";
    case "Declarative":
    default:
      return "Declarative";
  }
}

function getStatusInfo(accepted: boolean, deploymentReady: boolean) {
  if (!accepted) {
    return {
      message: "Not accepted",
      className: "bg-red-500/15 text-red-700 dark:text-red-400",
      rank: 0,
    };
  }
  if (!deploymentReady) {
    return {
      message: "Not ready",
      className: "bg-amber-500/20 text-amber-900 dark:text-amber-200",
      rank: 1,
    };
  }
  return {
    message: "Ready",
    className: "bg-primary/10 text-primary",
    rank: 2,
  };
}

function providerModelText(item: AgentResponse): string {
  const { agent, model, modelProvider } = item;
  const isBYO = agent.spec?.type === "BYO";
  const byoImage = isBYO ? agent.spec?.byo?.deployment?.image : undefined;
  if (isBYO) {
    return byoImage ? byoImage : "—";
  }
  if (!model && !modelProvider) {
    return "—";
  }
  return `${modelProvider} · ${model}`;
}

function ProviderModelCell({ item }: { item: AgentResponse }) {
  const { agent, model, modelProvider } = item;
  const isBYO = agent.spec?.type === "BYO";
  const byoImage = isBYO ? agent.spec?.byo?.deployment?.image : undefined;
  if (isBYO) {
    return (
      <div className="flex min-w-0 max-w-xs flex-col gap-1">
        <span className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">Image</span>
        <span className="text-sm [overflow-wrap:anywhere] text-muted-foreground">{byoImage || "—"}</span>
      </div>
    );
  }
  if (!model && !modelProvider) {
    return <span className="text-sm text-muted-foreground">—</span>;
  }
  return (
    <div className="flex min-w-0 max-w-xs flex-col gap-0.5">
      <span className="text-xs font-medium text-foreground [overflow-wrap:anywhere]">{modelProvider || "—"}</span>
      <span className="text-xs text-muted-foreground [overflow-wrap:anywhere]">{model || "—"}</span>
    </div>
  );
}

function compareStrings(a: string, b: string, dir: SortDir): number {
  const c = a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" });
  return dir === "asc" ? c : -c;
}

function sortAgents(items: AgentResponse[], key: SortKey, dir: SortDir): AgentResponse[] {
  return [...items].sort((A, B) => {
    const a = A.agent;
    const b = B.agent;
    switch (key) {
      case "name": {
        const s = (x: Agent) => x.metadata.name || "";
        return compareStrings(s(a), s(b), dir);
      }
      case "type": {
        return compareStrings(typeLabel(a.spec?.type), typeLabel(b.spec?.type), dir);
      }
      case "providerModel": {
        return compareStrings(providerModelText(A), providerModelText(B), dir);
      }
      case "toolCount": {
        return compareNumbers(countAgentToolBindings(A), countAgentToolBindings(B), dir);
      }
      case "skillsCount": {
        return compareNumbers(countSkills(a), countSkills(b), dir);
      }
      case "state": {
        const ra = getStatusInfo(A.accepted, A.deploymentReady).rank;
        const rb = getStatusInfo(B.accepted, B.deploymentReady).rank;
        if (ra !== rb) {
          return dir === "asc" ? ra - rb : rb - ra;
        }
        return compareStrings(
          (a.metadata.name || "") + (a.metadata.namespace || ""),
          (b.metadata.name || "") + (b.metadata.namespace || ""),
          "asc",
        );
      }
      default:
        return 0;
    }
  });
}

type SortableThProps = {
  col: SortKey;
  label: string;
  className?: string;
  textAlign?: "left" | "right";
  sort: { key: SortKey; dir: SortDir };
  onSort: (col: SortKey) => void;
};

function SortableTh({ col, label, className, textAlign = "left", sort, onSort }: SortableThProps) {
  const active = sort.key === col;
  return (
    <th
      scope="col"
      className={cn(
        "align-middle border-b border-border/50 bg-muted/20 px-3 py-2.5 text-left font-medium first:pl-4 last:pr-4",
        textAlign === "right" && "text-right",
        className,
      )}
      aria-sort={active ? (sort.dir === "asc" ? "ascending" : "descending") : "none"}
    >
      <button
        type="button"
        onClick={() => onSort(col)}
        className={cn(
          "inline-flex w-full max-w-full items-center gap-1.5 text-[10px] font-semibold uppercase tracking-[0.1em] text-muted-foreground transition-colors hover:text-foreground",
          textAlign === "right" && "justify-end",
        )}
      >
        <span className="truncate">{label}</span>
        {active ? (
          sort.dir === "asc" ? (
            <ArrowUp className="h-3.5 w-3.5 shrink-0 text-primary" aria-hidden />
          ) : (
            <ArrowDown className="h-3.5 w-3.5 shrink-0 text-primary" aria-hidden />
          )
        ) : (
          <span className="inline-flex w-3.5 shrink-0" aria-hidden />
        )}
      </button>
    </th>
  );
}

function AgentListRow({ item }: { item: AgentResponse }) {
  const { agent, deploymentReady, accepted } = item;
  const router = useRouter();
  const [memoriesOpen, setMemoriesOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  const name = agent.metadata.name || "";
  const namespace = agent.metadata.namespace || "";
  const isReady = accepted && deploymentReady;
  const status = getStatusInfo(accepted, deploymentReady);
  const providerTitle = providerModelText(item);
  const nTools = countAgentToolBindings(item);
  const nSkills = countSkills(agent);

  const chatPath = `/agents/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/chat`;
  const goChat = useCallback(() => {
    if (isReady) {
      router.push(chatPath);
    }
  }, [chatPath, isReady, router]);

  const handleEdit = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    router.push(`/agents/new?edit=true&name=${name}&namespace=${namespace}`);
  };

  const trBody = (
    <tr
      className={cn(
        "group border-b border-border/30 transition-colors [contain:paint]",
        isReady ? "cursor-pointer hover:bg-muted/45" : "hover:bg-muted/20",
      )}
      onClick={goChat}
      onAuxClick={(e) => {
        if (isReady && e.button === 1) {
          e.preventDefault();
          window.open(chatPath, "_blank", "noopener,noreferrer");
        }
      }}
      onKeyDown={(e) => {
        if (isReady && (e.key === "Enter" || e.key === " ")) {
          e.preventDefault();
          goChat();
        }
      }}
      tabIndex={isReady ? 0 : -1}
      role={isReady ? "link" : undefined}
      aria-label={isReady ? `Open chat for ${k8sRefUtils.toRef(namespace, name)}` : undefined}
    >
      <td className="relative px-3 py-3.5 pl-4 align-top [overflow-wrap:anywhere] first:pl-4">
        <div
          className="absolute bottom-0 left-0 top-0 w-px bg-border/80 group-hover:bg-primary/60"
          aria-hidden
        />
        <div className="pl-1.5">
          <div className="flex min-w-0 items-center gap-2">
            <KagentLogo className="h-4 w-4 shrink-0 opacity-80" />
            <span className="font-medium text-foreground">{name || "—"}</span>
          </div>
          {agent.spec?.description ? (
            <p className="mt-1 line-clamp-2 max-w-xl text-pretty text-xs text-muted-foreground sm:line-clamp-1">
              {agent.spec.description}
            </p>
          ) : null}
        </div>
      </td>
      <td className="px-3 py-3.5 align-middle text-sm text-foreground" title="Agent type">
        {typeLabel(agent.spec?.type)}
      </td>
      <td className="px-3 py-3.5 align-middle" title={providerTitle}>
        <ProviderModelCell item={item} />
      </td>
      <td
        className="whitespace-nowrap px-3 py-3.5 align-middle text-right text-sm tabular-nums text-muted-foreground"
        title="MCP/Agent tool bindings: number of tool names (or 1 per server if all tools)"
      >
        {nTools}
      </td>
      <td
        className="whitespace-nowrap px-3 py-3.5 align-middle text-right text-sm tabular-nums text-muted-foreground"
        title="Skill sources (image refs and Git repos)"
      >
        {nSkills}
      </td>
      <td className="px-3 py-3.5 align-middle">
        <span className={cn("inline-flex rounded-md px-2.5 py-0.5 text-xs font-medium", status.className)}>
          {status.message}
        </span>
      </td>
      <td className="w-10 px-1 py-3.5 align-middle" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-end">
          <DropdownMenu>
            <DropdownMenuTrigger asChild onClick={(e) => e.stopPropagation()}>
              <Button type="button" variant="ghost" size="icon" className="h-8 w-8" aria-label="Agent options">
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
              <DropdownMenuItem onClick={handleEdit} className="cursor-pointer">
                <Pencil className="mr-2 h-4 w-4" />
                Edit
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  setMemoriesOpen(true);
                }}
                className="cursor-pointer"
              >
                <Brain className="mr-2 h-4 w-4" />
                View memories
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  setDeleteOpen(true);
                }}
                className="cursor-pointer text-red-500 focus:text-red-500"
              >
                <Trash2 className="mr-2 h-4 w-4" />
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <DeleteButton
          agentName={name}
          namespace={namespace}
          externalOpen={deleteOpen}
          onExternalOpenChange={setDeleteOpen}
        />
        <MemoriesDialog
          agentName={name}
          namespace={namespace}
          open={memoriesOpen}
          onOpenChange={setMemoriesOpen}
        />
      </td>
    </tr>
  );

  return trBody;
}

export function AgentListView({ agentResponse }: AgentListViewProps) {
  const [sort, setSort] = useState<{ key: SortKey; dir: SortDir }>({ key: "name", dir: "asc" });

  const onSort = useCallback((col: SortKey) => {
    setSort((prev) => {
      if (prev.key !== col) {
        return { key: col, dir: "asc" };
      }
      return { key: col, dir: prev.dir === "asc" ? "desc" : "asc" };
    });
  }, []);

  const sorted = useMemo(
    () => sortAgents(agentResponse, sort.key, sort.dir),
    [agentResponse, sort.key, sort.dir],
  );

  if (agentResponse.length === 0) {
    return null;
  }

  return (
    <div className="overflow-x-auto rounded-xl border border-border/60 bg-card/15 shadow-sm">
      <table className="w-full min-w-[44rem] border-separate border-spacing-0 text-sm" role="table" aria-label="Agents">
        <caption className="sr-only">Agents; click a column header to sort.</caption>
        <thead>
          <tr>
            <SortableTh col="name" label="Name" sort={sort} onSort={onSort} />
            <SortableTh col="type" label="Type" sort={sort} onSort={onSort} />
            <SortableTh col="providerModel" label="Provider / Model" sort={sort} onSort={onSort} />
            <SortableTh col="toolCount" label="Tools" textAlign="right" sort={sort} onSort={onSort} />
            <SortableTh col="skillsCount" label="Skills" textAlign="right" sort={sort} onSort={onSort} />
            <SortableTh col="state" label="State" sort={sort} onSort={onSort} />
            <th
              scope="col"
              className="w-10 align-middle border-b border-border/50 bg-muted/20 px-1 py-2.5 last:pr-3"
            >
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((item) => {
            const key = k8sRefUtils.toRef(
              item.agent.metadata.namespace || "",
              item.agent.metadata.name || "",
            );
            return <AgentListRow key={key} item={item} />;
          })}
        </tbody>
      </table>
    </div>
  );
}