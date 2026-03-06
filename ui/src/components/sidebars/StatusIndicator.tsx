"use client";

import { useSidebar } from "@/components/ui/sidebar";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useSidebarStatus } from "@/lib/sidebar-status-context";
import { Loader2, AlertCircle, RefreshCw } from "lucide-react";

export function StatusIndicator() {
  const { state } = useSidebar();
  const { status, retry } = useSidebarStatus();
  const isCollapsed = state === "collapsed";

  const isOk = status === "ok";
  const isFailed = status === "plugins-failed";
  const isLoading = status === "loading";

  const dot = (
    <span
      className={`h-2 w-2 shrink-0 rounded-full ${
        isFailed ? "bg-destructive" : "bg-green-500"
      }`}
      aria-hidden="true"
    />
  );

  const statusText = isLoading
    ? "Loading…"
    : isFailed
      ? "Plugins failed"
      : "All systems operational";

  const content = (
    <div
      className="flex items-center gap-2 px-2 py-1 text-xs text-muted-foreground"
      {...(isFailed && { "data-testid": "plugins-error" })}
    >
      {isLoading ? (
        <Loader2 className="h-3 w-3 shrink-0 animate-spin" />
      ) : (
        dot
      )}
      <span className={isFailed ? "text-destructive" : undefined}>{statusText}</span>
      {isFailed && (
        <button
          type="button"
          onClick={retry}
          data-testid="plugins-retry"
          className="ml-auto inline-flex items-center gap-1 rounded px-1.5 py-0.5 hover:bg-muted"
          aria-label="Retry loading plugins"
        >
          <RefreshCw className="h-3 w-3" />
          Retry
        </button>
      )}
    </div>
  );

  if (isCollapsed) {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <div className="flex items-center justify-center px-2 py-1">
              {isLoading ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                dot
              )}
            </div>
          </TooltipTrigger>
          <TooltipContent side="right">{statusText}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }

  return content;
}
