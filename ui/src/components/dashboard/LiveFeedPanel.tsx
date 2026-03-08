"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { RecentEvent } from "@/types";

interface LiveFeedPanelProps {
  events: RecentEvent[];
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

export function LiveFeedPanel({ events }: LiveFeedPanelProps) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <div className="flex items-center gap-2">
          <CardTitle className="text-sm font-medium">Event Stream</CardTitle>
          <span className="h-2 w-2 rounded-full bg-green-500" />
        </div>
        <span className="text-xs text-muted-foreground">{events.length} events</span>
      </CardHeader>
      <CardContent>
        {events.length === 0 ? (
          <p className="text-sm text-muted-foreground">No events</p>
        ) : (
          <ScrollArea className="h-[300px]">
            <div className="space-y-3">
              {events.map((event) => (
                <div key={event.id} className="flex items-center justify-between">
                  <p className="text-sm">{event.summary}</p>
                  <p className="text-xs text-muted-foreground whitespace-nowrap ml-4">{formatRelativeTime(event.createdAt)}</p>
                </div>
              ))}
            </div>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  );
}
