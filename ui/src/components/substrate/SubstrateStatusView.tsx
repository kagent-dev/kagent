"use client";

import { useCallback, useMemo, type ComponentType } from "react";
import Link from "next/link";
import { RefreshCw, AlertCircle, Cpu, FileStack, Users, Boxes } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import type {
  SubstrateActorEntry,
  SubstrateActorTemplateEntry,
  SubstrateStatusResponse,
  SubstrateWorkerEntry,
  SubstrateWorkerPoolEntry,
} from "@/types";
import { cn } from "@/lib/utils";

export const SUBSTRATE_REFRESH_INTERVAL_OPTIONS = [
  { label: "Off", valueMs: 0 },
  { label: "2 seconds", valueMs: 2000 },
  { label: "5 seconds", valueMs: 5000 },
  { label: "10 seconds", valueMs: 10000 },
  { label: "30 seconds", valueMs: 30000 },
] as const;

type SubstrateStatusViewProps = {
  status: SubstrateStatusResponse | null;
  namespace: string;
  onNamespaceChange: (ns: string) => void;
  isLoading: boolean;
  loadError: string | null;
  onRefresh: () => Promise<void>;
  refreshIntervalMs: number;
  onRefreshIntervalChange: (intervalMs: number) => void;
};

function statusTone(label: string): "ok" | "warn" | "idle" | "busy" | "neutral" {
  const s = label.toLowerCase();
  if (s === "ready" || s === "running") return "ok";
  if (s === "failed" || s === "suspending") return "warn";
  if (s === "suspended" || s === "unknown" || s === "") return "idle";
  if (s.includes("resume") || s.includes("wait") || s.includes("golden")) return "busy";
  return "neutral";
}

function StatusChip({ label }: { label: string }) {
  const tone = statusTone(label);
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-sm border px-2 py-0.5 text-[11px] font-medium uppercase tracking-wide",
        tone === "ok" && "border-emerald-600/30 bg-emerald-500/10 text-emerald-800 dark:text-emerald-200",
        tone === "warn" && "border-amber-600/35 bg-amber-500/12 text-amber-900 dark:text-amber-100",
        tone === "busy" && "border-sky-600/30 bg-sky-500/10 text-sky-900 dark:text-sky-100",
        tone === "idle" && "border-border bg-muted/60 text-muted-foreground",
        tone === "neutral" && "border-border bg-background text-foreground",
      )}
    >
      {label || "—"}
    </span>
  );
}

function SectionHeader({
  icon: Icon,
  title,
  count,
  hint,
}: {
  icon: ComponentType<{ className?: string }>;
  title: string;
  count: number;
  hint?: string;
}) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-2 border-b border-border/80 pb-3">
      <div className="flex items-center gap-2.5">
        <Icon className="h-4 w-4 text-[hsl(28_72%_42%)]" aria-hidden />
        <h2 className="text-sm font-semibold tracking-tight">{title}</h2>
        <span className="tabular-nums text-xs text-muted-foreground">{count}</span>
      </div>
      {hint ? <p className="text-xs text-muted-foreground max-w-md text-right">{hint}</p> : null}
    </div>
  );
}

function EmptyRow({ message }: { message: string }) {
  return (
    <p className="py-8 text-sm text-muted-foreground border-b border-dashed border-border/60">{message}</p>
  );
}

function WorkerPoolsTable({ rows }: { rows: SubstrateWorkerPoolEntry[] }) {
  if (rows.length === 0) {
    return <EmptyRow message="No WorkerPools in this scope. Create one in the cluster or via Helm." />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wider text-muted-foreground">
            <th className="py-2 pr-4 font-medium">Pool</th>
            <th className="py-2 pr-4 font-medium">Replicas</th>
            <th className="py-2 font-medium">Ateom image</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border/70">
          {rows.map((wp) => (
            <tr key={`${wp.namespace}/${wp.name}`} className="align-top">
              <td className="py-3 pr-4 font-medium">
                <span className="text-muted-foreground">{wp.namespace}/</span>
                {wp.name}
              </td>
              <td className="py-3 pr-4 tabular-nums">{wp.replicas}</td>
              <td className="py-3 font-mono text-xs break-all text-muted-foreground">{wp.ateomImage}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ActorTemplatesTable({ rows }: { rows: SubstrateActorTemplateEntry[] }) {
  if (rows.length === 0) {
    return (
      <EmptyRow message="No ActorTemplates yet. They appear when you create a Substrate Agent Harness or a Sandbox workload agent with platform=substrate." />
    );
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wider text-muted-foreground">
            <th className="py-2 pr-4 font-medium">Template</th>
            <th className="py-2 pr-4 font-medium">Phase</th>
            <th className="py-2 pr-4 font-medium">Worker pool</th>
            <th className="py-2 font-medium">Harness</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border/70">
          {rows.map((t) => (
            <tr key={`${t.namespace}/${t.name}`}>
              <td className="py-3 pr-4">
                <div className="font-medium">
                  <span className="text-muted-foreground">{t.namespace}/</span>
                  {t.name}
                </div>
                {t.goldenActorId ? (
                  <div className="mt-1 font-mono text-[11px] text-muted-foreground">golden: {t.goldenActorId}</div>
                ) : null}
              </td>
              <td className="py-3 pr-4">
                <StatusChip label={t.phase ?? ""} />
              </td>
              <td className="py-3 pr-4 text-muted-foreground">{t.workerPoolRef ?? "—"}</td>
              <td className="py-3">
                {t.harnessName ? (
                  <Link
                    href={`/agents?namespace=${encodeURIComponent(t.namespace)}`}
                    className="text-[hsl(28_72%_38%)] underline-offset-2 hover:underline dark:text-[hsl(32_70%_62%)]"
                  >
                    {t.harnessName}
                  </Link>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ActorsTable({ rows, enabled }: { rows: SubstrateActorEntry[]; enabled: boolean }) {
  if (!enabled) {
    return (
      <EmptyRow message="ate-api is not configured on the controller. Set substrate-ate-api-endpoint to see live actors." />
    );
  }
  if (rows.length === 0) {
    return <EmptyRow message="No actors in ate-api for this namespace." />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wider text-muted-foreground">
            <th className="py-2 pr-4 font-medium">Actor</th>
            <th className="py-2 pr-4 font-medium">Status</th>
            <th className="py-2 pr-4 font-medium">Template</th>
            <th className="py-2 font-medium">Worker pod</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border/70">
          {rows.map((a) => (
            <tr key={a.actorId}>
              <td className="py-3 pr-4 font-mono text-xs">{a.actorId}</td>
              <td className="py-3 pr-4">
                <StatusChip label={a.status} />
              </td>
              <td className="py-3 pr-4 text-muted-foreground">
                {a.actorTemplateNamespace && a.actorTemplateName
                  ? `${a.actorTemplateNamespace}/${a.actorTemplateName}`
                  : "—"}
              </td>
              <td className="py-3 font-mono text-xs text-muted-foreground">
                {a.ateomPodName ? `${a.ateomPodNamespace ?? ""}/${a.ateomPodName}` : "—"}
                {a.ateomPodIp ? ` · ${a.ateomPodIp}` : ""}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function WorkersTable({ rows, enabled }: { rows: SubstrateWorkerEntry[]; enabled: boolean }) {
  if (!enabled) {
    return <EmptyRow message="Worker assignments require ate-api." />;
  }
  if (rows.length === 0) {
    return <EmptyRow message="No worker assignments reported by ate-api." />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wider text-muted-foreground">
            <th className="py-2 pr-4 font-medium">Pod</th>
            <th className="py-2 pr-4 font-medium">Pool</th>
            <th className="py-2 font-medium">Actor</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border/70">
          {rows.map((w) => (
            <tr key={`${w.workerNamespace}/${w.workerPool}/${w.workerPod}`}>
              <td className="py-3 pr-4 font-mono text-xs">
                {w.workerNamespace}/{w.workerPod}
              </td>
              <td className="py-3 pr-4">{w.workerPool}</td>
              <td className="py-3 font-mono text-xs text-muted-foreground">{w.actorId || "idle"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function formatRefreshIntervalLabel(intervalMs: number): string {
  if (intervalMs <= 0) return "Off";
  const match = SUBSTRATE_REFRESH_INTERVAL_OPTIONS.find((o) => o.valueMs === intervalMs);
  if (match) return match.label;
  if (intervalMs % 1000 === 0) return `${intervalMs / 1000} seconds`;
  return `${intervalMs} ms`;
}

export function SubstrateStatusView({
  status,
  namespace,
  onNamespaceChange,
  isLoading,
  loadError,
  onRefresh,
  refreshIntervalMs,
  onRefreshIntervalChange,
}: SubstrateStatusViewProps) {
  const summary = useMemo(() => {
    if (!status) return null;
    const running = status.actors.filter((a) => a.status.toLowerCase() === "running").length;
    const readyTemplates = status.actorTemplates.filter((t) => t.phase?.toLowerCase() === "ready").length;
    return {
      pools: status.workerPools.length,
      templates: status.actorTemplates.length,
      readyTemplates,
      actors: status.actors.length,
      running,
      workers: status.workers.length,
      busyWorkers: status.workers.filter((w) => w.actorId).length,
    };
  }, [status]);

  const handleRefresh = useCallback(() => {
    void onRefresh();
  }, [onRefresh]);

  const refreshSelectValue = String(refreshIntervalMs);
  const hasPresetInterval = SUBSTRATE_REFRESH_INTERVAL_OPTIONS.some((o) => o.valueMs === refreshIntervalMs);

  return (
    <div className="space-y-10">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="max-w-xs w-full">
          <label htmlFor="substrate-ns" className="text-xs font-medium text-muted-foreground mb-1.5 block">
            Namespace
          </label>
          <NamespaceCombobox
            id="substrate-ns"
            value={namespace}
            onValueChange={onNamespaceChange}
            includeAllNamespaces
            autoSelectDefault={false}
            allNamespacesLabel="All watched namespaces"
            placeholder="All watched namespaces"
          />
        </div>
        <div className="flex flex-wrap items-end gap-2 shrink-0">
          <div className="w-[9.5rem]">
            <label htmlFor="substrate-refresh-interval" className="text-xs font-medium text-muted-foreground mb-1.5 block">
              Auto-refresh
            </label>
            <Select
              value={refreshSelectValue}
              onValueChange={(value) => onRefreshIntervalChange(Number(value))}
            >
              <SelectTrigger id="substrate-refresh-interval" className="h-9">
                <SelectValue placeholder="Auto-refresh">{formatRefreshIntervalLabel(refreshIntervalMs)}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {SUBSTRATE_REFRESH_INTERVAL_OPTIONS.map((option) => (
                  <SelectItem key={option.valueMs} value={String(option.valueMs)}>
                    {option.label}
                  </SelectItem>
                ))}
                {!hasPresetInterval ? (
                  <SelectItem value={refreshSelectValue}>{formatRefreshIntervalLabel(refreshIntervalMs)}</SelectItem>
                ) : null}
              </SelectContent>
            </Select>
          </div>
          <Button type="button" variant="outline" size="sm" onClick={handleRefresh} disabled={isLoading} className="gap-2 h-9">
            <RefreshCw className={cn("h-4 w-4", isLoading && "animate-spin")} />
            Refresh
          </Button>
        </div>
      </div>

      {loadError ? (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Could not load substrate status</AlertTitle>
          <AlertDescription>{loadError}</AlertDescription>
        </Alert>
      ) : null}

      {status?.ateApiError ? (
        <Alert>
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>ate-api partial data</AlertTitle>
          <AlertDescription>{status.ateApiError}</AlertDescription>
        </Alert>
      ) : null}

      {summary ? (
        <dl className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-px rounded-lg overflow-hidden border border-border/80 bg-border/80">
          {[
            { label: "Worker pools", value: summary.pools },
            { label: "Templates ready", value: `${summary.readyTemplates}/${summary.templates}` },
            { label: "Actors running", value: `${summary.running}/${summary.actors}` },
            { label: "Workers busy", value: `${summary.busyWorkers}/${summary.workers}` },
            { label: "ate-api", value: status?.enabled ? "connected" : "off" },
            { label: "Scope", value: namespace || "all" },
          ].map((item) => (
            <div key={item.label} className="bg-background px-4 py-3">
              <dt className="text-[10px] uppercase tracking-widest text-muted-foreground">{item.label}</dt>
              <dd className="mt-1 text-lg font-semibold tabular-nums tracking-tight">{item.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}

      <section className="space-y-4">
        <SectionHeader icon={Boxes} title="Worker pools" count={status?.workerPools.length ?? 0} hint="Kubernetes WorkerPool CRs" />
        <WorkerPoolsTable rows={status?.workerPools ?? []} />
      </section>

      <section className="space-y-4">
        <SectionHeader
          icon={FileStack}
          title="Actor templates"
          count={status?.actorTemplates.length ?? 0}
          hint="Golden snapshots and harness-owned templates"
        />
        <ActorTemplatesTable rows={status?.actorTemplates ?? []} />
      </section>

      <section className="space-y-4">
        <SectionHeader icon={Users} title="Actors" count={status?.actors.length ?? 0} hint="Live state from ate-api" />
        <ActorsTable rows={status?.actors ?? []} enabled={status?.enabled ?? false} />
      </section>

      <section className="space-y-4">
        <SectionHeader icon={Cpu} title="Workers" count={status?.workers.length ?? 0} hint="ateom pod assignments" />
        <WorkersTable rows={status?.workers ?? []} enabled={status?.enabled ?? false} />
      </section>
    </div>
  );
}
