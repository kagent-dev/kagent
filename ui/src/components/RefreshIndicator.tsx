"use client";

import { RefreshCw } from "lucide-react";
import { useAgents } from "./AgentsProvider";
import { cn } from "@/lib/utils";

export function RefreshIndicator() {
  const { isRefreshing } = useAgents();

  if (!isRefreshing) {
    return null;
  }

  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <RefreshCw className={cn("h-4 w-4 animate-spin")} />
      <span>Updating...</span>
    </div>
  );
}
