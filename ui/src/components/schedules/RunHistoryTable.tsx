"use client";

import { RunHistoryEntry } from "@/types";
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

interface RunHistoryTableProps {
  entries: RunHistoryEntry[];
  agentNamespace?: string;
  agentName?: string;
}

function formatDuration(startTime: string, endTime?: string): string {
  if (!endTime) return "-";
  const start = new Date(startTime).getTime();
  const end = new Date(endTime).getTime();
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

function statusBadge(entry: RunHistoryEntry): { label: string; variant: "default" | "destructive" | "secondary" | "outline"; className: string } {
  switch (entry.status) {
    case "DispatchFailed":
      return { label: "Dispatch Failed", variant: "destructive", className: "bg-red-600 hover:bg-red-600/80 text-white" };
    case "Succeeded":
      return { label: "Succeeded", variant: "default", className: "bg-green-600 hover:bg-green-600/80 text-white" };
    case "Failed":
      return { label: "Failed", variant: "destructive", className: "bg-red-600 hover:bg-red-600/80 text-white" };
    case "Timeout":
      return { label: "Timeout", variant: "destructive", className: "bg-amber-600 hover:bg-amber-600/80 text-white" };
    case "InProgress":
      return { label: "Running", variant: "secondary", className: "bg-blue-500 hover:bg-blue-500/80 text-white" };
    default:
      return { label: "Unknown", variant: "outline", className: "" };
  }
}

export function RunHistoryTable({ entries, agentNamespace, agentName }: RunHistoryTableProps) {
  if (entries.length === 0) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        No runs yet
      </div>
    );
  }

  const sorted = [...entries].sort(
    (a, b) => new Date(b.startTime).getTime() - new Date(a.startTime).getTime()
  );

  return (
    <Table className="min-w-[860px]">
      <TableHeader>
        <TableRow>
          <TableHead>Start Time</TableHead>
          <TableHead>Completion Time</TableHead>
          <TableHead>Duration</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Session ID</TableHead>
          <TableHead>Message</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {sorted.map((entry, index) => {
          const badge = statusBadge(entry);
          const message = entry.message || "";
          return (
            <TableRow key={`${entry.startTime}-${index}`}>
              <TableCell className="whitespace-nowrap">{formatDateTime(entry.startTime)}</TableCell>
              <TableCell className="whitespace-nowrap">
                {entry.endTime ? formatDateTime(entry.endTime) : "-"}
              </TableCell>
              <TableCell className="whitespace-nowrap">
                {formatDuration(entry.startTime, entry.endTime)}
              </TableCell>
              <TableCell>
                <Badge variant={badge.variant} className={badge.className}>
                  {badge.label}
                </Badge>
              </TableCell>
              <TableCell className="font-mono text-xs">
                {entry.sessionId ? (
                  agentNamespace && agentName ? (
                    <a
                      href={`/agents/${encodeURIComponent(agentNamespace)}/${encodeURIComponent(agentName)}/chat/${encodeURIComponent(entry.sessionId)}`}
                      className="text-blue-500 hover:underline"
                      aria-label={`Open session ${entry.sessionId}`}
                      title={entry.sessionId}
                    >
                      {entry.sessionId.slice(0, 8)}...
                    </a>
                  ) : (
                    <span title={entry.sessionId}>{entry.sessionId.slice(0, 8)}...</span>
                  )
                ) : (
                  "-"
                )}
              </TableCell>
              <TableCell className="max-w-xs truncate" title={message}>
                {message || "-"}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
