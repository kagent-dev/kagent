"use client";

import { SidebarTrigger } from "@/components/ui/sidebar";
import KAgentLogoWithText from "./kagent-logo-text";

/**
 * Visible only when the sidebar runs in mobile (Sheet) mode. The sidebar
 * switches at `md:` (see `Sidebar` in `components/ui/sidebar`), so this bar
 * must hide at the same breakpoint to avoid a band where neither the desktop
 * sidebar header nor the mobile trigger is visible.
 */
export function MobileTopBar() {
  return (
    <div className="flex items-center gap-2 border-b px-4 py-3 md:hidden">
      <SidebarTrigger aria-label="Open navigation" />
      <KAgentLogoWithText className="h-5" />
    </div>
  );
}
