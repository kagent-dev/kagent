"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { Activity, Pause, Play, Trash2 } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

interface FeedEvent {
  agent: string;
  sessionId: string;
  subject: string;
  type: string;
  data: string;
  timestamp: number;
}

const EVENT_TYPE_STYLES: Record<string, { bg: string; text: string }> = {
  token:            { bg: "bg-zinc-100 dark:bg-zinc-800", text: "text-zinc-700 dark:text-zinc-300" },
  tool_start:       { bg: "bg-blue-100 dark:bg-blue-900/40", text: "text-blue-700 dark:text-blue-300" },
  tool_end:         { bg: "bg-green-100 dark:bg-green-900/40", text: "text-green-700 dark:text-green-300" },
  error:            { bg: "bg-red-100 dark:bg-red-900/40", text: "text-red-700 dark:text-red-300" },
  approval_request: { bg: "bg-orange-100 dark:bg-orange-900/40", text: "text-orange-700 dark:text-orange-300" },
  completion:       { bg: "bg-purple-100 dark:bg-purple-900/40", text: "text-purple-700 dark:text-purple-300" },
};

const MAX_EVENTS = 500;

function formatTime(ts: number): string {
  const d = new Date(ts);
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export default function FeedPage() {
  const [events, setEvents] = useState<FeedEvent[]>([]);
  const [status, setStatus] = useState<"connecting" | "connected" | "disconnected">("connecting");
  const [paused, setPaused] = useState(false);
  const [eventCount, setEventCount] = useState(0);
  const [filterTypes, setFilterTypes] = useState<Set<string>>(new Set());
  const pausedRef = useRef(paused);
  const bufferRef = useRef<FeedEvent[]>([]);
  const eventSourceRef = useRef<EventSource | null>(null);

  pausedRef.current = paused;

  const addEvent = useCallback((event: FeedEvent) => {
    setEventCount((c) => c + 1);
    if (pausedRef.current) {
      bufferRef.current.push(event);
      return;
    }
    setEvents((prev) => {
      const next = [event, ...prev];
      return next.length > MAX_EVENTS ? next.slice(0, MAX_EVENTS) : next;
    });
  }, []);

  const handleResume = useCallback(() => {
    setPaused(false);
    if (bufferRef.current.length > 0) {
      setEvents((prev) => {
        const next = [...bufferRef.current.reverse(), ...prev];
        bufferRef.current = [];
        return next.length > MAX_EVENTS ? next.slice(0, MAX_EVENTS) : next;
      });
    }
  }, []);

  const handleClear = useCallback(() => {
    setEvents([]);
    bufferRef.current = [];
    setEventCount(0);
  }, []);

  const toggleFilter = useCallback((type: string) => {
    setFilterTypes((prev) => {
      const next = new Set(prev);
      if (next.has(type)) {
        next.delete(type);
      } else {
        next.add(type);
      }
      return next;
    });
  }, []);

  useEffect(() => {
    let reconnectTimer: NodeJS.Timeout | null = null;

    const connect = () => {
      setStatus("connecting");
      const es = new EventSource("/_p/nats-activity-feed/events");
      eventSourceRef.current = es;

      es.onopen = () => setStatus("connected");

      es.addEventListener("activity", (e) => {
        try {
          const data = JSON.parse(e.data) as FeedEvent;
          addEvent(data);
        } catch {
          // skip malformed events
        }
      });

      es.onerror = () => {
        setStatus("disconnected");
        es.close();
        eventSourceRef.current = null;
        reconnectTimer = setTimeout(connect, 2000);
      };
    };

    connect();

    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
        eventSourceRef.current = null;
      }
      if (reconnectTimer) clearTimeout(reconnectTimer);
    };
  }, [addEvent]);

  const filteredEvents = filterTypes.size > 0
    ? events.filter((e) => !filterTypes.has(e.type))
    : events;

  const allTypes = Array.from(new Set(events.map((e) => e.type)));

  const statusColor = status === "connected" ? "bg-green-500" : status === "connecting" ? "bg-orange-500" : "bg-red-500";

  return (
    <div className="space-y-4 p-6 h-full flex flex-col">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Live Feed</h1>
          <p className="text-muted-foreground">Real-time NATS agent activity stream</p>
        </div>
        <div className="flex items-center gap-4">
          <span className="text-xs text-muted-foreground">{eventCount} events</span>
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <span className={`h-2 w-2 rounded-full ${statusColor} ${status === "connected" ? "animate-pulse" : ""}`} />
            {status}
          </div>
        </div>
      </div>

      <div className="flex items-center gap-2 flex-wrap">
        <Button
          variant={paused ? "default" : "outline"}
          size="sm"
          onClick={paused ? handleResume : () => setPaused(true)}
        >
          {paused ? <Play className="h-3 w-3 mr-1" /> : <Pause className="h-3 w-3 mr-1" />}
          {paused ? `Resume (${bufferRef.current.length} buffered)` : "Pause"}
        </Button>
        <Button variant="outline" size="sm" onClick={handleClear}>
          <Trash2 className="h-3 w-3 mr-1" />
          Clear
        </Button>
        <div className="h-4 w-px bg-border mx-1" />
        {allTypes.map((type) => {
          const style = EVENT_TYPE_STYLES[type] || EVENT_TYPE_STYLES.token;
          const hidden = filterTypes.has(type);
          return (
            <Badge
              key={type}
              variant="outline"
              className={`cursor-pointer select-none ${hidden ? "opacity-30 line-through" : ""} ${style.bg} ${style.text}`}
              onClick={() => toggleFilter(type)}
            >
              {type}
            </Badge>
          );
        })}
      </div>

      <Card className="flex-1 min-h-0">
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <div className="flex items-center gap-2">
            <CardTitle className="text-sm font-medium">Event Stream</CardTitle>
            <span className={`h-2 w-2 rounded-full ${statusColor}`} />
          </div>
          <span className="text-xs text-muted-foreground">{filteredEvents.length} visible</span>
        </CardHeader>
        <CardContent className="h-[calc(100%-3.5rem)]">
          {filteredEvents.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-2 py-12 text-muted-foreground">
              <Activity className="h-8 w-8 opacity-30" />
              <p className="text-sm">{status === "connected" ? "Waiting for events..." : "Connecting to activity feed..."}</p>
            </div>
          ) : (
            <ScrollArea className="h-[calc(100vh-320px)]">
              <div className="space-y-1 font-mono text-xs">
                {filteredEvents.map((event, i) => {
                  const style = EVENT_TYPE_STYLES[event.type] || EVENT_TYPE_STYLES.token;
                  return (
                    <div
                      key={`${event.timestamp}-${i}`}
                      className="flex items-start gap-2 rounded px-2 py-1.5 hover:bg-muted/50"
                    >
                      <span className="text-muted-foreground whitespace-nowrap shrink-0 tabular-nums">
                        {formatTime(event.timestamp)}
                      </span>
                      <Badge variant="outline" className={`shrink-0 text-[10px] px-1.5 py-0 ${style.bg} ${style.text}`}>
                        {event.agent}
                      </Badge>
                      <Badge variant="outline" className={`shrink-0 text-[10px] px-1.5 py-0 ${style.bg} ${style.text}`}>
                        {event.type}
                      </Badge>
                      <span className="truncate text-foreground">{event.data}</span>
                    </div>
                  );
                })}
              </div>
            </ScrollArea>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
