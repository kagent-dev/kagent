"use client";

import { SidebarTrigger } from "@/components/ui/sidebar";
import KAgentLogoWithText from "./kagent-logo-text";

export function MobileTopBar() {
  return (
    <div className="flex items-center gap-2 px-4 py-3 border-b lg:hidden">
      <SidebarTrigger />
      <KAgentLogoWithText className="h-5" />
    </div>
  );
}
