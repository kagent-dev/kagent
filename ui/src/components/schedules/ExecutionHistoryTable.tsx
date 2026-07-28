"use client";

import { useEffect, useState } from "react";
import { ScheduledRunExecution } from "@/types";
import { Badge } from "@/components/ui/badge";
import { formatDateTime } from "@/lib/formatDateTime";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

interface ExecutionHistoryTableProps {
  executions: ScheduledRunExecution[];
  targetNamespace?: string;
  targetName?: string;
}

function formatDuration(execution: ScheduledRunExecution, now: number): string {
  if (!execution.completionTime && execution.status !== "InProgress") return "-";
  const { startTime } = execution;
  const start = new Date(startTime).getTime();
  const end = execution.completionTime ? new Date(execution.completionTime).getTime() : now;
  if (Number.isNaN(start) || Number.isNaN(end)) return "-";
  const diffMs = end - start;
  if (diffMs < 0) return "-";

  const seconds = Math.floor(diffMs / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  if (minutes < 60) return `${minutes}m ${remainingSeconds}s`;
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return `${hours}h ${remainingMinutes}m`;
}

function statusBadge(execution: ScheduledRunExecution): { label: string; variant: "default" | "destructive" | "secondary" | "outline"; className: string } {
  switch (execution.status) {
    case "DispatchFailed":
      return { label: "Dispatch Failed", variant: "destructive", className: "bg-red-600 hover:bg-red-600/80 text-white" };
    case "Succeeded":
      return { label: "Succeeded", variant: "default", className: "bg-green-600 hover:bg-green-600/80 text-white" };
    case "Failed":
      return { label: "Failed", variant: "destructive", className: "bg-red-600 hover:bg-red-600/80 text-white" };
    case "TimedOut":
      return { label: "Timed Out", variant: "destructive", className: "bg-amber-600 hover:bg-amber-600/80 text-white" };
    case "InProgress":
      return { label: "Running", variant: "secondary", className: "bg-blue-500 hover:bg-blue-500/80 text-white" };
    default:
      return { label: "Unknown", variant: "outline", className: "" };
  }
}

function SessionIdCell({ sessionID, targetNamespace, targetName }: {
  sessionID?: string;
  targetNamespace?: string;
  targetName?: string;
}) {
  if (!sessionID) return <>-</>;
  const shortID = `${sessionID.slice(0, 8)}...`;
  if (!targetNamespace || !targetName) {
    return <span title={sessionID}>{shortID}</span>;
  }
  const href = `/agents/${encodeURIComponent(targetNamespace)}/${encodeURIComponent(targetName)}/chat/${encodeURIComponent(sessionID)}`;
  return (
    <a
      href={href}
      className="text-blue-500 hover:underline"
      aria-label={`Open session ${sessionID}`}
      title={sessionID}
    >
      {shortID}
    </a>
  );
}

export function ExecutionHistoryTable({ executions, targetNamespace, targetName }: ExecutionHistoryTableProps) {
  const [now, setNow] = useState(() => Date.now());
  const hasInProgressExecution = executions.some((execution) => execution.status === "InProgress");

  useEffect(() => {
    if (!hasInProgressExecution) return;
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, [hasInProgressExecution]);

  if (executions.length === 0) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        No executions yet
      </div>
    );
  }

  const sorted = [...executions].sort(
    (a, b) => new Date(b.startTime).getTime() - new Date(a.startTime).getTime()
  );

  return (
    <Table className="min-w-[960px]">
      <TableHeader>
        <TableRow>
          <TableHead>Start Time</TableHead>
          <TableHead>Completion Time</TableHead>
          <TableHead>Duration</TableHead>
          <TableHead>Trigger</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Session ID</TableHead>
          <TableHead>Status Details</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {sorted.map((execution) => {
          const badge = statusBadge(execution);
          const statusMessage = execution.statusMessage || "";
          return (
            <TableRow key={execution.id}>
              <TableCell className="whitespace-nowrap">{formatDateTime(execution.startTime)}</TableCell>
              <TableCell className="whitespace-nowrap">
                {execution.completionTime ? formatDateTime(execution.completionTime) : "-"}
              </TableCell>
              <TableCell className="whitespace-nowrap">
                {formatDuration(execution, now)}
              </TableCell>
              <TableCell>{execution.trigger}</TableCell>
              <TableCell>
                <Badge variant={badge.variant} className={badge.className}>
                  {badge.label}
                </Badge>
              </TableCell>
              <TableCell className="font-mono text-xs">
                <SessionIdCell
                  sessionID={execution.sessionID}
                  targetNamespace={targetNamespace}
                  targetName={targetName}
                />
              </TableCell>
              <TableCell className="max-w-xs truncate" title={statusMessage}>
                {statusMessage || "-"}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
