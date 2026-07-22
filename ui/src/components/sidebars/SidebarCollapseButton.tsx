"use client";

import { ChevronsLeft, ChevronsRight } from "lucide-react";
import { useSidebar } from "@/components/ui/sidebar";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

/**
 * Desktop-only collapse/expand toggle, rendered in the sidebar footer.
 * Hidden on mobile where the `Sheet` overlay provides its own close affordance,
 * and a `SidebarTrigger` lives in `MobileTopBar`.
 *
 * In expanded state: shows a labelled button with a left-chevron.
 * In collapsed (icon) state: shows the right-chevron only, with a tooltip.
 */
export function SidebarCollapseButton() {
  const { state, toggleSidebar, isMobile } = useSidebar();
  if (isMobile) return null;
  const collapsed = state === "collapsed";

  const label = collapsed ? "Expand sidebar" : "Collapse sidebar";
  const Icon = collapsed ? ChevronsRight : ChevronsLeft;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={toggleSidebar}
          aria-label={label}
          aria-expanded={!collapsed}
          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
        >
          <Icon className="h-4 w-4 shrink-0" aria-hidden />
          <span className="group-data-[collapsible=icon]:hidden">
            Collapse
          </span>
          <kbd className="ml-auto hidden rounded border bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground group-data-[collapsible=icon]:hidden md:inline-block">
            ⌘B
          </kbd>
        </button>
      </TooltipTrigger>
      {collapsed && (
        <TooltipContent side="right">
          {label} <span className="ml-1 opacity-60">(⌘B)</span>
        </TooltipContent>
      )}
    </Tooltip>
  );
}
