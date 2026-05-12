"use client";

import { RunHistoryEntry } from "@/types";
import { Badge } from "@/components/ui/badge";
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

function formatDuration(startTime: string, completionTime?: string): string {
  if (!completionTime) return "-";
  const start = new Date(startTime).getTime();
  const end = new Date(completionTime).getTime();
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

function formatDateTime(dateStr: string): string {
  try {
    return new Date(dateStr).toLocaleString();
  } catch {
    return dateStr;
  }
}

function statusVariant(status: string): "default" | "destructive" | "secondary" | "outline" {
  switch (status) {
    case "Succeeded":
      return "default";
    case "Failed":
      return "destructive";
    case "Running":
      return "secondary";
    default:
      return "outline";
  }
}

function statusClassName(status: string): string {
  switch (status) {
    case "Succeeded":
      return "bg-green-600 hover:bg-green-600/80 text-white";
    case "Failed":
      return "bg-red-600 hover:bg-red-600/80 text-white";
    case "Running":
      return "bg-yellow-600 hover:bg-yellow-600/80 text-white";
    default:
      return "";
  }
}

export function RunHistoryTable({ entries, agentNamespace, agentName }: RunHistoryTableProps) {
  if (!entries || entries.length === 0) {
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
    <Table>
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
        {sorted.map((entry, index) => (
          <TableRow key={`${entry.startTime}-${index}`}>
            <TableCell className="whitespace-nowrap">{formatDateTime(entry.startTime)}</TableCell>
            <TableCell className="whitespace-nowrap">
              {entry.completionTime ? formatDateTime(entry.completionTime) : "-"}
            </TableCell>
            <TableCell className="whitespace-nowrap">
              {formatDuration(entry.startTime, entry.completionTime)}
            </TableCell>
            <TableCell>
              <Badge
                variant={statusVariant(entry.status)}
                className={statusClassName(entry.status)}
              >
                {entry.status}
              </Badge>
            </TableCell>
            <TableCell className="font-mono text-xs">
              {entry.sessionId ? (
                agentNamespace && agentName ? (
                  <a
                    href={`/agents/${agentNamespace}/${agentName}/chat/${entry.sessionId}`}
                    className="text-blue-500 hover:underline"
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
            <TableCell className="max-w-xs truncate" title={entry.message}>
              {entry.message || "-"}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
