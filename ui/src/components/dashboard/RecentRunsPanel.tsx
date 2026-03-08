"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { RecentRun } from "@/types";
import Link from "next/link";

interface RecentRunsPanelProps {
  runs: RecentRun[];
}

function formatRelativeTime(dateStr: string): string {
  const now = new Date();
  const date = new Date(dateStr);
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 1) return "just now";
  if (diffMins < 60) return `${diffMins}m ago`;
  const diffHours = Math.floor(diffMins / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.floor(diffHours / 24);
  return `${diffDays}d ago`;
}

export function RecentRunsPanel({ runs }: RecentRunsPanelProps) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-sm font-medium">Recent Runs</CardTitle>
        <Link href="/agents" className="text-xs text-muted-foreground hover:text-foreground">
          View all &rarr;
        </Link>
      </CardHeader>
      <CardContent>
        {runs.length === 0 ? (
          <p className="text-sm text-muted-foreground">No recent runs</p>
        ) : (
          <ScrollArea className="h-[300px]">
            <div className="space-y-3">
              {runs.map((run) => (
                <div key={run.sessionId} className="flex items-center justify-between">
                  <div>
                    <p className="text-sm font-medium">{run.agentName}</p>
                    <p className="text-xs text-muted-foreground">{run.sessionName}</p>
                  </div>
                  <p className="text-xs text-muted-foreground">{formatRelativeTime(run.updatedAt)}</p>
                </div>
              ))}
            </div>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  );
}
