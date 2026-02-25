"use client";

import { useSidebar } from "@/components/ui/sidebar";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

export function StatusIndicator() {
  const { state } = useSidebar();
  const isCollapsed = state === "collapsed";

  const dot = (
    <span
      className="h-2 w-2 shrink-0 rounded-full bg-green-500"
      aria-hidden="true"
    />
  );

  if (isCollapsed) {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <div className="flex items-center justify-center px-2 py-1">
              {dot}
            </div>
          </TooltipTrigger>
          <TooltipContent side="right">All systems operational</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }

  return (
    <div className="flex items-center gap-2 px-2 py-1 text-xs text-muted-foreground">
      {dot}
      <span>All systems operational</span>
    </div>
  );
}
